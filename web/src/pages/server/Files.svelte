<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import {
    Folder, File as FileIc, FileText, FileCode, FileArchive, FileImage, Package, Upload, FolderUp, FolderPlus, FilePlus, Download,
    Pencil, Copy, Trash2, Move, Archive, ArchiveRestore, ChevronRight, House, ArrowLeft, Save, Search, RefreshCw, X, Link2, ArrowUp, ArrowDown,
  } from '@lucide/svelte';
  import Menu, { type MenuItem } from '../../components/Menu.svelte';
  import { api, upload, ApiError } from '../../lib/api';
  import { router, go } from '../../lib/router.svelte';
  import { can } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog, promptDialog } from '../../lib/dialogs.svelte';
  import { bytes, dateTime, ago, num } from '../../lib/format';
  import type { FileEntry, Server } from '../../lib/types';

  let { server }: { server: Server } = $props();

  const base = $derived(`/api/servers/${server.id}/files`);
  const canWrite = $derived(can(server, 'files.write'));
  const canDelete = $derived(can(server, 'files.delete'));

  let path = $derived(router.route.query.get('path') ?? '');
  let editPath = $derived(router.route.query.get('edit'));

  let entries = $state<FileEntry[]>([]);
  let loading = $state(true);
  let error = $state('');
  let selected = $state<string[]>([]);
  let filter = $state('');
  let sortKey = $state<'name' | 'size' | 'modTime'>('name');
  let sortAsc = $state(true);
  let uploading = $state<{ loaded: number; total: number; count: number } | null>(null);
  let dragDepth = $state(0);

  // editor state
  let content = $state('');
  let original = $state('');
  let modTime = $state(0);
  let editLoading = $state(false);
  let editError = $state('');
  let saving = $state(false);
  let dirty = $derived(editPath !== null && content !== original);

  const join = (dir: string, name: string) => (dir ? `${dir}/${name}` : name);
  const parentOf = (p: string) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '');
  const baseName = (p: string) => p.slice(p.lastIndexOf('/') + 1);

  function href(dir: string, edit?: string) {
    const q = new URLSearchParams();
    if (dir) q.set('path', dir);
    if (edit) q.set('edit', edit);
    const s = q.toString();
    return `/servers/${server.id}/files${s ? '?' + s : ''}`;
  }

  async function load() {
    const p = path;
    loading = true;
    error = '';
    try {
      const r = await api.get<{ path: string; entries: FileEntry[] }>(`${base}?path=${encodeURIComponent(p)}`);
      if (p !== path) return;
      entries = r.entries;
      selected = selected.filter((s) => r.entries.some((e) => e.path === s));
    } catch (e: any) {
      error = e.message;
      entries = [];
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    path;
    untrack(() => {
      selected = [];
      filter = '';
      load();
    });
  });

  async function openEditor(p: string) {
    editLoading = true;
    editError = '';
    content = original = '';
    try {
      const r = await api.get<{ content: string; modTime: number }>(`${base}/content?path=${encodeURIComponent(p)}`);
      content = original = r.content;
      modTime = r.modTime;
    } catch (e: any) {
      editError = e.message;
    } finally {
      editLoading = false;
    }
  }

  $effect(() => {
    const p = editPath;
    if (p !== null) untrack(() => openEditor(p));
  });

  let crumbs = $derived.by(() => {
    const parts = path ? path.split('/') : [];
    return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') }));
  });

  let shown = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    const list = f ? entries.filter((e) => e.name.toLowerCase().includes(f)) : [...entries];
    const dir = sortAsc ? 1 : -1;
    list.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      if (sortKey === 'size') return (a.size - b.size) * dir;
      if (sortKey === 'modTime') return (a.modTime - b.modTime) * dir;
      return a.name.localeCompare(b.name, 'de', { numeric: true, sensitivity: 'base' }) * dir;
    });
    return list;
  });

  function sortBy(k: typeof sortKey) {
    if (sortKey === k) sortAsc = !sortAsc;
    else {
      sortKey = k;
      sortAsc = k === 'name';
    }
  }

  let allSelected = $derived(shown.length > 0 && shown.every((e) => selected.includes(e.path)));
  function toggleAll() {
    selected = allSelected ? [] : shown.map((e) => e.path);
  }
  function toggle(p: string) {
    selected = selected.includes(p) ? selected.filter((x) => x !== p) : [...selected, p];
  }

  function iconFor(e: FileEntry) {
    if (e.isDir) return Folder;
    const n = e.name.toLowerCase();
    if (n.endsWith('.jar')) return Package;
    if (e.archive || /\.(zip|tar|gz|tgz|7z|rar)$/.test(n)) return FileArchive;
    if (/\.(png|jpe?g|gif|webp|svg|ico)$/.test(n)) return FileImage;
    if (/\.(json|ya?ml|toml|properties|cfg|conf|ini|js|ts|sh|bat|mcmeta|xml)$/.test(n)) return FileCode;
    if (/\.(txt|log|md)$/.test(n)) return FileText;
    return FileIc;
  }

  function open(e: FileEntry) {
    if (e.isDir) go(href(e.path));
    else if (e.editable) go(href(path, e.path));
    else location.href = `${base}/download?path=${encodeURIComponent(e.path)}`;
  }

  async function op(name: string, body: object, okText = '') {
    try {
      await api.post(`${base}/${name}`, body);
      if (okText) toastOk(okText);
      return true;
    } catch (e) {
      toastError(e);
      return false;
    } finally {
      load();
    }
  }

  async function newFolder() {
    const n = await promptDialog({ title: 'Neuer Ordner', label: 'Name', placeholder: 'plugins', confirm: 'Anlegen' });
    if (n?.trim()) op('mkdir', { path: join(path, n.trim()) });
  }

  async function newFile() {
    const n = await promptDialog({ title: 'Neue Datei', label: 'Dateiname', placeholder: 'notizen.txt', confirm: 'Anlegen' });
    if (!n?.trim()) return;
    const p = join(path, n.trim());
    if (await op('touch', { path: p })) go(href(path, p));
  }

  async function rename(e: FileEntry) {
    const n = await promptDialog({ title: 'Umbenennen', label: 'Neuer Name', value: e.name, confirm: 'Umbenennen' });
    if (n?.trim() && n.trim() !== e.name) op('rename', { from: e.path, to: join(parentOf(e.path), n.trim()) });
  }

  async function duplicate(e: FileEntry) {
    const dot = e.isDir ? -1 : e.name.lastIndexOf('.');
    const suggestion = dot > 0 ? `${e.name.slice(0, dot)}-kopie${e.name.slice(dot)}` : `${e.name}-kopie`;
    const n = await promptDialog({ title: 'Kopieren', label: 'Name der Kopie', value: suggestion, confirm: 'Kopieren' });
    if (n?.trim()) op('copy', { from: e.path, to: join(parentOf(e.path), n.trim()) }, 'Kopie angelegt');
  }

  async function move(paths: string[]) {
    const dest = await promptDialog({
      title: paths.length === 1 ? `„${baseName(paths[0])}“ verschieben` : `${paths.length} Einträge verschieben`,
      label: 'Zielordner',
      value: path,
      hint: 'Pfad relativ zum Serverordner. Leer lassen für den Hauptordner.',
      confirm: 'Verschieben',
      optional: true,
    });
    if (dest === null) return;
    const d = dest.trim().replace(/^\/+|\/+$/g, '');
    let okCount = 0;
    for (const p of paths) {
      try {
        await api.post(`${base}/rename`, { from: p, to: join(d, baseName(p)) });
        okCount++;
      } catch (e) {
        toastError(e);
        break;
      }
    }
    if (okCount) toastOk(okCount === 1 ? 'Verschoben' : `${okCount} Einträge verschoben`);
    selected = [];
    load();
  }

  async function remove(paths: string[]) {
    const ok = await confirmDialog({
      title: paths.length === 1 ? `„${baseName(paths[0])}“ löschen?` : `${paths.length} Einträge löschen?`,
      text: 'Gelöschte Dateien lassen sich nur über ein Backup zurückholen.',
      confirm: 'Löschen',
      danger: true,
    });
    if (!ok) return;
    if (await op('delete', { paths }, paths.length === 1 ? 'Gelöscht' : `${paths.length} Einträge gelöscht`)) selected = [];
  }

  async function compress(paths: string[]) {
    if (await op('compress', { paths }, 'Archiv erstellt')) selected = [];
  }

  async function extract(e: FileEntry) {
    op('extract', { path: e.path, dest: parentOf(e.path) }, `„${e.name}“ entpackt`);
  }

  function downloadUrl(paths: string[]) {
    return `${base}/download?` + paths.map((p) => 'paths=' + encodeURIComponent(p)).join('&');
  }

  function menuFor(e: FileEntry): MenuItem[] {
    const items: MenuItem[] = [];
    if (e.isDir) items.push({ label: 'Öffnen', icon: Folder, onclick: () => open(e) });
    else if (e.editable) items.push({ label: canWrite ? 'Bearbeiten' : 'Ansehen', icon: Pencil, onclick: () => open(e) });
    items.push({ label: e.isDir ? 'Als ZIP herunterladen' : 'Herunterladen', icon: Download, href: downloadUrl([e.path]) });
    if (canWrite) {
      items.push({ divider: true, label: '' });
      items.push({ label: 'Umbenennen', icon: Pencil, onclick: () => rename(e) });
      items.push({ label: 'Kopieren', icon: Copy, onclick: () => duplicate(e) });
      items.push({ label: 'Verschieben', icon: Move, onclick: () => move([e.path]) });
      if (e.archive) items.push({ label: 'Hier entpacken', icon: ArchiveRestore, onclick: () => extract(e) });
      else items.push({ label: 'Komprimieren', icon: Archive, onclick: () => compress([e.path]) });
    }
    if (canDelete) {
      items.push({ divider: true, label: '' });
      items.push({ label: 'Löschen', icon: Trash2, danger: true, onclick: () => remove([e.path]) });
    }
    return items;
  }

  // ---- uploads ----
  let fileInput: HTMLInputElement;
  let dirInput: HTMLInputElement;

  async function doUpload(files: { file: File; path?: string }[]) {
    if (!files.length) return;
    const total = files.reduce((a, f) => a + f.file.size, 0);
    uploading = { loaded: 0, total, count: files.length };
    try {
      const r = await upload(`${base}/upload?path=${encodeURIComponent(path)}`, files, (loaded, t) => {
        uploading = { loaded, total: t, count: files.length };
      });
      const n = r?.uploaded?.length ?? files.length;
      toastOk(n === 1 ? 'Datei hochgeladen' : `${num(n)} Dateien hochgeladen`);
    } catch (e) {
      toastError(e);
    } finally {
      uploading = null;
      load();
    }
  }

  function pickFiles(ev: Event) {
    const input = ev.target as HTMLInputElement;
    const list = [...(input.files ?? [])].map((file) => ({ file }));
    input.value = '';
    doUpload(list);
  }

  function pickDir(ev: Event) {
    const input = ev.target as HTMLInputElement;
    const list = [...(input.files ?? [])].map((file) => ({ file, path: file.webkitRelativePath || file.name }));
    input.value = '';
    doUpload(list);
  }

  async function walk(entry: FileSystemEntry, prefix: string, out: { file: File; path?: string }[]) {
    if (entry.isFile) {
      const file = await new Promise<File>((res, rej) => (entry as FileSystemFileEntry).file(res, rej));
      out.push({ file, path: prefix + file.name });
    } else if (entry.isDirectory) {
      const reader = (entry as FileSystemDirectoryEntry).createReader();
      for (;;) {
        const batch = await new Promise<FileSystemEntry[]>((res, rej) => reader.readEntries(res, rej));
        if (!batch.length) break;
        for (const c of batch) await walk(c, prefix + entry.name + '/', out);
      }
    }
  }

  const hasFiles = (e: DragEvent) => !!e.dataTransfer && [...e.dataTransfer.types].includes('Files');

  function dragEnter(e: DragEvent) {
    if (!canWrite || !hasFiles(e) || editPath !== null) return;
    e.preventDefault();
    dragDepth++;
  }
  function dragOver(e: DragEvent) {
    if (!canWrite || !hasFiles(e) || editPath !== null) return;
    e.preventDefault();
    e.dataTransfer!.dropEffect = 'copy';
  }
  function dragLeave() {
    if (dragDepth > 0) dragDepth--;
  }
  async function drop(e: DragEvent) {
    if (!canWrite || !hasFiles(e) || editPath !== null) return;
    e.preventDefault();
    dragDepth = 0;
    const out: { file: File; path?: string }[] = [];
    const items = e.dataTransfer?.items;
    const roots = items ? [...items].map((i) => i.webkitGetAsEntry?.()).filter((x): x is FileSystemEntry => !!x) : [];
    if (roots.length) {
      for (const r of roots) await walk(r, '', out);
    } else {
      for (const f of e.dataTransfer?.files ?? []) out.push({ file: f });
    }
    doUpload(out);
  }

  // ---- editor ----
  async function save(force = false) {
    if (editPath === null || saving || !canWrite) return;
    saving = true;
    try {
      const r = await api.put<{ modTime: number }>(`${base}/content?path=${encodeURIComponent(editPath)}`, {
        content,
        expectModTime: force ? 0 : modTime,
      });
      original = content;
      modTime = r.modTime ?? modTime;
      toastOk('Gespeichert');
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        saving = false;
        const ok = await confirmDialog({
          title: 'Datei wurde zwischenzeitlich geändert',
          text: 'Seit dem Öffnen hat jemand (oder der Server selbst) die Datei verändert. Möchtest du sie trotzdem mit deiner Fassung überschreiben?',
          confirm: 'Überschreiben',
          danger: true,
        });
        if (ok) return save(true);
        return;
      }
      toastError(e);
    } finally {
      saving = false;
    }
  }

  async function closeEditor() {
    if (dirty) {
      const ok = await confirmDialog({ title: 'Änderungen verwerfen?', text: 'Die Datei hat ungespeicherte Änderungen.', confirm: 'Verwerfen', danger: true });
      if (!ok) return;
    }
    content = original = '';
    go(href(path));
  }

  onMount(() => {
    const warn = (e: BeforeUnloadEvent) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = '';
      }
    };
    addEventListener('beforeunload', warn);
    return () => removeEventListener('beforeunload', warn);
  });
</script>

<input bind:this={fileInput} type="file" multiple hidden onchange={pickFiles} />
<input bind:this={dirInput} type="file" multiple hidden onchange={pickDir} webkitdirectory />

{#if editPath !== null}
  <div class="editor">
    <div class="ebar">
      <button class="btn ghost sm icon" onclick={closeEditor} aria-label="Zurück zur Dateiliste" title="Zurück"><ArrowLeft /></button>
      <div class="epath">
        <span class="faint mono small ellipsis">{parentOf(editPath) ? parentOf(editPath) + '/' : ''}</span><strong class="mono">{baseName(editPath)}</strong>
        {#if dirty}<span class="dirty" title="Ungespeicherte Änderungen"></span>{/if}
      </div>
      <span class="spacer"></span>
      {#if modTime}<span class="tiny faint nowrap">Stand {dateTime(modTime)}</span>{/if}
      <a class="btn sm" href={downloadUrl([editPath])} download data-external><Download /> Herunterladen</a>
      {#if canWrite}
        <button class="btn primary sm" onclick={() => save()} disabled={!dirty || saving}><Save /> Speichern</button>
      {/if}
    </div>
    {#if editLoading}
      <div class="panel eload"><span class="muted">Datei wird geladen …</span></div>
    {:else if editError}
      <div class="notice danger"><span>{editError}</span></div>
    {:else}
      <div class="ebody">
        {#key editPath}
          {#await import('../../components/CodeEditor.svelte')}
            <div class="eload muted">Editor wird geladen …</div>
          {:then { default: CodeEditor }}
            <CodeEditor bind:value={content} filename={editPath} readonly={!canWrite} onsave={() => save()} />
          {/await}
        {/key}
      </div>
      <p class="tiny faint keys"><span class="kbd">Strg</span> + <span class="kbd">S</span> speichert, <span class="kbd">Strg</span> + <span class="kbd">F</span> sucht in der Datei.</p>
    {/if}
  </div>
{:else}
  <div
    class="files"
    role="region"
    aria-label="Dateien"
    ondragenter={dragEnter}
    ondragover={dragOver}
    ondragleave={dragLeave}
    ondrop={drop}
  >
    <div class="bar">
      <nav class="crumbs" aria-label="Pfad">
        <a href={href('')} class="crumb root" class:current={!path}><House size={15} /> Serverordner</a>
        {#each crumbs as c (c.path)}
          <ChevronRight size={14} class="sep-ic" />
          <a href={href(c.path)} class="crumb" class:current={c.path === path}>{c.name}</a>
        {/each}
      </nav>
      <span class="spacer"></span>
      <div class="search">
        <Search size={15} />
        <input class="input" placeholder="In diesem Ordner filtern" bind:value={filter} aria-label="Dateien filtern" />
      </div>
      <button class="btn icon" onclick={load} title="Neu laden" aria-label="Neu laden"><RefreshCw /></button>
      {#if canWrite}
        <Menu
          text="Neu"
          icon={FolderPlus}
          items={[
            { label: 'Neuer Ordner', icon: FolderPlus, onclick: newFolder },
            { label: 'Neue Datei', icon: FilePlus, onclick: newFile },
            { label: 'Ordner hochladen', icon: FolderUp, onclick: () => dirInput.click() },
          ]}
        />
        <button class="btn primary" onclick={() => fileInput.click()}><Upload /> Hochladen</button>
      {/if}
    </div>

    {#if uploading}
      <div class="uploading panel">
        <span class="small">{uploading.count === 1 ? 'Datei wird' : `${num(uploading.count)} Dateien werden`} hochgeladen …</span>
        <span class="spacer"></span>
        <span class="small num muted">{bytes(uploading.loaded)} / {bytes(uploading.total)}</span>
        <div class="meter"><span style="width:{uploading.total ? (uploading.loaded / uploading.total) * 100 : 0}%"></span></div>
      </div>
    {/if}

    {#if selected.length}
      <div class="selbar">
        <strong class="small">{selected.length} ausgewählt</strong>
        <span class="spacer"></span>
        <a class="btn sm" href={downloadUrl(selected)} download data-external><Download /> Herunterladen</a>
        {#if canWrite}
          <button class="btn sm" onclick={() => compress(selected)}><Archive /> Komprimieren</button>
          <button class="btn sm" onclick={() => move(selected)}><Move /> Verschieben</button>
        {/if}
        {#if canDelete}<button class="btn sm danger" onclick={() => remove(selected)}><Trash2 /> Löschen</button>{/if}
        <button class="btn ghost sm icon" onclick={() => (selected = [])} aria-label="Auswahl aufheben" title="Auswahl aufheben"><X /></button>
      </div>
    {/if}

    <div class="panel list" class:drag={dragDepth > 0}>
      {#if error}
        <div class="empty">
          <h3>Ordner nicht verfügbar</h3>
          <p>{error}</p>
          <a class="btn" href={href('')}>Zum Serverordner</a>
        </div>
      {:else}
        <div class="table-wrap">
          <table class="table">
            <thead>
              <tr>
                <th class="cb"><input type="checkbox" checked={allSelected} onchange={toggleAll} aria-label="Alle auswählen" /></th>
                <th class="sortable" onclick={() => sortBy('name')}>
                  Name {#if sortKey === 'name'}{#if sortAsc}<ArrowUp size={12} />{:else}<ArrowDown size={12} />{/if}{/if}
                </th>
                <th class="sortable r size" onclick={() => sortBy('size')}>
                  Größe {#if sortKey === 'size'}{#if sortAsc}<ArrowUp size={12} />{:else}<ArrowDown size={12} />{/if}{/if}
                </th>
                <th class="sortable mod" onclick={() => sortBy('modTime')}>
                  Geändert {#if sortKey === 'modTime'}{#if sortAsc}<ArrowUp size={12} />{:else}<ArrowDown size={12} />{/if}{/if}
                </th>
                <th class="act"></th>
              </tr>
            </thead>
            <tbody>
              {#if path}
                <tr class="up">
                  <td></td>
                  <td colspan="4"><a href={href(parentOf(path))} class="name"><span class="ic"><Folder /></span><span class="muted">..</span></a></td>
                </tr>
              {/if}
              {#each shown as e (e.path)}
                {@const Icon = iconFor(e)}
                <tr class:sel={selected.includes(e.path)}>
                  <td class="cb"><input type="checkbox" checked={selected.includes(e.path)} onchange={() => toggle(e.path)} aria-label="{e.name} auswählen" /></td>
                  <td class="namecell">
                    <button class="name" class:dir={e.isDir} onclick={() => open(e)} title={e.mode}>
                      <span class="ic"><Icon /></span>
                      <span class="ellipsis">{e.name}</span>
                      {#if e.symlink}<Link2 size={13} class="faint" />{/if}
                    </button>
                  </td>
                  <td class="r num muted size">{e.isDir ? '' : bytes(e.size)}</td>
                  <td class="muted mod nowrap" title={dateTime(e.modTime)}>{ago(e.modTime)}</td>
                  <td class="act"><Menu items={menuFor(e)} /></td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        {#if !loading && shown.length === 0}
          <div class="empty">
            {#if filter}
              <p>Keine Einträge passen zu „{filter}“.</p>
            {:else}
              <h3>Dieser Ordner ist leer</h3>
              {#if canWrite}<p>Ziehe Dateien oder Ordner hierher, um sie hochzuladen.</p>{/if}
            {/if}
          </div>
        {/if}
      {/if}
      {#if dragDepth > 0}
        <div class="dropzone"><Upload /><strong>Loslassen zum Hochladen</strong><span class="small">nach /{path}</span></div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .files {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .crumbs {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 2px;
    min-width: 0;
    font-size: 14px;
  }
  .crumb {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 7px;
    border-radius: var(--r-s);
    color: var(--text-2);
    font-weight: 560;
  }
  .crumb:hover {
    background: var(--surface-2);
    color: var(--text);
    text-decoration: none;
  }
  .crumb.current {
    color: var(--text);
    font-weight: 650;
  }
  .crumbs :global(.sep-ic) {
    color: var(--text-3);
  }
  .search {
    position: relative;
    width: 230px;
  }
  .search :global(svg) {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-3);
    pointer-events: none;
  }
  .search .input {
    padding-left: 32px;
    height: 34px;
  }
  .uploading {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
    padding: 11px 16px;
  }
  .uploading .meter {
    flex-basis: 100%;
  }
  .selbar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    padding: 8px 10px 8px 16px;
    background: var(--accent-soft);
    border-radius: var(--r-m);
  }
  .list {
    position: relative;
    overflow: hidden;
  }
  .list.drag {
    border-color: var(--accent);
  }
  .table td,
  .table th {
    padding-top: 6px;
    padding-bottom: 6px;
  }
  .cb {
    width: 38px;
    padding-right: 0 !important;
  }
  .cb input {
    accent-color: var(--accent);
    width: 15px;
    height: 15px;
    vertical-align: middle;
  }
  .table th :global(svg) {
    vertical-align: -1px;
  }
  .size {
    width: 110px;
    white-space: nowrap;
  }
  .mod {
    width: 170px;
    white-space: nowrap;
  }
  .act {
    width: 44px;
    text-align: right;
  }
  tr.sel td {
    background: color-mix(in srgb, var(--accent-soft) 70%, transparent);
  }
  .namecell {
    max-width: 0;
    width: 100%;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-width: 0;
    padding: 4px 0;
    background: none;
    border: none;
    color: var(--text);
    font: inherit;
    font-size: 14px;
    text-align: left;
    cursor: pointer;
  }
  a.name:hover {
    text-decoration: none;
  }
  .name:hover .ellipsis {
    color: var(--accent);
  }
  .name.dir {
    font-weight: 600;
  }
  .ic {
    display: grid;
    place-items: center;
    flex: none;
    color: var(--text-3);
  }
  .ic :global(svg) {
    width: 17px;
    height: 17px;
  }
  .name.dir .ic {
    color: var(--copper);
  }
  .dropzone {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    background: color-mix(in srgb, var(--bg) 82%, transparent);
    border: 2px dashed var(--accent);
    border-radius: var(--r-l);
    color: var(--accent);
    pointer-events: none;
  }
  .dropzone :global(svg) {
    width: 28px;
    height: 28px;
  }
  .editor {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .ebar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .epath {
    display: flex;
    align-items: center;
    min-width: 0;
    font-size: 14.5px;
  }
  .dirty {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    background: var(--warn);
    margin-left: 10px;
    flex: none;
  }
  .ebody {
    height: calc(100vh - var(--head-h, 93px) - 124px);
    min-height: 360px;
    border: 1px solid var(--line);
    border-radius: var(--r-l);
    overflow: hidden;
  }
  .eload {
    padding: 60px;
    text-align: center;
  }
  .keys {
    margin-top: -2px;
  }
  @media (max-width: 760px) {
    .mod,
    .size {
      display: none;
    }
    .search {
      width: 100%;
      order: 10;
    }
  }
</style>
