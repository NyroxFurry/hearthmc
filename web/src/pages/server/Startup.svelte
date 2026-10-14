<script lang="ts">
  import { untrack } from 'svelte';
  import { ArrowUpCircle, Download, TriangleAlert, Coffee, TerminalSquare, MemoryStick } from '@lucide/svelte';
  import Modal from '../../components/Modal.svelte';
  import Toggle from '../../components/Toggle.svelte';
  import VersionPicker from '../../components/VersionPicker.svelte';
  import { api } from '../../lib/api';
  import { can, session } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog } from '../../lib/dialogs.svelte';
  import { mb } from '../../lib/format';
  import { flavorLabel } from '../../lib/mc';
  import type { Server, Status } from '../../lib/types';

  let { server, status, onsaved }: { server: Server; status: Status; onsaved?: () => void } = $props();

  const mc = $derived(server.game === 'minecraft');
  const canEdit = $derived(can(server, 'settings.edit'));
  const isAdmin = $derived(!!session.user?.isAdmin);
  const canMemory = $derived(isAdmin || (server.isOwner && !!session.user?.canCreateServers));
  const offline = $derived(status.state === 'offline');

  // editable copy of the settings
  function snapshot() {
    const cur = server.settings ?? {};
    const copy = JSON.parse(JSON.stringify(cur));
    for (const k of ['trackChat', 'manageRcon', 'eula']) copy[k] = !!copy[k];
    for (const k of ['jvmArgs', 'serverArgs', 'startupOverride', 'command', 'stopCommand', 'readyRegex']) copy[k] = copy[k] ?? '';
    return copy;
  }
  function javaModeOf(j: string): 'auto' | 'system' | 'path' {
    return j === 'auto' || j === 'system' ? j : 'path';
  }

  // svelte-ignore state_referenced_locally
  let s = $state<Record<string, any>>(snapshot());
  // svelte-ignore state_referenced_locally
  let memory = $state(server.memoryMb);
  // svelte-ignore state_referenced_locally
  let javaMode = $state<'auto' | 'system' | 'path'>(javaModeOf(server.settings?.java ?? 'auto'));
  // svelte-ignore state_referenced_locally
  let javaPath = $state<string>(javaModeOf(server.settings?.java ?? 'auto') === 'path' ? server.settings.java : '');
  // svelte-ignore state_referenced_locally
  let envText = $state<string>((server.settings?.env ?? []).join('\n'));
  let saving = $state(false);

  function reset() {
    const cur = server.settings ?? {};
    s = snapshot();
    memory = server.memoryMb;
    const j = cur.java ?? 'auto';
    javaMode = javaModeOf(j);
    javaPath = javaMode === 'path' ? j : '';
    envText = (cur.env ?? []).join('\n');
  }

  $effect(() => {
    server.updatedAt;
    untrack(reset);
  });

  let javaValue = $derived(javaMode === 'path' ? javaPath.trim() : javaMode);

  function patch(): Record<string, any> {
    const cur = snapshot();
    const out: Record<string, any> = {};
    const keys = mc
      ? ['javaMajor', 'flagsPreset', 'jvmArgs', 'serverArgs', 'minMemoryMb', 'trackChat', ...(isAdmin ? ['startupOverride', 'manageRcon'] : [])]
      : ['command', 'stopCommand', 'readyRegex', 'stopTimeout'];
    for (const k of keys) if (JSON.stringify(s[k] ?? null) !== JSON.stringify(cur[k] ?? null)) out[k] = s[k];
    if (mc && javaValue !== (server.settings?.java ?? 'auto')) out.java = javaValue;
    if (!mc) {
      const env = envText
        .split('\n')
        .map((l) => l.trim())
        .filter(Boolean);
      if (JSON.stringify(env) !== JSON.stringify(server.settings?.env ?? [])) out.env = env;
    }
    return out;
  }

  let changes = $derived.by(() => {
    s;
    javaValue;
    envText;
    return Object.keys(patch()).length + (memory !== server.memoryMb ? 1 : 0);
  });

  async function save() {
    saving = true;
    try {
      const body: Record<string, any> = {};
      const p = patch();
      if (Object.keys(p).length) body.settings = p;
      if (memory !== server.memoryMb) body.memoryMb = memory;
      if (body.settings?.minMemoryMb !== undefined) body.settings.minMemoryMb = Number(body.settings.minMemoryMb) || 0;
      if (body.settings?.javaMajor !== undefined) body.settings.javaMajor = Number(body.settings.javaMajor) || 0;
      if (body.settings?.stopTimeout !== undefined) body.settings.stopTimeout = Number(body.settings.stopTimeout) || 0;
      await api.patch(`/api/servers/${server.id}`, body);
      toastOk(offline ? 'Gespeichert' : 'Gespeichert, wirksam ab dem nächsten Start');
      onsaved?.();
    } catch (e) {
      toastError(e);
    } finally {
      saving = false;
    }
  }

  // ---- version change ----
  let versionOpen = $state(false);
  let vFlavor = $state('paper');
  let vVersion = $state('');
  let vBuild = $state('');
  let changing = $state(false);

  function openVersion() {
    vFlavor = server.settings?.flavor ?? 'paper';
    vVersion = server.settings?.version ?? '';
    vBuild = '';
    versionOpen = true;
  }

  async function changeVersion() {
    changing = true;
    try {
      await api.post(`/api/servers/${server.id}/mc/version`, { flavor: vFlavor, version: vVersion, build: vBuild });
      versionOpen = false;
      toastOk(`Wechsel auf ${flavorLabel(vFlavor, vVersion)} wird installiert`);
      onsaved?.();
    } catch (e) {
      toastError(e);
    } finally {
      changing = false;
    }
  }

  async function reinstall() {
    const ok = await confirmDialog({
      title: 'Serversoftware neu herunterladen?',
      text: 'Lädt die Server-JAR der eingestellten Version erneut, bei Paper und Purpur den neuesten Build. Welt, Plugins und Einstellungen bleiben unverändert.',
      confirm: 'Neu herunterladen',
    });
    if (!ok) return;
    try {
      await api.post(`/api/servers/${server.id}/reinstall`);
      toastOk('Installation gestartet');
    } catch (e) {
      toastError(e);
    }
  }

  const javaVersions = [8, 11, 16, 17, 21, 25];
  const memPresets = [1024, 2048, 4096, 6144, 8192, 12288, 16384];

  let preview = $derived.by(() => {
    if (!mc) return '';
    if (isAdmin && s.startupOverride) return s.startupOverride;
    const mem = memory || 2048;
    const min = Number(s.minMemoryMb) > 0 ? Number(s.minMemoryMb) : s.flagsPreset === 'aikar' ? mem : Math.max(256, Math.floor(mem / 4));
    const parts = ['java', `-Xms${min}M`, `-Xmx${mem}M`];
    if (s.flagsPreset === 'aikar') parts.push('[Aikar-Flags]');
    if (s.jvmArgs) parts.push(s.jvmArgs);
    parts.push(s.loaderArgsFile ? '@' + s.loaderArgsFile : `-jar ${s.jar || 'server.jar'}`, 'nogui');
    if (s.serverArgs) parts.push(s.serverArgs);
    return parts.join(' ');
  });
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Start &amp; Version</h2>
      <p>Serversoftware, Java und Startparameter. Änderungen gelten ab dem nächsten Start.</p>
    </div>
  </div>

  {#if mc}
    <section class="panel">
      <div class="panel-head"><h3>Serversoftware</h3></div>
      <div class="panel-body version">
        <div class="vinfo">
          <span class="vname">{flavorLabel(server.settings?.flavor, server.settings?.version)}</span>
          <span class="small muted">
            {#if server.settings?.installedBuild}Installiert: Build {server.settings.installedBuild}{:else if server.settings?.flavor === 'custom'}Eigene JAR-Datei: <code>{server.settings?.jar || 'server.jar'}</code>{:else}Noch nicht installiert{/if}
            {#if server.settings?.javaMajor}, Java {server.settings.javaMajor}{/if}
          </span>
        </div>
        {#if canEdit}
          <div class="row wrap">
            <button class="btn" onclick={reinstall} disabled={!offline || status.state === 'installing'} title={offline ? '' : 'Nur bei gestopptem Server'}><Download /> Neu herunterladen</button>
            <button class="btn primary" onclick={openVersion} disabled={!offline} title={offline ? '' : 'Nur bei gestopptem Server'}><ArrowUpCircle /> Version wechseln</button>
          </div>
        {/if}
      </div>
      {#if canEdit && !offline}<p class="panel-body tiny faint stopped">Zum Wechseln der Version muss der Server gestoppt sein.</p>{/if}
    </section>
  {/if}

  {#if canMemory}
    <section class="panel">
      <div class="panel-head"><MemoryStick size={16} /><h3>Arbeitsspeicher</h3><span class="spacer"></span><strong>{mb(memory)}</strong></div>
      <div class="panel-body stack">
        <input type="range" min="512" max="32768" step="512" bind:value={memory} class="range" aria-label="Arbeitsspeicher" />
        <div class="row wrap">
          {#each memPresets as p (p)}
            <button type="button" class="btn sm" class:primary={memory === p} onclick={() => (memory = p)}>{mb(p)}</button>
          {/each}
        </div>
        {#if !isAdmin && session.user?.maxMemoryMb}<p class="hint">Dein Kontingent für alle eigenen Server: {mb(session.user.maxMemoryMb)}.</p>{/if}
      </div>
    </section>
  {/if}

  {#if mc}
    <section class="panel">
      <div class="panel-head"><Coffee size={16} /><h3>Java</h3></div>
      <div class="panel-body stack">
        <div class="form-grid">
          <div class="field">
            <label for="jm">Java-Quelle</label>
            <select id="jm" class="select" bind:value={javaMode} disabled={!canEdit || server.runtime === 'docker'}>
              <option value="auto">Automatisch herunterladen (empfohlen)</option>
              <option value="system">Java des Systems verwenden</option>
              {#if isAdmin || javaMode === 'path'}<option value="path" disabled={!isAdmin}>Eigener Pfad</option>{/if}
            </select>
            {#if server.runtime === 'docker'}<p class="hint">Im Docker-Container kommt Java aus dem Image eclipse-temurin.</p>{/if}
          </div>
          <div class="field">
            <label for="jv">Java-Version</label>
            <select id="jv" class="select" bind:value={s.javaMajor} disabled={!canEdit}>
              {#each javaVersions as v (v)}<option value={v}>Java {v}</option>{/each}
              {#if s.javaMajor && !javaVersions.includes(Number(s.javaMajor))}<option value={s.javaMajor}>Java {s.javaMajor}</option>{/if}
            </select>
            <p class="hint">Wird bei der Installation passend zur Minecraft-Version gesetzt.</p>
          </div>
        </div>
        {#if javaMode === 'path'}
          <div class="field">
            <label for="jp">Pfad zur Java-Programmdatei</label>
            <input id="jp" class="input mono" bind:value={javaPath} placeholder="/usr/lib/jvm/java-21/bin/java" disabled={!isAdmin} />
          </div>
        {/if}
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><TerminalSquare size={16} /><h3>Startparameter</h3></div>
      <div class="panel-body stack">
        <Toggle
          checked={s.flagsPreset === 'aikar'}
          label="Optimierte Garbage-Collection (Aikar-Flags)"
          hint="Bewährte Einstellungen für flüssige Ticks. Für fast alle Server die richtige Wahl."
          disabled={!canEdit}
          onchange={(v) => (s.flagsPreset = v ? 'aikar' : 'none')}
        />
        <div class="form-grid">
          <div class="field">
            <label for="xms">Startspeicher (Xms) in MB</label>
            <input id="xms" class="input num" type="number" min="0" step="256" bind:value={s.minMemoryMb} disabled={!canEdit} placeholder="automatisch" />
            <p class="hint">0 = automatisch{s.flagsPreset === 'aikar' ? ' (bei Aikar-Flags gleich dem Maximum)' : ''}.</p>
          </div>
        </div>
        <div class="field">
          <label for="jvm">Zusätzliche JVM-Argumente</label>
          <input id="jvm" class="input mono" bind:value={s.jvmArgs} disabled={!canEdit} placeholder="-Dfile.encoding=UTF-8" spellcheck="false" />
        </div>
        <div class="field">
          <label for="sa">Server-Argumente</label>
          <input id="sa" class="input mono" bind:value={s.serverArgs} disabled={!canEdit} placeholder="z. B. --world-dir worlds" spellcheck="false" />
        </div>
        {#if isAdmin}
          <div class="field">
            <label for="so">Eigener Startbefehl <span class="faint">(nur Administratoren)</span></label>
            <textarea id="so" class="textarea mono" rows="2" bind:value={s.startupOverride} placeholder={'{{java}} -Xmx{{memory}}M -jar {{jar}} nogui'} spellcheck="false"></textarea>
            <p class="hint">Ersetzt den kompletten Befehl. Platzhalter: <code>{'{{java}}'}</code> <code>{'{{memory}}'}</code> <code>{'{{jar}}'}</code> <code>{'{{port}}'}</code>. Leer lassen für den Standard.</p>
          </div>
          <Toggle bind:checked={s.manageRcon} label="RCON vom Panel verwalten" hint="Setzt Port und Passwort vor jedem Start. Ohne RCON funktionieren Spieleraktionen, Positionen und TPS nur eingeschränkt." />
        {/if}
        <div class="preview">
          <span class="tiny faint">Ungefährer Startbefehl</span>
          <code class="cmd">{preview}</code>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Aufzeichnung</h3></div>
      <div class="panel-body">
        <Toggle bind:checked={s.trackChat} label="Chatnachrichten speichern" hint="Chat erscheint dann im Ereignisverlauf, in Spielerprofilen und optional auf der öffentlichen Seite. Spieler sollten davon wissen." disabled={!canEdit} />
      </div>
    </section>
  {:else}
    <section class="panel">
      <div class="panel-head"><TerminalSquare size={16} /><h3>Programm</h3></div>
      <div class="panel-body stack">
        <div class="field">
          <label for="cmd">Startbefehl</label>
          <input id="cmd" class="input mono" bind:value={s.command} disabled={!canEdit} spellcheck="false" />
          <p class="hint">Wird im Serverordner ausgeführt. Platzhalter: <code>{'{{port}}'}</code>, <code>{'{{memory}}'}</code></p>
        </div>
        <div class="form-grid">
          <div class="field">
            <label for="stop">Stopp-Befehl</label>
            <input id="stop" class="input mono" bind:value={s.stopCommand} disabled={!canEdit} placeholder="leer = Signal senden" />
          </div>
          <div class="field">
            <label for="rr">Zeile für „bereit“ (Regex)</label>
            <input id="rr" class="input mono" bind:value={s.readyRegex} disabled={!canEdit} placeholder="leer = sofort bereit" />
          </div>
          <div class="field">
            <label for="st">Zeit zum Beenden (Sekunden)</label>
            <input id="st" class="input num" type="number" min="0" bind:value={s.stopTimeout} disabled={!canEdit} />
          </div>
        </div>
        <div class="field">
          <label for="env">Umgebungsvariablen</label>
          <textarea id="env" class="textarea mono" rows="4" bind:value={envText} disabled={!canEdit} placeholder="NAME=wert" spellcheck="false"></textarea>
          <p class="hint">Eine Variable pro Zeile im Format NAME=wert.</p>
        </div>
      </div>
    </section>
  {/if}

  {#if changes > 0 && (canEdit || canMemory)}
    <div class="savebar">
      <span class="small">{changes === 1 ? 'Eine Änderung' : `${changes} Änderungen`} nicht gespeichert</span>
      <span class="spacer"></span>
      <button class="btn" onclick={reset}>Verwerfen</button>
      <button class="btn primary" onclick={save} disabled={saving}>Speichern</button>
    </div>
  {/if}
</div>

<Modal bind:open={versionOpen} title="Version wechseln" size="l">
  <div class="stack">
    <VersionPicker bind:flavor={vFlavor} bind:version={vVersion} bind:build={vBuild} />
    <div class="notice warn">
      <TriangleAlert />
      <span>
        Lege vorher ein Backup an. Ein Wechsel auf eine ältere Version kann die Welt beschädigen, und Plugins oder Mods müssen zur neuen Version passen.
        {#if vFlavor !== server.settings?.flavor}Beim Wechsel der Server-Art bleiben vorhandene Plugins oder Mods liegen, werden aber evtl. nicht geladen.{/if}
      </span>
    </div>
  </div>
  {#snippet footer()}
    <button class="btn" onclick={() => (versionOpen = false)}>Abbrechen</button>
    <button class="btn primary" onclick={changeVersion} disabled={changing || (!vVersion && vFlavor !== 'custom')}>
      {flavorLabel(vFlavor, vVersion)} installieren
    </button>
  {/snippet}
</Modal>

<style>
  .version {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
  }
  .vinfo {
    flex: 1;
    min-width: 220px;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .vname {
    font-family: var(--font-display);
    font-size: 19px;
    font-weight: 700;
  }
  .stopped {
    border-top: 1px solid var(--line);
    padding-top: 10px;
    padding-bottom: 12px;
  }
  .panel-head :global(svg) {
    color: var(--text-3);
  }
  .range {
    width: 100%;
    accent-color: var(--accent);
  }
  .preview {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .cmd {
    display: block;
    padding: 9px 11px;
    border-radius: var(--r-s);
    background: var(--console);
    color: var(--console-text);
    font-size: 12.5px;
    line-height: 1.5;
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
