<script lang="ts">
  import { onMount } from 'svelte';
  import { UserPlus, Pencil, KeyRound, ShieldOff, Ban, CircleCheck, Trash2, ShieldCheck, Dices } from '@lucide/svelte';
  import Modal from '../../components/Modal.svelte';
  import Toggle from '../../components/Toggle.svelte';
  import Menu, { type MenuItem } from '../../components/Menu.svelte';
  import { api } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog, promptDialog } from '../../lib/dialogs.svelte';
  import { ago, date, mb } from '../../lib/format';
  import type { User } from '../../lib/types';

  let users = $state<User[]>([]);
  let q = $state('');

  async function load() {
    try {
      users = await api.get('/api/admin/users');
    } catch (e) {
      toastError(e);
    }
  }
  onMount(load);

  let shown = $derived(users.filter((u) => !q.trim() || u.username.toLowerCase().includes(q.trim().toLowerCase()) || u.email.toLowerCase().includes(q.trim().toLowerCase())));

  // ---- editor ----
  let open = $state(false);
  let editId = $state<number | null>(null);
  let f = $state({ username: '', email: '', password: '', isAdmin: false, canCreateServers: false, maxServers: 0, maxMemoryMb: 0 });

  function genPassword() {
    const chars = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';
    const buf = new Uint32Array(14);
    crypto.getRandomValues(buf);
    f.password = [...buf].map((n) => chars[n % chars.length]).join('');
  }

  function startNew() {
    editId = null;
    f = { username: '', email: '', password: '', isAdmin: false, canCreateServers: true, maxServers: 2, maxMemoryMb: 8192 };
    genPassword();
    open = true;
  }

  function startEdit(u: User) {
    editId = u.id;
    f = { username: u.username, email: u.email, password: '', isAdmin: u.isAdmin, canCreateServers: u.canCreateServers, maxServers: u.maxServers, maxMemoryMb: u.maxMemoryMb };
    open = true;
  }

  async function save(e: Event) {
    e.preventDefault();
    const body: any = { ...f, maxServers: Number(f.maxServers) || 0, maxMemoryMb: Number(f.maxMemoryMb) || 0 };
    if (editId && !body.password) delete body.password;
    try {
      if (editId) await api.patch(`/api/admin/users/${editId}`, body);
      else await api.post('/api/admin/users', body);
      toastOk(editId ? 'Benutzer gespeichert' : `${f.username} angelegt`);
      open = false;
      load();
    } catch (err) {
      toastError(err);
    }
  }

  async function patch(u: User, body: object, msg: string) {
    try {
      await api.patch(`/api/admin/users/${u.id}`, body);
      toastOk(msg);
      load();
    } catch (e) {
      toastError(e);
    }
  }

  async function resetPassword(u: User) {
    const pw = await promptDialog({
      title: `Neues Passwort für ${u.username}`,
      label: 'Passwort',
      hint: 'Mindestens 8 Zeichen. Alle Sitzungen des Benutzers werden abgemeldet.',
      confirm: 'Passwort setzen',
    });
    if (pw) patch(u, { password: pw }, 'Passwort gesetzt');
  }

  async function reset2fa(u: User) {
    const ok = await confirmDialog({ title: `Zwei-Faktor für ${u.username} zurücksetzen?`, text: 'Der Benutzer kann sich danach nur mit dem Passwort anmelden und 2FA neu einrichten.', confirm: 'Zurücksetzen' });
    if (!ok) return;
    try {
      await api.post(`/api/admin/users/${u.id}/reset-2fa`);
      toastOk('Zwei-Faktor zurückgesetzt');
      load();
    } catch (e) {
      toastError(e);
    }
  }

  async function remove(u: User) {
    const ok = await confirmDialog({ title: `${u.username} löschen?`, text: 'Das Konto und alle Serverfreigaben werden entfernt.', confirm: 'Löschen', danger: true });
    if (!ok) return;
    try {
      await api.del(`/api/admin/users/${u.id}`);
      load();
    } catch (e) {
      toastError(e);
    }
  }

  function items(u: User): MenuItem[] {
    const self = u.id === session.user?.id;
    const out: MenuItem[] = [
      { label: 'Bearbeiten', icon: Pencil, onclick: () => startEdit(u) },
      { label: 'Passwort setzen', icon: KeyRound, onclick: () => resetPassword(u) },
    ];
    if (u.totpEnabled) out.push({ label: 'Zwei-Faktor zurücksetzen', icon: ShieldOff, onclick: () => reset2fa(u) });
    if (!self) {
      out.push(
        u.disabled
          ? { label: 'Wieder aktivieren', icon: CircleCheck, onclick: () => patch(u, { disabled: false }, `${u.username} ist wieder aktiv`) }
          : { label: 'Sperren', icon: Ban, onclick: () => patch(u, { disabled: true }, `${u.username} wurde gesperrt`) },
        { divider: true, label: '' },
        { label: 'Löschen', icon: Trash2, danger: true, onclick: () => remove(u) },
      );
    }
    return out;
  }
</script>

<div class="stack">
  <div class="bar">
    <input class="input search" bind:value={q} placeholder="Benutzer suchen" aria-label="Benutzer suchen" />
    <span class="spacer"></span>
    <a class="btn" href="/admin/invites">Einladungslink</a>
    <button class="btn primary" onclick={startNew}><UserPlus /> Benutzer anlegen</button>
  </div>

  <div class="panel table-wrap">
    <table class="table">
      <thead>
        <tr>
          <th>Benutzer</th>
          <th>Rolle</th>
          <th class="r">Server</th>
          <th>Kontingent</th>
          <th>Zuletzt angemeldet</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each shown as u (u.id)}
          <tr class:off={u.disabled}>
            <td>
              <div class="who">
                <span class="av">{u.username.slice(0, 1).toUpperCase()}</span>
                <div class="wi">
                  <strong>{u.username}{#if u.id === session.user?.id}<span class="faint small"> (du)</span>{/if}</strong>
                  {#if u.email}<span class="tiny faint">{u.email}</span>{/if}
                </div>
                {#if u.totpEnabled}<span title="Zwei-Faktor aktiv"><ShieldCheck size={15} class="tfa" /></span>{/if}
              </div>
            </td>
            <td>
              {#if u.disabled}<span class="tag danger">gesperrt</span>
              {:else if u.isAdmin}<span class="tag copper">Administrator</span>
              {:else if u.canCreateServers}<span class="tag accent">darf Server anlegen</span>
              {:else}<span class="tag">Mitglied</span>{/if}
            </td>
            <td class="r num">{u.servers ?? 0}</td>
            <td class="small muted">
              {#if u.isAdmin}unbegrenzt{:else if u.canCreateServers}{u.maxServers || '∞'} Server, {u.maxMemoryMb ? mb(u.maxMemoryMb) : '∞ RAM'}{:else}–{/if}
            </td>
            <td class="small muted" title={u.lastLoginAt ? date(u.lastLoginAt) : ''}>{u.lastLoginAt ? ago(u.lastLoginAt) : 'nie'}</td>
            <td class="r"><Menu items={items(u)} /></td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</div>

<Modal bind:open title={editId ? 'Benutzer bearbeiten' : 'Benutzer anlegen'} size="m">
  <form id="user-form" class="stack" onsubmit={save}>
    <div class="form-grid">
      <div class="field">
        <label for="u-n">Benutzername</label>
        <input id="u-n" class="input" bind:value={f.username} required minlength="3" maxlength="32" autocomplete="off" />
      </div>
      <div class="field">
        <label for="u-e">E-Mail <span class="faint">(optional)</span></label>
        <input id="u-e" class="input" type="email" bind:value={f.email} />
      </div>
    </div>
    <div class="field">
      <label for="u-p">{editId ? 'Neues Passwort (optional)' : 'Passwort'}</label>
      <div class="row">
        <input id="u-p" class="input mono" bind:value={f.password} minlength="8" required={!editId} autocomplete="new-password" />
        <button type="button" class="btn icon" onclick={genPassword} title="Zufälliges Passwort" aria-label="Zufälliges Passwort erzeugen"><Dices /></button>
      </div>
      {#if !editId}<p class="hint">Gib das Passwort sicher weiter. Der Benutzer kann es danach selbst ändern.</p>{/if}
    </div>
    <Toggle bind:checked={f.isAdmin} label="Administrator" hint="Voller Zugriff auf alle Server, Benutzer und Einstellungen." />
    {#if !f.isAdmin}
      <Toggle bind:checked={f.canCreateServers} label="Darf eigene Server anlegen" />
      {#if f.canCreateServers}
        <div class="form-grid">
          <div class="field">
            <label for="u-ms">Max. Server</label>
            <input id="u-ms" class="input num" type="number" min="0" bind:value={f.maxServers} />
            <p class="hint">0 = unbegrenzt</p>
          </div>
          <div class="field">
            <label for="u-mm">Max. Arbeitsspeicher (MB, alle Server)</label>
            <input id="u-mm" class="input num" type="number" min="0" step="512" bind:value={f.maxMemoryMb} />
            <p class="hint">0 = unbegrenzt</p>
          </div>
        </div>
      {/if}
    {/if}
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={() => (open = false)}>Abbrechen</button>
    <button class="btn primary" type="submit" form="user-form">{editId ? 'Speichern' : 'Anlegen'}</button>
  {/snippet}
</Modal>

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .search {
    max-width: 280px;
  }
  .who {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .wi {
    display: flex;
    flex-direction: column;
  }
  .av {
    width: 30px;
    height: 30px;
    display: grid;
    place-items: center;
    border-radius: 8px;
    background: var(--surface-3);
    font-weight: 700;
    font-size: 13px;
    color: var(--text-2);
    flex: none;
  }
  .who :global(.tfa) {
    color: var(--accent);
  }
  tr.off td {
    opacity: 0.6;
  }
</style>
