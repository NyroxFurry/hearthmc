<script lang="ts">
  import PageBand from '../components/PageBand.svelte';
  import { onMount } from 'svelte';
  import { renderSVG } from 'uqr';
  import { ShieldCheck, ShieldOff, KeyRound, KeySquare, Monitor, Smartphone, LogOut, Plus, Trash2, Download, Sun, Moon } from '@lucide/svelte';
  import Modal from '../components/Modal.svelte';
  import CopyButton from '../components/CopyButton.svelte';
  import { api } from '../lib/api';
  import { session, applyTheme, themePref, type ThemePref } from '../lib/session.svelte';
  import { toastError, toastOk } from '../lib/toast.svelte';
  import { confirmDialog, promptDialog } from '../lib/dialogs.svelte';
  import { ago, date, dateTime } from '../lib/format';

  let user = $derived(session.user!);

  // ---- profile ----
  let email = $state(session.user?.email ?? '');
  let savingEmail = $state(false);
  async function saveEmail(e: Event) {
    e.preventDefault();
    savingEmail = true;
    try {
      const r = await api.put('/api/account', { email });
      session.user = r.user;
      toastOk('E-Mail gespeichert');
    } catch (err) {
      toastError(err);
    } finally {
      savingEmail = false;
    }
  }

  let theme = $state<ThemePref>(themePref());
  function setTheme(t: ThemePref) {
    theme = t;
    applyTheme(t);
  }

  // ---- password ----
  let pwCurrent = $state('');
  let pwNew = $state('');
  let pwRepeat = $state('');
  let pwBusy = $state(false);
  async function changePassword(e: Event) {
    e.preventDefault();
    if (pwNew !== pwRepeat) return toastError('Die neuen Passwörter stimmen nicht überein');
    pwBusy = true;
    try {
      await api.put('/api/account/password', { current: pwCurrent, new: pwNew });
      pwCurrent = pwNew = pwRepeat = '';
      toastOk('Passwort geändert. Andere Sitzungen wurden abgemeldet.');
      loadSessions();
    } catch (err) {
      toastError(err);
    } finally {
      pwBusy = false;
    }
  }

  // ---- 2FA ----
  let tfaOpen = $state(false);
  let tfaSecret = $state('');
  let tfaUrl = $state('');
  let tfaCode = $state('');
  let tfaBusy = $state(false);
  let recovery = $state<string[]>([]);
  let qr = $derived(tfaUrl ? renderSVG(tfaUrl, { pixelSize: 5, whiteColor: '#ffffff', blackColor: '#16191e' }) : '');

  async function startTfa() {
    try {
      const r = await api.post<{ secret: string; url: string }>('/api/account/2fa/setup');
      tfaSecret = r.secret;
      tfaUrl = r.url;
      tfaCode = '';
      recovery = [];
      tfaOpen = true;
    } catch (e) {
      toastError(e);
    }
  }

  async function enableTfa(e: Event) {
    e.preventDefault();
    tfaBusy = true;
    try {
      const r = await api.post<{ recoveryCodes: string[] }>('/api/account/2fa/enable', { code: tfaCode.replace(/\s/g, '') });
      recovery = r.recoveryCodes;
      if (session.user) session.user = { ...session.user, totpEnabled: true };
    } catch (err) {
      toastError(err);
    } finally {
      tfaBusy = false;
    }
  }

  async function disableTfa() {
    const pw = await promptDialog({ title: 'Zwei-Faktor-Anmeldung ausschalten', label: 'Passwort zur Bestätigung', confirm: 'Ausschalten' });
    if (!pw) return;
    try {
      await api.post('/api/account/2fa/disable', { password: pw });
      if (session.user) session.user = { ...session.user, totpEnabled: false };
      toastOk('Zwei-Faktor-Anmeldung ausgeschaltet');
    } catch (e) {
      toastError(e);
    }
  }

  function downloadCodes() {
    const blob = new Blob([`Wiederherstellungscodes für ${session.panel.name} (${user.username})\n\n${recovery.join('\n')}\n`], { type: 'text/plain' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = 'wiederherstellungscodes.txt';
    a.click();
    URL.revokeObjectURL(a.href);
  }

  // ---- sessions ----
  interface Sess {
    key: string;
    current: boolean;
    createdAt: number;
    lastSeenAt: number;
    expiresAt: number;
    ip: string;
    userAgent: string;
  }
  let sessions = $state<Sess[]>([]);
  async function loadSessions() {
    try {
      sessions = await api.get<Sess[]>('/api/account/sessions');
    } catch {}
  }

  function device(ua: string): { label: string; mobile: boolean } {
    const browser = /Edg\//.test(ua) ? 'Edge' : /OPR\//.test(ua) ? 'Opera' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : /curl|Go-http|python/i.test(ua) ? 'Skript' : 'Browser';
    const os = /Windows/.test(ua) ? 'Windows' : /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Mac OS X/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : '';
    return { label: os ? `${browser} auf ${os}` : browser, mobile: /Android|iPhone|iPad|Mobile/.test(ua) };
  }

  async function endSession(s: Sess) {
    try {
      await api.del(`/api/account/sessions/${s.key}`);
      loadSessions();
    } catch (e) {
      toastError(e);
    }
  }

  async function endOthers() {
    for (const s of sessions.filter((x) => !x.current)) await api.del(`/api/account/sessions/${s.key}`).catch(() => {});
    toastOk('Andere Sitzungen abgemeldet');
    loadSessions();
  }

  // ---- api keys ----
  interface Key {
    id: number;
    name: string;
    prefix: string;
    createdAt: number;
    lastUsedAt: number;
    expiresAt: number;
  }
  let keys = $state<Key[]>([]);
  let keyOpen = $state(false);
  let keyName = $state('');
  let keyDays = $state(0);
  let newToken = $state('');

  async function loadKeys() {
    try {
      keys = await api.get<Key[]>('/api/account/keys');
    } catch {}
  }

  async function createKey(e: Event) {
    e.preventDefault();
    try {
      const r = await api.post<{ token: string }>('/api/account/keys', { name: keyName, days: Number(keyDays) });
      newToken = r.token;
      loadKeys();
    } catch (err) {
      toastError(err);
    }
  }

  async function deleteKey(k: Key) {
    if (!(await confirmDialog({ title: `Schlüssel „${k.name}“ löschen?`, text: 'Programme, die ihn nutzen, verlieren sofort den Zugriff.', confirm: 'Löschen', danger: true }))) return;
    try {
      await api.del(`/api/account/keys/${k.id}`);
      loadKeys();
    } catch (e) {
      toastError(e);
    }
  }

  // ---- ssh keys (SFTP) ----
  interface SSHKey {
    id: number;
    name: string;
    fingerprint: string;
    createdAt: number;
    lastUsedAt: number;
  }
  let sshKeys = $state<SSHKey[]>([]);
  let sshOpen = $state(false);
  let sshName = $state('');
  let sshKey = $state('');
  let sshBusy = $state(false);

  async function loadSSHKeys() {
    try {
      sshKeys = await api.get<SSHKey[]>('/api/account/ssh-keys');
    } catch {}
  }

  async function addSSHKey(e: Event) {
    e.preventDefault();
    sshBusy = true;
    try {
      await api.post('/api/account/ssh-keys', { name: sshName, publicKey: sshKey });
      sshOpen = false;
      toastOk('SSH-Schlüssel hinzugefügt');
      loadSSHKeys();
    } catch (err) {
      toastError(err);
    } finally {
      sshBusy = false;
    }
  }

  async function deleteSSHKey(k: SSHKey) {
    if (!(await confirmDialog({ title: `SSH-Schlüssel „${k.name}“ entfernen?`, text: 'SFTP-Programme mit diesem Schlüssel können sich danach nicht mehr anmelden.', confirm: 'Entfernen', danger: true }))) return;
    try {
      await api.del(`/api/account/ssh-keys/${k.id}`);
      loadSSHKeys();
    } catch (e) {
      toastError(e);
    }
  }

  onMount(() => {
    loadSessions();
    loadKeys();
    loadSSHKeys();
  });
</script>

<PageBand title="Konto & Sicherheit" kicker={session.panel.name || 'Hearth'}>
  {#snippet sub()}Angemeldet als <strong>{user.username}</strong>{user.isAdmin ? ', Administrator' : ''}. Mitglied seit {date(user.createdAt)}.{/snippet}
</PageBand>

<div class="page narrow">

  <div class="stack wide">
    <section class="panel">
      <div class="panel-head"><h3>Profil</h3></div>
      <form class="panel-body form-grid" onsubmit={saveEmail}>
        <div class="field">
          <label for="un">Benutzername</label>
          <input id="un" class="input" value={user.username} disabled />
        </div>
        <div class="field">
          <label for="em">E-Mail</label>
          <div class="row">
            <input id="em" class="input" type="email" bind:value={email} autocomplete="email" />
            <button class="btn" disabled={savingEmail || email === user.email}>Speichern</button>
          </div>
        </div>
      </form>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Darstellung</h3></div>
      <div class="panel-body row wrap">
        <div class="seg" role="group" aria-label="Farbschema">
          <button class:on={theme === 'system'} onclick={() => setTheme('system')}><Monitor size={14} /> Wie System</button>
          <button class:on={theme === 'dark'} onclick={() => setTheme('dark')}><Moon size={14} /> Dunkel</button>
          <button class:on={theme === 'light'} onclick={() => setTheme('light')}><Sun size={14} /> Hell</button>
        </div>
        <span class="small faint">Gilt für diesen Browser.</span>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Passwort ändern</h3></div>
      <form class="panel-body stack" onsubmit={changePassword}>
        <div class="form-grid">
          <div class="field">
            <label for="pc">Aktuelles Passwort</label>
            <input id="pc" class="input" type="password" bind:value={pwCurrent} autocomplete="current-password" required />
          </div>
          <div class="field">
            <label for="pn">Neues Passwort</label>
            <input id="pn" class="input" type="password" bind:value={pwNew} autocomplete="new-password" minlength="8" required />
          </div>
          <div class="field">
            <label for="pr">Neues Passwort wiederholen</label>
            <input id="pr" class="input" type="password" bind:value={pwRepeat} autocomplete="new-password" minlength="8" required />
          </div>
        </div>
        <div><button class="btn primary" disabled={pwBusy || !pwCurrent || pwNew.length < 8}>Passwort ändern</button></div>
      </form>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h3>Zwei-Faktor-Anmeldung</h3>
        <span class="spacer"></span>
        {#if user.totpEnabled}<span class="tag accent"><ShieldCheck size={13} /> Aktiv</span>{:else}<span class="tag warn">Aus</span>{/if}
      </div>
      <div class="panel-body row wrap tfa">
        <p class="small muted grow">
          {#if user.totpEnabled}
            Bei der Anmeldung wird zusätzlich ein Code aus deiner Authenticator-App abgefragt. Verlierst du das Gerät, hilft ein Wiederherstellungscode.
          {:else}
            Schützt dein Konto mit einem zweiten Faktor aus einer App wie Aegis, 2FAS, Google Authenticator oder 1Password. Für Administratoren dringend empfohlen.
          {/if}
        </p>
        {#if user.totpEnabled}
          <button class="btn" onclick={disableTfa}><ShieldOff /> Ausschalten</button>
        {:else}
          <button class="btn primary" onclick={startTfa}><ShieldCheck /> Einrichten</button>
        {/if}
      </div>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h3>Angemeldete Geräte</h3>
        <span class="spacer"></span>
        {#if sessions.length > 1}<button class="btn sm" onclick={endOthers}><LogOut /> Alle anderen abmelden</button>{/if}
      </div>
      <ul class="list sess">
        {#each sessions as s (s.key)}
          {@const d = device(s.userAgent)}
          <li>
            <span class="dev">{#if d.mobile}<Smartphone />{:else}<Monitor />{/if}</span>
            <div class="grow">
              <strong>{d.label}</strong>
              {#if s.current}<span class="tag accent">Dieses Gerät</span>{/if}
              <div class="small muted">{s.ip || 'unbekannte IP'}, aktiv {ago(s.lastSeenAt)}, angemeldet am {date(s.createdAt)}</div>
            </div>
            {#if !s.current}<button class="btn sm" onclick={() => endSession(s)}>Abmelden</button>{/if}
          </li>
        {/each}
      </ul>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h3>API-Schlüssel</h3>
        <span class="spacer"></span>
        <button
          class="btn sm"
          onclick={() => {
            keyName = '';
            keyDays = 0;
            newToken = '';
            keyOpen = true;
          }}><Plus /> Schlüssel erstellen</button
        >
      </div>
      {#if keys.length}
        <ul class="list sess">
          {#each keys as k (k.id)}
            <li>
              <span class="dev"><KeyRound /></span>
              <div class="grow">
                <strong>{k.name}</strong> <code class="small">{k.prefix}…</code>
                <div class="small muted">
                  Erstellt {date(k.createdAt)}, {k.lastUsedAt ? `zuletzt benutzt ${ago(k.lastUsedAt)}` : 'noch nie benutzt'}{k.expiresAt ? `, läuft ab am ${date(k.expiresAt)}` : ''}
                </div>
              </div>
              <button class="btn ghost sm icon" onclick={() => deleteKey(k)} aria-label="Schlüssel löschen" title="Löschen"><Trash2 /></button>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="panel-body small muted">Mit einem API-Schlüssel steuern Skripte oder Bots das Panel mit deinen Rechten, etwa per <code>Authorization: Bearer hth_…</code>.</p>
      {/if}
    </section>

    <section class="panel">
      <div class="panel-head">
        <h3>SSH-Schlüssel für SFTP</h3>
        <span class="spacer"></span>
        <button
          class="btn sm"
          onclick={() => {
            sshName = '';
            sshKey = '';
            sshOpen = true;
          }}><Plus /> Schlüssel hinzufügen</button
        >
      </div>
      {#if sshKeys.length}
        <ul class="list sess">
          {#each sshKeys as k (k.id)}
            <li>
              <span class="dev"><KeySquare /></span>
              <div class="grow">
                <strong>{k.name}</strong>
                <div class="small muted"><code class="small">{k.fingerprint}</code></div>
                <div class="small muted">Hinzugefügt {date(k.createdAt)}, {k.lastUsedAt ? `zuletzt benutzt ${ago(k.lastUsedAt)}` : 'noch nie benutzt'}</div>
              </div>
              <button class="btn ghost sm icon" onclick={() => deleteSSHKey(k)} aria-label="SSH-Schlüssel entfernen" title="Entfernen"><Trash2 /></button>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="panel-body small muted">
          Mit einem SSH-Schlüssel meldest du dich bei SFTP ohne Passwort an.{user.totpEnabled ? ' Weil deine Zwei-Faktor-Anmeldung aktiv ist, geht SFTP nur mit Schlüssel.' : ''}
        </p>
      {/if}
    </section>
  </div>
</div>

<Modal bind:open={tfaOpen} title="Zwei-Faktor-Anmeldung einrichten" size="m">
  {#if recovery.length}
    <div class="stack">
      <p><strong>Fertig.</strong> Bewahre diese Wiederherstellungscodes sicher auf. Jeder funktioniert genau einmal, falls du keinen Zugriff auf die App hast.</p>
      <div class="codes mono">
        {#each recovery as c (c)}<span>{c}</span>{/each}
      </div>
      <div class="row">
        <CopyButton text={recovery.join('\n')} label="Kopieren" cls="btn sm" />
        <button class="btn sm" onclick={downloadCodes}><Download /> Als Datei speichern</button>
      </div>
    </div>
  {:else}
    <form id="tfa-form" class="stack" onsubmit={enableTfa}>
      <p class="small">Scanne den Code mit deiner Authenticator-App und gib danach den angezeigten sechsstelligen Code ein.</p>
      <div class="qr">{@html qr}</div>
      <div class="field">
        <span class="label">Oder Schlüssel von Hand eingeben</span>
        <div class="row"><code class="secret">{tfaSecret.replace(/(.{4})/g, '$1 ').trim()}</code><CopyButton text={tfaSecret} cls="btn ghost sm icon" /></div>
      </div>
      <div class="field">
        <label for="tc">Code aus der App</label>
        <input id="tc" class="input mono code" bind:value={tfaCode} inputmode="numeric" autocomplete="one-time-code" placeholder="123 456" required />
      </div>
    </form>
  {/if}
  {#snippet footer()}
    {#if recovery.length}
      <button class="btn primary" onclick={() => (tfaOpen = false)}>Ich habe die Codes gesichert</button>
    {:else}
      <button class="btn" onclick={() => (tfaOpen = false)}>Abbrechen</button>
      <button class="btn primary" type="submit" form="tfa-form" disabled={tfaBusy || tfaCode.replace(/\s/g, '').length < 6}>Aktivieren</button>
    {/if}
  {/snippet}
</Modal>

<Modal bind:open={keyOpen} title="API-Schlüssel erstellen" size="m">
  {#if newToken}
    <div class="stack">
      <p>Kopiere den Schlüssel jetzt. Er wird aus Sicherheitsgründen nur dieses eine Mal angezeigt.</p>
      <div class="row tokenrow"><code class="token">{newToken}</code><CopyButton text={newToken} label="Kopieren" cls="btn sm primary" /></div>
    </div>
  {:else}
    <form id="key-form" class="stack" onsubmit={createKey}>
      <div class="field">
        <label for="kn">Name</label>
        <input id="kn" class="input" bind:value={keyName} maxlength="60" placeholder="z. B. Discord-Bot" required />
      </div>
      <div class="field">
        <label for="kd">Gültigkeit</label>
        <select id="kd" class="select" bind:value={keyDays}>
          <option value={0}>Unbegrenzt</option>
          <option value={30}>30 Tage</option>
          <option value={90}>90 Tage</option>
          <option value={365}>1 Jahr</option>
        </select>
      </div>
      <p class="hint">Der Schlüssel hat dieselben Rechte wie dein Konto.</p>
    </form>
  {/if}
  {#snippet footer()}
    {#if newToken}
      <button class="btn primary" onclick={() => (keyOpen = false)}>Fertig</button>
    {:else}
      <button class="btn" onclick={() => (keyOpen = false)}>Abbrechen</button>
      <button class="btn primary" type="submit" form="key-form" disabled={!keyName.trim()}>Erstellen</button>
    {/if}
  {/snippet}
</Modal>

<Modal bind:open={sshOpen} title="SSH-Schlüssel hinzufügen" size="m">
  <form id="ssh-form" class="stack" onsubmit={addSSHKey}>
    <div class="field">
      <label for="sk">Öffentlicher Schlüssel</label>
      <textarea id="sk" class="input mono sshkey" bind:value={sshKey} rows="4" placeholder="ssh-ed25519 AAAA… name@rechner" spellcheck="false" required></textarea>
      <p class="hint">Der Inhalt der Datei mit der Endung .pub, etwa <code>~/.ssh/id_ed25519.pub</code>. Einen neuen Schlüssel erzeugt <code>ssh-keygen -t ed25519</code>. Den privaten Schlüssel nie hier einfügen.</p>
    </div>
    <div class="field">
      <label for="skn">Name</label>
      <input id="skn" class="input" bind:value={sshName} maxlength="60" placeholder="z. B. Laptop (sonst aus dem Schlüssel)" />
    </div>
  </form>
  {#snippet footer()}
    <button class="btn" onclick={() => (sshOpen = false)}>Abbrechen</button>
    <button class="btn primary" type="submit" form="ssh-form" disabled={sshBusy || !sshKey.trim()}>Hinzufügen</button>
  {/snippet}
</Modal>

<style>
  .sshkey {
    font-size: 12.5px;
    resize: vertical;
    min-height: 92px;
  }
  .wide {
    gap: 18px;
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .tfa {
    align-items: center;
    gap: 16px;
  }
  .seg button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .sess {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .sess li {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 12px 18px;
  }
  .dev {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: 8px;
    background: var(--surface-2);
    color: var(--text-2);
    flex: none;
  }
  .dev :global(svg) {
    width: 17px;
    height: 17px;
  }
  .qr {
    align-self: center;
    width: 200px;
    padding: 10px;
    background: #fff;
    border-radius: 10px;
    line-height: 0;
  }
  .qr :global(svg) {
    width: 100%;
    height: auto;
  }
  .secret {
    font-size: 13px;
    letter-spacing: 0.04em;
  }
  .code {
    font-size: 20px;
    height: 46px;
    letter-spacing: 0.2em;
    text-align: center;
  }
  .codes {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 6px 18px;
    padding: 14px 18px;
    border-radius: var(--r-m);
    background: var(--surface-2);
    font-size: 15px;
  }
  .tokenrow {
    padding: 8px 8px 8px 12px;
    border: 1px solid var(--line-2);
    border-radius: var(--r-m);
    background: var(--bg);
  }
  .token {
    flex: 1;
    background: none;
    padding: 0;
    overflow-wrap: anywhere;
  }
</style>
