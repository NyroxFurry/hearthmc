<script lang="ts">
  import FlavorPicker from '../../components/FlavorPicker.svelte';
  import { untrack } from 'svelte';
  import { ImageUp, Trash2, Plus, X, TriangleAlert, Shield } from '@lucide/svelte';
  import Toggle from '../../components/Toggle.svelte';
  import ServerIcon from '../../components/ServerIcon.svelte';
  import { api } from '../../lib/api';
  import { can, session, servers } from '../../lib/session.svelte';
  import { go } from '../../lib/router.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog } from '../../lib/dialogs.svelte';
  import { mb } from '../../lib/format';
  import type { PortSpec, Server, Status, User } from '../../lib/types';

  let { server, status, onsaved }: { server: Server; status: Status; onsaved?: () => void } = $props();

  const canEdit = $derived(can(server, 'settings.edit'));
  const isAdmin = $derived(!!session.user?.isAdmin);
  const mc = $derived(server.game === 'minecraft');

  let f = $state({
    name: '',
    description: '',
    color: '',
    displayAddress: '',
    autostart: false,
    autoRestart: true,
    cpuLimit: 0,
    diskLimitMb: 0,
    port: 0,
    extraPorts: [] as PortSpec[],
    runtime: 'native' as 'native' | 'docker',
    dockerImage: '',
    ownerId: 0,
    suspended: false,
  });
  let users = $state<User[]>([]);
  let saving = $state(false);
  let iconVersion = $state(Date.now());

  function reset() {
    f = {
      name: server.name,
      description: server.description,
      color: server.color,
      displayAddress: server.displayAddress,
      autostart: server.autostart,
      autoRestart: server.autoRestart,
      cpuLimit: server.cpuLimit,
      diskLimitMb: server.diskLimitMb,
      port: server.port,
      extraPorts: (server.extraPorts ?? []).map((p) => ({ ...p })),
      runtime: server.runtime,
      dockerImage: server.dockerImage,
      ownerId: server.ownerId,
      suspended: server.suspended,
    };
  }

  $effect(() => {
    server.updatedAt;
    untrack(reset);
  });

  $effect(() => {
    if (isAdmin)
      untrack(() =>
        api
          .get<User[]>('/api/admin/users')
          .then((u) => (users = u))
          .catch(() => {}),
      );
  });

  const generalKeys = ['name', 'description', 'color', 'displayAddress', 'autostart', 'autoRestart'] as const;
  const adminKeys = ['cpuLimit', 'diskLimitMb', 'port', 'extraPorts', 'runtime', 'dockerImage', 'ownerId', 'suspended'] as const;

  let diff = $derived.by(() => {
    const out: Record<string, any> = {};
    const cur = server as any;
    for (const k of generalKeys) if (canEdit && JSON.stringify(f[k]) !== JSON.stringify(cur[k])) out[k] = f[k];
    if (isAdmin) for (const k of adminKeys) if (JSON.stringify(f[k]) !== JSON.stringify(cur[k] ?? (k === 'extraPorts' ? [] : undefined))) out[k] = f[k];
    return out;
  });
  let dirty = $derived(Object.keys(diff).length > 0);

  async function save() {
    saving = true;
    try {
      const body = { ...diff };
      for (const k of ['cpuLimit', 'diskLimitMb', 'port', 'ownerId']) if (k in body) body[k] = Number(body[k]) || 0;
      if (body.extraPorts) body.extraPorts = body.extraPorts.map((p: PortSpec) => ({ ...p, port: Number(p.port) }));
      const s = await api.patch<Server>(`/api/servers/${server.id}`, body);
      servers.put(s);
      toastOk('Einstellungen gespeichert');
      onsaved?.();
    } catch (e) {
      toastError(e);
    } finally {
      saving = false;
    }
  }

  let iconInput: HTMLInputElement;
  async function uploadIcon(ev: Event) {
    const input = ev.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    try {
      await api.post(`/api/servers/${server.id}/mc/icon`, file);
      iconVersion = Date.now();
      toastOk('Servericon gespeichert. Es erscheint nach dem nächsten Start in der Serverliste.');
      onsaved?.();
    } catch (e) {
      toastError(e);
    }
  }
  async function removeIcon() {
    try {
      await api.del(`/api/servers/${server.id}/mc/icon`);
      iconVersion = Date.now();
      onsaved?.();
    } catch (e) {
      toastError(e);
    }
  }

  async function deleteServer() {
    const ok = await confirmDialog({
      title: `„${server.name}“ endgültig löschen?`,
      text: 'Alle Dateien, Welten, Backups, Statistiken und Einstellungen dieses Servers werden gelöscht. Das lässt sich nicht rückgängig machen.',
      typed: server.name,
      confirm: 'Server löschen',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.del(`/api/servers/${server.id}`, { confirm: server.name });
      servers.remove(server.id);
      toastOk(`„${server.name}“ wurde gelöscht`);
      go('/');
    } catch (e) {
      toastError(e);
    }
  }

</script>

<input bind:this={iconInput} type="file" accept="image/png,image/jpeg,image/gif" hidden onchange={uploadIcon} />

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Allgemein</h2>
      <p>Name, Darstellung und Verhalten des Servers.</p>
    </div>
  </div>

  <section class="panel">
    <div class="panel-body stack">
      <div class="ident">
        <div class="iconbox">
          {#key iconVersion}
            <ServerIcon id={server.id} name={f.name || server.name} color={f.color} size={64} version={iconVersion} hasIcon={server.hasIcon} />
          {/key}
          {#if mc && canEdit}
            <div class="row">
              <button class="btn sm" onclick={() => iconInput.click()}><ImageUp /> Icon</button>
              <button class="btn ghost sm icon" onclick={removeIcon} title="Icon entfernen" aria-label="Icon entfernen"><Trash2 /></button>
            </div>
          {/if}
        </div>
        <div class="stack grow">
          <div class="form-grid">
            <div class="field">
              <label for="sn">Name</label>
              <input id="sn" class="input" bind:value={f.name} maxlength="60" disabled={!canEdit} />
            </div>
            <div class="field">
              <label for="sa">Adresse für Spieler</label>
              <input id="sa" class="input mono" bind:value={f.displayAddress} maxlength="120" placeholder="{location.hostname}:{server.port}" disabled={!canEdit} />
            </div>
          </div>
          <div class="field">
            <label for="sd">Beschreibung</label>
            <textarea id="sd" class="textarea" rows="2" bind:value={f.description} maxlength="500" disabled={!canEdit}></textarea>
          </div>
        </div>
      </div>
      <div class="field">
        <span class="label">Farbe</span>
        <p class="hint">Färbt den Kopfbereich, die Leiste und die öffentliche Seite dieses Servers.</p>
        <FlavorPicker bind:value={f.color} disabled={!canEdit} seed={server.id} />
        {#if mc}<p class="hint">Die Kachel zeigt die Farbe, solange kein eigenes Servericon hochgeladen ist. Icons werden auf 64×64 Pixel verkleinert.</p>{/if}
      </div>
    </div>
  </section>

  <section class="panel">
    <div class="panel-head"><h3>Verhalten</h3></div>
    <div class="panel-body stack">
      <Toggle bind:checked={f.autostart} label="Mit dem Panel starten" hint="Startet automatisch, wenn das Panel oder der Host neu startet." disabled={!canEdit} />
      <Toggle bind:checked={f.autoRestart} label="Nach Absturz neu starten" hint="Bei wiederholten Abstürzen kurz hintereinander wird pausiert, damit nichts in einer Schleife hängt." disabled={!canEdit} />
    </div>
  </section>

  {#if isAdmin}
    <section class="panel">
      <div class="panel-head"><Shield size={16} /><h3>Ressourcen und Netzwerk</h3><span class="spacer"></span><span class="tag">Administratoren</span></div>
      <div class="panel-body stack">
        <div class="form-grid">
          <div class="field">
            <label for="cpu">CPU-Limit (Kerne)</label>
            <input id="cpu" class="input num" type="number" min="0" step="0.5" bind:value={f.cpuLimit} />
            <p class="hint">0 = unbegrenzt. Nur im Docker-Container wirksam.</p>
          </div>
          <div class="field">
            <label for="disk">Speicherplatz-Limit (MB)</label>
            <input id="disk" class="input num" type="number" min="0" step="1024" bind:value={f.diskLimitMb} />
            <p class="hint">0 = unbegrenzt. {f.diskLimitMb > 0 ? mb(f.diskLimitMb) : ''}</p>
          </div>
          <div class="field">
            <label for="port">Port</label>
            <input id="port" class="input num" type="number" min="1" max="65535" bind:value={f.port} />
          </div>
        </div>

        <div class="field">
          <span class="label">Zusätzliche Ports</span>
          <p class="hint">Für Plugins mit eigenem Port, etwa Voice-Chat oder eine Karten-Webseite. Im Docker-Container werden sie freigegeben.</p>
          {#each f.extraPorts as p, i (i)}
            <div class="portrow">
              <input class="input num" type="number" min="1" max="65535" bind:value={p.port} aria-label="Port" />
              <select class="select" bind:value={p.protocol} aria-label="Protokoll">
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
                <option value="both">TCP + UDP</option>
              </select>
              <input class="input" bind:value={p.note} placeholder="Wofür? (z. B. BlueMap)" aria-label="Notiz" />
              <button class="btn ghost icon" onclick={() => (f.extraPorts = f.extraPorts.filter((_, j) => j !== i))} aria-label="Port entfernen"><X /></button>
            </div>
          {/each}
          <div><button class="btn sm" onclick={() => (f.extraPorts = [...f.extraPorts, { port: 0, protocol: 'tcp', note: '' }])}><Plus /> Port</button></div>
        </div>

        <div class="form-grid">
          <div class="field">
            <label for="rt">Ausführung</label>
            <select id="rt" class="select" bind:value={f.runtime} disabled={status.state !== 'offline'}>
              <option value="native">Direkt auf dem Host</option>
              <option value="docker" disabled={!session.meta?.docker && server.runtime !== 'docker'}>Docker-Container</option>
            </select>
            {#if status.state !== 'offline'}<p class="hint">Nur bei gestopptem Server änderbar.</p>{/if}
          </div>
          {#if f.runtime === 'docker'}
            <div class="field">
              <label for="img">Docker-Image</label>
              <input id="img" class="input mono" bind:value={f.dockerImage} placeholder={mc ? 'automatisch (eclipse-temurin)' : 'z. B. ubuntu:24.04'} />
            </div>
          {/if}
          <div class="field">
            <label for="owner">Besitzer</label>
            <select id="owner" class="select" bind:value={f.ownerId}>
              {#each users as u (u.id)}<option value={u.id}>{u.username}</option>{/each}
              {#if !users.some((u) => u.id === f.ownerId)}<option value={f.ownerId}>{server.ownerName}</option>{/if}
            </select>
          </div>
        </div>
        <Toggle bind:checked={f.suspended} label="Server sperren" hint="Gesperrte Server können nur von Administratoren gestartet werden, etwa bei offenen Rechnungen oder Regelverstößen." />
      </div>
    </section>
  {/if}

  {#if isAdmin || server.isOwner}
    <section class="panel danger-zone">
      <div class="panel-head"><TriangleAlert size={16} /><h3>Server löschen</h3></div>
      <div class="panel-body row wrap">
        <p class="small muted grow">Entfernt den Server mit allen Dateien, Backups und Statistiken. Der Server wird vorher gestoppt.</p>
        <button class="btn danger" onclick={deleteServer}><Trash2 /> Server löschen</button>
      </div>
    </section>
  {/if}

  {#if dirty}
    <div class="savebar">
      <span class="small">Ungespeicherte Änderungen</span>
      <span class="spacer"></span>
      <button class="btn" onclick={reset}>Verwerfen</button>
      <button class="btn primary" onclick={save} disabled={saving || !f.name.trim()}>Speichern</button>
    </div>
  {/if}
</div>

<style>
  .ident {
    display: flex;
    gap: 20px;
    align-items: flex-start;
  }
  .iconbox {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .portrow {
    display: grid;
    grid-template-columns: 110px 140px 1fr auto;
    gap: 8px;
    margin-top: 4px;
  }
  .panel-head :global(svg) {
    color: var(--text-3);
  }
  .danger-zone {
    border-color: color-mix(in srgb, var(--danger) 35%, var(--line));
  }
  .danger-zone .panel-head :global(svg) {
    color: var(--danger);
  }
  @media (max-width: 700px) {
    .ident {
      flex-direction: column;
    }
    .portrow {
      grid-template-columns: 1fr 1fr auto;
    }
    .portrow .input:nth-child(3) {
      grid-column: 1 / -1;
    }
  }
</style>
