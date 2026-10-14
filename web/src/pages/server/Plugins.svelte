<script lang="ts">
  import { onMount } from 'svelte';
  import { Puzzle, RefreshCw, Upload, Search, Download, Trash2, ExternalLink, ArrowUpCircle, TriangleAlert, RotateCw, Heart } from '@lucide/svelte';
  import Tabs from '../../components/Tabs.svelte';
  import Toggle from '../../components/Toggle.svelte';
  import Modal from '../../components/Modal.svelte';
  import Menu, { type MenuItem } from '../../components/Menu.svelte';
  import { api, qs, upload } from '../../lib/api';
  import { can } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog } from '../../lib/dialogs.svelte';
  import { bytes, num, ago, date } from '../../lib/format';
  import type { InstalledPlugin, MRHit, Server, Status } from '../../lib/types';

  let { server, status }: { server: Server; status: Status } = $props();

  const mcUrl = $derived(`/api/servers/${server.id}/mc`);
  let tab = $state('installed');
  let supported = $state(true);
  let dir = $state('plugins');
  let kind = $state<'plugins' | 'mods'>('plugins');
  let plugins = $state<InstalledPlugin[]>([]);
  let loaded = $state(false);
  let checking = $state(false);
  let checked = $state(false);
  let needsRestart = $state(false);
  let uploading = $state(false);

  const noun = $derived(kind === 'mods' ? 'Mod' : 'Plugin');
  const nouns = $derived(kind === 'mods' ? 'Mods' : 'Plugins');

  async function load() {
    try {
      const r = await api.get<{ supported: boolean; dir: string; plugins: InstalledPlugin[]; kind: 'plugins' | 'mods' }>(`${mcUrl}/plugins`);
      supported = r.supported;
      if (r.supported) {
        dir = r.dir;
        kind = r.kind;
      }
      const updates = new Map(plugins.filter((p) => p.update).map((p) => [p.file, p.update]));
      plugins = r.plugins.map((p) => ({ ...p, update: p.update ?? updates.get(p.file) }));
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }

  onMount(load);

  function changed() {
    if (status.state !== 'offline') needsRestart = true;
  }

  async function checkUpdates() {
    checking = true;
    try {
      const r = await api.post<{ plugins: InstalledPlugin[] }>(`${mcUrl}/plugins/check-updates`);
      plugins = r.plugins;
      checked = true;
      const n = r.plugins.filter((p) => p.update).length;
      toastOk(n ? `${n} ${n === 1 ? 'Update' : 'Updates'} verfügbar` : 'Alles auf dem neuesten Stand');
    } catch (e) {
      toastError(e);
    } finally {
      checking = false;
    }
  }

  async function toggle(p: InstalledPlugin) {
    try {
      await api.post(`${mcUrl}/plugins/toggle`, { file: p.file });
      changed();
      load();
    } catch (e) {
      toastError(e);
      load();
    }
  }

  async function remove(p: InstalledPlugin) {
    if (!(await confirmDialog({ title: `${p.name || p.file} löschen?`, text: `Die Datei ${p.file} wird entfernt. Konfigurationsdateien im Ordner bleiben erhalten.`, confirm: 'Löschen', danger: true }))) return;
    try {
      await api.del(`${mcUrl}/plugins${qs({ file: p.file })}`);
      changed();
      toastOk('Gelöscht');
      load();
    } catch (e) {
      toastError(e);
    }
  }

  let updating = $state<string[]>([]);
  async function update(p: InstalledPlugin) {
    if (!p.update || !p.projectId) return;
    updating = [...updating, p.file];
    try {
      await api.post(`${mcUrl}/modrinth/install`, { project: p.projectId, versionId: p.update.versionId, dependencies: true, replace: p.file });
      toastOk(`${p.name} aktualisiert auf ${p.update.versionNumber}`);
      changed();
      plugins = plugins.map((x) => (x.file === p.file ? { ...x, update: undefined } : x));
      load();
    } catch (e) {
      toastError(e);
    } finally {
      updating = updating.filter((f) => f !== p.file);
    }
  }

  async function updateAll() {
    for (const p of plugins.filter((x) => x.update)) await update(p);
  }

  let fileInput: HTMLInputElement;
  async function pick(ev: Event) {
    const input = ev.target as HTMLInputElement;
    const files = [...(input.files ?? [])].filter((f) => f.name.endsWith('.jar'));
    input.value = '';
    if (!files.length) return toastError('Bitte eine .jar-Datei wählen');
    uploading = true;
    try {
      await upload(`/api/servers/${server.id}/files/upload?path=${encodeURIComponent(dir)}`, files.map((file) => ({ file })));
      toastOk(files.length === 1 ? `${files[0].name} hochgeladen` : `${files.length} Dateien hochgeladen`);
      changed();
      load();
    } catch (e) {
      toastError(e);
    } finally {
      uploading = false;
    }
  }

  function menu(p: InstalledPlugin): MenuItem[] {
    const items: MenuItem[] = [];
    if (p.slug) items.push({ label: 'Auf Modrinth ansehen', icon: ExternalLink, onclick: () => window.open(`https://modrinth.com/project/${p.slug}`, '_blank', 'noopener') });
    else if (p.website) items.push({ label: 'Webseite öffnen', icon: ExternalLink, onclick: () => window.open(p.website, '_blank', 'noopener') });
    if (can(server, 'files.read')) items.push({ label: 'Herunterladen', icon: Download, href: `/api/servers/${server.id}/files/download?path=${encodeURIComponent(dir + '/' + p.file)}` });
    items.push({ divider: true, label: '' });
    items.push({ label: 'Löschen', icon: Trash2, danger: true, onclick: () => remove(p) });
    return items;
  }

  // ---- discover ----
  let query = $state('');
  let sort = $state('relevance');
  let anyVersion = $state(false);
  let hits = $state<MRHit[]>([]);
  let hitTotal = $state(0);
  let searching = $state(false);
  let searchError = $state('');
  let searched = $state(false);

  async function search(append = false) {
    searching = true;
    searchError = '';
    try {
      const r = await api.get<{ hits: MRHit[]; total: number }>(
        `${mcUrl}/modrinth/search${qs({ q: query, sort, anyVersion: anyVersion ? 1 : '', offset: append ? hits.length : 0, limit: 20 })}`,
      );
      hits = append ? [...hits, ...r.hits] : r.hits;
      hitTotal = r.total;
      searched = true;
    } catch (e: any) {
      searchError = e.message;
    } finally {
      searching = false;
    }
  }

  let timer: ReturnType<typeof setTimeout>;
  $effect(() => {
    query;
    sort;
    anyVersion;
    if (tab !== 'discover' || !supported) return;
    clearTimeout(timer);
    timer = setTimeout(() => search(), query ? 300 : 0);
  });

  let installedProjects = $derived(new Set(plugins.map((p) => p.projectId).filter(Boolean)));

  // ---- install dialog ----
  interface MRVersion {
    id: string;
    name: string;
    version_number: string;
    version_type: string;
    game_versions: string[];
    loaders: string[];
    date_published: string;
    downloads: number;
    files: { filename: string; size: number; primary: boolean }[];
    dependencies: { dependency_type: string }[];
  }
  let installOpen = $state(false);
  let target = $state<MRHit | null>(null);
  let versions = $state<MRVersion[]>([]);
  let versionsLoading = $state(false);
  let versionsError = $state('');
  let chosen = $state('');
  let withDeps = $state(true);
  let installing = $state(false);

  async function openInstall(h: MRHit) {
    target = h;
    installOpen = true;
    versions = [];
    chosen = '';
    versionsError = '';
    versionsLoading = true;
    try {
      versions = await api.get<MRVersion[]>(`${mcUrl}/modrinth/versions${qs({ project: h.project_id, anyVersion: anyVersion ? 1 : '' })}`);
      chosen = (versions.find((v) => v.version_type === 'release') ?? versions[0])?.id ?? '';
    } catch (e: any) {
      versionsError = e.message;
    } finally {
      versionsLoading = false;
    }
  }

  async function install() {
    if (!target) return;
    installing = true;
    try {
      const r = await api.post<{ installed: string[] }>(`${mcUrl}/modrinth/install`, { project: target.project_id, versionId: chosen, dependencies: withDeps });
      toastOk(r.installed.length > 1 ? `${target.title} und ${r.installed.length - 1} Abhängigkeit(en) installiert` : `${target.title} installiert`);
      installOpen = false;
      changed();
      load();
    } catch (e) {
      toastError(e);
    } finally {
      installing = false;
    }
  }

  async function restart() {
    try {
      await api.post(`/api/servers/${server.id}/power`, { action: 'restart' });
      needsRestart = false;
    } catch (e) {
      toastError(e);
    }
  }

  const sortLabels: Record<string, string> = { relevance: 'Relevanz', downloads: 'Downloads', follows: 'Beliebtheit', newest: 'Neueste', updated: 'Zuletzt aktualisiert' };
  const typeLabels: Record<string, string> = { release: 'Release', beta: 'Beta', alpha: 'Alpha' };
  let updates = $derived(plugins.filter((p) => p.update).length);
</script>

<input bind:this={fileInput} type="file" accept=".jar" multiple hidden onchange={pick} />

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>{nouns}</h2>
      <p>{kind === 'mods' ? 'Mods für den Server aus dem Ordner mods/.' : 'Plugins aus dem Ordner plugins/.'} Neue {nouns} findest du direkt auf Modrinth.</p>
    </div>
    {#if supported}
      {#if can(server, 'files.write')}
        <button class="btn" onclick={() => fileInput.click()} disabled={uploading}><Upload /> JAR hochladen</button>
      {/if}
      <button class="btn" onclick={checkUpdates} disabled={checking || !plugins.length}><RefreshCw class={checking ? 'spin' : ''} /> Nach Updates suchen</button>
    {/if}
  </div>

  {#if needsRestart}
    <div class="notice warn">
      <TriangleAlert />
      <span style="flex:1">Änderungen an {nouns} werden erst nach einem Neustart des Servers aktiv.</span>
      {#if can(server, 'power.stop')}<button class="btn sm" onclick={restart}><RotateCw /> Jetzt neu starten</button>{/if}
    </div>
  {/if}

  {#if loaded && !supported}
    <div class="panel">
      <div class="empty">
        <Puzzle />
        <h3>Keine Erweiterungen möglich</h3>
        <p>Vanilla-Server laden weder Plugins noch Mods. Unter „Start &amp; Version“ kannst du auf Paper (Plugins) oder Fabric (Mods) wechseln.</p>
      </div>
    </div>
  {:else}
    <Tabs items={[{ id: 'installed', label: 'Installiert', count: plugins.length }, { id: 'discover', label: 'Entdecken' }]} bind:value={tab} />

    {#if tab === 'installed'}
      {#if updates > 0}
        <div class="notice info">
          <ArrowUpCircle />
          <span style="flex:1">{updates} {updates === 1 ? 'Update ist' : 'Updates sind'} verfügbar.</span>
          <button class="btn sm" onclick={updateAll} disabled={updating.length > 0}>Alle aktualisieren</button>
        </div>
      {/if}
      <div class="panel">
        {#if loaded && plugins.length === 0}
          <div class="empty">
            <Puzzle />
            <h3>Noch keine {nouns}</h3>
            <p>Suche unter „Entdecken“ nach {nouns} oder lade eine JAR-Datei hoch.</p>
            <button class="btn primary" onclick={() => (tab = 'discover')}><Search /> {nouns} entdecken</button>
          </div>
        {:else}
          <ul class="plist">
            {#each plugins as p (p.file)}
              <li class:off={!p.enabled}>
                <span class="icon">
                  {#if p.iconUrl}<img src={p.iconUrl} alt="" width="40" height="40" loading="lazy" />{:else}<Puzzle />{/if}
                </span>
                <div class="pinfo">
                  <div class="pname">
                    <strong class="ellipsis">{p.name || p.file}</strong>
                    {#if p.version}<span class="small muted">{p.version}</span>{/if}
                    {#if !p.enabled}<span class="tag">deaktiviert</span>{/if}
                    {#if p.update}<span class="tag info">Update {p.update.versionNumber}</span>{/if}
                  </div>
                  {#if p.description}<p class="small muted ellipsis">{p.description}</p>{/if}
                  <p class="tiny faint ellipsis">
                    {p.file}, {bytes(p.size)}{#if p.authors?.length}, von {p.authors.slice(0, 3).join(', ')}{/if}
                  </p>
                </div>
                <div class="pact">
                  {#if p.update}
                    <button class="btn sm primary" onclick={() => update(p)} disabled={updating.includes(p.file)}>Aktualisieren</button>
                  {/if}
                  <Toggle checked={p.enabled} label="" ariaLabel="{p.name || p.file} aktivieren" onchange={() => toggle(p)} />
                  <Menu items={menu(p)} />
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
      {#if checked && !updates && plugins.length}<p class="tiny faint">Updates werden über Modrinth gesucht. {nouns} von anderen Quellen werden dabei nicht erkannt.</p>{/if}
    {:else}
      <div class="tools">
        <div class="search">
          <Search size={15} />
          <input class="input" placeholder="{nouns} auf Modrinth suchen" bind:value={query} aria-label="Suchen" />
        </div>
        <select class="select sortsel" bind:value={sort} aria-label="Sortierung">
          {#each Object.entries(sortLabels) as [k, l] (k)}<option value={k}>{l}</option>{/each}
        </select>
        <label class="check small"><input type="checkbox" bind:checked={anyVersion} /> Auch für andere Minecraft-Versionen</label>
      </div>

      {#if searchError}
        <div class="notice danger"><TriangleAlert /><span>{searchError}</span></div>
      {:else if searched && hits.length === 0}
        <div class="panel"><div class="empty"><p>Nichts gefunden{query ? ` für „${query}“` : ''}.</p></div></div>
      {/if}

      <div class="hits">
        {#each hits as h (h.project_id)}
          <article class="panel hit">
            <div class="htop">
              <span class="icon">{#if h.icon_url}<img src={h.icon_url} alt="" width="44" height="44" loading="lazy" />{:else}<Puzzle />{/if}</span>
              <div class="hmeta">
                <a class="htitle" href="https://modrinth.com/{h.project_type}/{h.slug}" target="_blank" rel="noopener noreferrer">{h.title}</a>
                <span class="tiny faint">von {h.author}</span>
              </div>
            </div>
            <p class="small muted desc">{h.description}</p>
            <div class="hfoot">
              <span class="tiny faint num" title="Downloads"><Download size={12} /> {num(h.downloads)}</span>
              <span class="tiny faint num" title="Follower"><Heart size={12} /> {num(h.follows)}</span>
              <span class="tiny faint">{ago(Date.parse(h.date_modified) / 1000)}</span>
              <span class="spacer"></span>
              {#if installedProjects.has(h.project_id)}
                <span class="tag accent">Installiert</span>
              {:else}
                <button class="btn sm primary" onclick={() => openInstall(h)}>Installieren</button>
              {/if}
            </div>
          </article>
        {/each}
      </div>
      {#if hits.length < hitTotal}
        <div class="center"><button class="btn" onclick={() => search(true)} disabled={searching}>Mehr laden</button></div>
      {/if}
      <p class="tiny faint">Ergebnisse von Modrinth, gefiltert auf {server.settings?.flavor} {anyVersion ? '' : server.settings?.version}.</p>
    {/if}
  {/if}
</div>

<Modal bind:open={installOpen} title={target ? `${target.title} installieren` : 'Installieren'} size="m">
  {#if versionsLoading}
    <p class="muted">Versionen werden geladen …</p>
  {:else if versionsError}
    <div class="notice danger"><TriangleAlert /><span>{versionsError}</span></div>
  {:else if versions.length === 0}
    <p class="muted">Keine passende Version für {server.settings?.flavor} {server.settings?.version} gefunden. Aktiviere „Auch für andere Minecraft-Versionen“, um ältere Versionen zu sehen.</p>
  {:else}
    <div class="stack">
      <div class="vlist" role="radiogroup" aria-label="Version">
        {#each versions.slice(0, 30) as v (v.id)}
          <label class="ver" class:on={chosen === v.id}>
            <input type="radio" name="ver" value={v.id} bind:group={chosen} />
            <span class="vn">
              <strong>{v.version_number}</strong>
              {#if v.version_type !== 'release'}<span class="tag warn">{typeLabels[v.version_type] ?? v.version_type}</span>{/if}
            </span>
            <span class="tiny faint">
              {v.game_versions.slice(-3).join(', ')}{v.game_versions.length > 3 ? ' …' : ''}, {v.loaders.join(', ')}, {date(Date.parse(v.date_published) / 1000)}
            </span>
          </label>
        {/each}
      </div>
      <label class="check small"><input type="checkbox" bind:checked={withDeps} /> Benötigte Abhängigkeiten automatisch mitinstallieren</label>
    </div>
  {/if}
  {#snippet footer()}
    <button class="btn" onclick={() => (installOpen = false)}>Abbrechen</button>
    <button class="btn primary" onclick={install} disabled={!chosen || installing}>{installing ? 'Wird installiert …' : 'Installieren'}</button>
  {/snippet}
</Modal>

<style>
  .plist {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .plist li {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--line);
  }
  .plist li:last-child {
    border-bottom: none;
  }
  .plist li.off .pinfo,
  .plist li.off .icon {
    opacity: 0.55;
  }
  .icon {
    width: 40px;
    height: 40px;
    flex: none;
    display: grid;
    place-items: center;
    border-radius: 9px;
    background: var(--surface-2);
    color: var(--text-3);
    overflow: hidden;
  }
  .icon img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .pinfo {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .pname {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .pact {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .search {
    position: relative;
    flex: 1;
    min-width: 220px;
    max-width: 420px;
  }
  .search :global(svg) {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-3);
  }
  .search .input {
    padding-left: 32px;
  }
  .sortsel {
    width: auto;
  }
  .hits {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(290px, 1fr));
    gap: 14px;
  }
  .hit {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 16px;
  }
  .htop {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .hit .icon {
    width: 44px;
    height: 44px;
  }
  .hmeta {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .htitle {
    font-weight: 650;
    color: var(--text);
  }
  .desc {
    flex: 1;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .hfoot {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .hfoot span {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .center {
    display: flex;
    justify-content: center;
  }
  .vlist {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 360px;
    overflow-y: auto;
  }
  .ver {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 9px 12px;
    border: 1px solid var(--line-2);
    border-radius: var(--r-m);
    cursor: pointer;
    position: relative;
  }
  .ver input {
    position: absolute;
    opacity: 0;
  }
  .ver.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .ver:has(input:focus-visible) {
    outline: 2px solid var(--accent);
  }
  .vn {
    display: flex;
    align-items: center;
    gap: 8px;
  }
</style>
