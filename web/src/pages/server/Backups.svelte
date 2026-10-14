<script lang="ts">
  import { onMount } from 'svelte';
  import { Archive, Plus, Download, ArchiveRestore, Pencil, Lock, LockOpen, Trash2, TriangleAlert, Settings2 } from '@lucide/svelte';
  import Menu, { type MenuItem } from '../../components/Menu.svelte';
  import Modal from '../../components/Modal.svelte';
  import { api } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { can } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog, confirmWithOption, promptDialog } from '../../lib/dialogs.svelte';
  import { bytes, dateTime, ago, num } from '../../lib/format';
  import type { Backup, Server, Status } from '../../lib/types';

  let { server, status }: { server: Server; status: Status } = $props();

  const url = $derived(`/api/servers/${server.id}/backups`);
  let list = $state<Backup[]>([]);
  let loaded = $state(false);
  let ignore = $state<string[]>([]);
  let max = $state(0);
  let totalSize = $state(0);
  let progress = $state<{ backupId: string; status: string; files: number; bytes: number; total: number; error?: string } | null>(null);
  let creating = $state(false);

  let ignoreOpen = $state(false);
  let ignoreText = $state('');
  let savingIgnore = $state(false);

  async function load() {
    try {
      const r = await api.get<{ backups: Backup[]; running: string; ignore: string[]; max: number; totalSize: number }>(url);
      list = r.backups;
      ignore = r.ignore;
      max = r.max;
      totalSize = r.totalSize;
      if (!r.running && progress?.status === 'running') progress = null;
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }

  onMount(() => {
    load();
    return live.on('backup', server.id, (e) => {
      progress = e.d;
      if (['ok', 'failed', 'restored'].includes(e.d.status)) {
        if (e.d.status === 'ok') toastOk('Backup fertig');
        if (e.d.status === 'failed') toastError('Backup fehlgeschlagen: ' + (e.d.error || 'unbekannter Fehler'));
        if (e.d.status === 'restored') toastOk('Backup wiederhergestellt');
        setTimeout(() => (progress = null), 1200);
        load();
      } else if (e.d.status === 'running' && !list.some((b) => b.id === e.d.backupId)) {
        load();
      }
    });
  });

  async function create() {
    const name = await promptDialog({
      title: 'Backup erstellen',
      label: 'Name (optional)',
      placeholder: 'z. B. Vor dem Update auf 1.21',
      hint: 'Läuft der Server, wird das automatische Speichern kurz pausiert, damit die Welt konsistent gesichert wird.',
      confirm: 'Backup starten',
      optional: true,
    });
    if (name === null) return;
    creating = true;
    try {
      await api.post(url, { name });
      load();
    } catch (e) {
      toastError(e);
    } finally {
      creating = false;
    }
  }

  async function restore(b: Backup) {
    const r = await confirmWithOption({
      title: `„${b.name}“ wiederherstellen?`,
      text: 'Dateien aus dem Backup überschreiben die aktuellen Dateien des Servers. Erstelle vorher ein Backup, wenn du den jetzigen Stand behalten willst.',
      checkbox: 'Vorher alle Dateien im Serverordner entfernen (sauberer Stand)',
      checkboxDefault: true,
      confirm: 'Wiederherstellen',
      danger: true,
    });
    if (!r.ok) return;
    try {
      await api.post(`${url}/${b.id}/restore`, { wipe: r.checked });
      progress = { backupId: b.id, status: 'restoring', files: 0, bytes: 0, total: b.size };
    } catch (e) {
      toastError(e);
    }
  }

  async function rename(b: Backup) {
    const n = await promptDialog({ title: 'Backup umbenennen', label: 'Name', value: b.name, confirm: 'Speichern' });
    if (!n?.trim()) return;
    try {
      await api.patch(`${url}/${b.id}`, { name: n.trim() });
      load();
    } catch (e) {
      toastError(e);
    }
  }

  async function setLocked(b: Backup, locked: boolean) {
    try {
      await api.patch(`${url}/${b.id}`, { locked });
      toastOk(locked ? 'Backup gesperrt, es wird nicht automatisch gelöscht' : 'Sperre aufgehoben');
      load();
    } catch (e) {
      toastError(e);
    }
  }

  async function remove(b: Backup) {
    const ok = await confirmDialog({ title: `„${b.name}“ löschen?`, text: 'Das Backup wird endgültig entfernt.', confirm: 'Löschen', danger: true });
    if (!ok) return;
    try {
      await api.del(`${url}/${b.id}`);
      toastOk('Backup gelöscht');
      load();
    } catch (e) {
      toastError(e);
    }
  }

  function menu(b: Backup): MenuItem[] {
    const items: MenuItem[] = [];
    if (b.status === 'ok') items.push({ label: 'Herunterladen', icon: Download, href: `${url}/${b.id}/download` });
    if (can(server, 'backups.restore') && b.status === 'ok')
      items.push({ label: 'Wiederherstellen', icon: ArchiveRestore, onclick: () => restore(b), disabled: status.state !== 'offline' || !!status.busy });
    if (can(server, 'backups.create')) items.push({ label: 'Umbenennen', icon: Pencil, onclick: () => rename(b) });
    if (can(server, 'backups.delete')) {
      items.push(b.locked ? { label: 'Sperre aufheben', icon: LockOpen, onclick: () => setLocked(b, false) } : { label: 'Sperren', icon: Lock, onclick: () => setLocked(b, true) });
      items.push({ divider: true, label: '' });
      items.push({ label: 'Löschen', icon: Trash2, danger: true, disabled: b.locked || b.status === 'running', onclick: () => remove(b) });
    }
    return items;
  }

  function openIgnore() {
    ignoreText = ignore.join('\n');
    ignoreOpen = true;
  }

  async function saveIgnore() {
    savingIgnore = true;
    try {
      const r = await api.put<{ ignore: string[] }>(`${url}/settings`, { ignore: ignoreText.split('\n') });
      ignore = r.ignore;
      ignoreOpen = false;
      toastOk('Ausnahmen gespeichert');
    } catch (e) {
      toastError(e);
    } finally {
      savingIgnore = false;
    }
  }

  let unlocked = $derived(list.filter((b) => !b.locked).length);
  let pct = $derived(progress && progress.total > 0 ? Math.min(100, (progress.bytes / progress.total) * 100) : 0);
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Backups</h2>
      <p>
        {num(list.length)} {list.length === 1 ? 'Backup' : 'Backups'} mit zusammen {bytes(totalSize)}.
        {#if max > 0}Ab {max} ungesperrten Backups wird das älteste automatisch entfernt{unlocked >= max ? ' – das passiert beim nächsten Backup' : ''}.{/if}
      </p>
    </div>
    {#if can(server, 'settings.edit')}
      <button class="btn" onclick={openIgnore}><Settings2 /> Ausnahmen</button>
    {/if}
    {#if can(server, 'backups.create')}
      <button class="btn primary" onclick={create} disabled={creating || progress?.status === 'running' || !!status.busy}><Plus /> Backup erstellen</button>
    {/if}
  </div>

  {#if progress && (progress.status === 'running' || progress.status === 'restoring')}
    <div class="panel prog">
      <div class="row">
        <strong>{progress.status === 'running' ? 'Backup wird erstellt' : 'Backup wird wiederhergestellt'}</strong>
        <span class="spacer"></span>
        <span class="small muted num">{num(progress.files)} Dateien, {bytes(progress.bytes)}{progress.total ? ' von ' + bytes(progress.total) : ''}</span>
      </div>
      <div class="meter"><span style="width:{progress.total ? pct : 100}%" class:indet={!progress.total}></span></div>
    </div>
  {/if}

  {#if status.state !== 'offline' && can(server, 'backups.restore') && list.some((b) => b.status === 'ok')}
    <p class="small faint">Zum Wiederherstellen muss der Server gestoppt sein.</p>
  {/if}

  <div class="panel">
    {#if loaded && list.length === 0}
      <div class="empty">
        <Archive />
        <h3>Noch keine Backups</h3>
        <p>Ein Backup sichert den kompletten Serverordner mit Welt, Plugins und Einstellungen als ZIP-Datei. Regelmäßige Backups richtest du unter Zeitpläne ein.</p>
      </div>
    {:else}
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Erstellt</th>
              <th class="r">Größe</th>
              <th class="r">Dateien</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {#each list as b (b.id)}
              <tr>
                <td>
                  <div class="bname">
                    {#if b.locked}<Lock size={14} class="lock" aria-label="Gesperrt" />{/if}
                    <span class="ellipsis">{b.name}</span>
                    {#if b.status === 'running'}<span class="tag info">läuft</span>{/if}
                    {#if b.status === 'failed'}<span class="tag danger" title={b.error}><TriangleAlert size={12} /> fehlgeschlagen</span>{/if}
                  </div>
                  {#if b.status === 'failed' && b.error}<div class="tiny faint err">{b.error}</div>{/if}
                </td>
                <td class="nowrap"><span title={dateTime(b.createdAt)}>{dateTime(b.createdAt)}</span> <span class="faint small">({ago(b.createdAt)})</span></td>
                <td class="r num">{b.status === 'ok' ? bytes(b.size) : '–'}</td>
                <td class="r num muted">{b.status === 'ok' ? num(b.files) : '–'}</td>
                <td class="r"><Menu items={menu(b)} /></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>
</div>

<Modal bind:open={ignoreOpen} title="Vom Backup ausnehmen" size="m">
  <div class="stack">
    <p class="small muted">Ein Muster pro Zeile, relativ zum Serverordner. Ordner enden mit <code>/</code>, <code>*</code> steht für beliebige Zeichen. Leere Liste bedeutet: alles sichern.</p>
    <textarea class="textarea mono" rows="9" bind:value={ignoreText} spellcheck="false"></textarea>
  </div>
  {#snippet footer()}
    <button class="btn" onclick={() => (ignoreOpen = false)}>Abbrechen</button>
    <button class="btn primary" onclick={saveIgnore} disabled={savingIgnore}>Speichern</button>
  {/snippet}
</Modal>

<style>
  .prog {
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .meter .indet {
    animation: pulse 1.2s ease-in-out infinite;
  }
  @keyframes pulse {
    50% {
      opacity: 0.4;
    }
  }
  .bname {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 560;
    min-width: 0;
  }
  .bname :global(.lock) {
    color: var(--warn);
    flex: none;
  }
  .err {
    margin-top: 2px;
    max-width: 420px;
  }
</style>
