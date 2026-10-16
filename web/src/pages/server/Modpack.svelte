<script lang="ts">
  import { onMount } from 'svelte';
  import { Package, Download, Search, Check, X, TriangleAlert, Server as ServerIc, Monitor, FileArchive, ExternalLink, Info as InfoIc } from '@lucide/svelte';
  import Modal from '../../components/Modal.svelte';
  import Toggle from '../../components/Toggle.svelte';
  import { api, qs } from '../../lib/api';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { bytes, num, ago } from '../../lib/format';
  import { flavorLabel } from '../../lib/mc';
  import type { MRHit, Server } from '../../lib/types';

  let { server }: { server: Server } = $props();

  interface PackMod {
    file: string;
    name: string;
    version: string;
    iconUrl?: string;
    projectId?: string;
    slug?: string;
    size: number;
    source: 'modrinth' | 'file';
    client: 'required' | 'optional' | 'unsupported';
  }
  interface ClientMod {
    projectId: string;
    slug: string;
    title: string;
    description?: string;
    iconUrl?: string;
  }
  interface PackReport {
    mods: number;
    linked: number;
    bundled: number;
    serverOnly: number;
    clientMods: number;
    skipped: string[];
    warnings: string[];
  }
  interface Build {
    version: string;
    fileName: string;
    size: number;
    createdAt: number;
    by: string;
    report: PackReport;
  }
  interface Config {
    name: string;
    summary: string;
    clientMods: string[];
    bundleUnknown: boolean;
    serverList: boolean;
    address: string;
    public: boolean;
    build: number;
    last?: Build;
  }
  interface PackInfo {
    supported: boolean;
    flavor: string;
    mcVersion?: string;
    loaderVersion?: string;
    config: Config;
    mods: PackMod[];
    modsError?: string;
    clientMods: ClientMod[];
    defaultAddress: string;
    publicPage: boolean;
    publicUrl?: string;
  }

  const mcUrl = $derived(`/api/servers/${server.id}/mc`);
  let info = $state<PackInfo | null>(null);
  let loadError = $state('');

  async function load() {
    try {
      info = await api.get<PackInfo>(`${mcUrl}/modpack`);
      loadError = '';
    } catch (e: any) {
      loadError = e.message;
    }
  }
  onMount(load);

  let fromModrinth = $derived(info ? info.mods.filter((m) => m.source === 'modrinth').length : 0);
  let unknown = $derived(info ? info.mods.filter((m) => m.source === 'file') : []);
  let serverProjects = $derived(new Set(info ? info.mods.map((m) => m.projectId).filter(Boolean) : []));
  let last = $derived(info?.config.last);

  function download() {
    const a = document.createElement('a');
    a.href = `${mcUrl}/modpack/download`;
    a.download = last?.fileName ?? 'modpack.mrpack';
    document.body.append(a);
    a.click();
    a.remove();
  }

  async function setPublic(v: boolean) {
    if (!info) return;
    try {
      const r = await api.put<{ config: Config }>(`${mcUrl}/modpack`, { public: v });
      info.config = r.config;
      toastOk(v ? 'Das Pack wird auf der öffentlichen Seite angeboten' : 'Das Pack ist nicht mehr öffentlich');
    } catch (e) {
      toastError(e);
      load();
    }
  }

  // ---- dialog ----
  let open = $state(false);
  let selected = $state<ClientMod[]>([]);
  let name = $state('');
  let serverList = $state(true);
  let address = $state('');
  let bundleUnknown = $state(true);
  let building = $state(false);

  let query = $state('');
  let hits = $state<MRHit[]>([]);
  let hitTotal = $state(0);
  let searching = $state(false);
  let searchError = $state('');

  function openDialog() {
    if (!info) return;
    selected = info.clientMods.filter((c) => info!.config.clientMods.includes(c.projectId));
    name = info.config.name;
    serverList = info.config.serverList;
    address = info.config.address;
    bundleUnknown = info.config.bundleUnknown;
    query = '';
    open = true;
    search();
  }

  async function search(append = false) {
    searching = true;
    searchError = '';
    try {
      const r = await api.get<{ hits: MRHit[]; total: number }>(`${mcUrl}/modpack/client-mods${qs({ q: query, offset: append ? hits.length : 0 })}`);
      hits = append ? [...hits, ...r.hits] : r.hits;
      hitTotal = r.total;
    } catch (e: any) {
      searchError = e.message;
    } finally {
      searching = false;
    }
  }

  let timer: ReturnType<typeof setTimeout>;
  function onQuery() {
    clearTimeout(timer);
    timer = setTimeout(() => search(), 300);
  }

  let selectedIds = $derived(new Set(selected.map((c) => c.projectId)));

  function toggleMod(h: MRHit) {
    if (selectedIds.has(h.project_id)) selected = selected.filter((c) => c.projectId !== h.project_id);
    else selected = [...selected, { projectId: h.project_id, slug: h.slug, title: h.title, description: h.description, iconUrl: h.icon_url }];
  }

  async function build() {
    building = true;
    try {
      const r = await api.post<{ config: Config }>(`${mcUrl}/modpack/build`, {
        name,
        clientMods: selected.map((c) => c.projectId),
        serverList,
        address: address.trim(),
        bundleUnknown,
      });
      open = false;
      if (info) {
        info.config = r.config;
        info.clientMods = selected;
      }
      toastOk(`Modpack Version ${r.config.last?.version} erstellt`);
      download();
    } catch (e) {
      toastError(e);
    } finally {
      building = false;
    }
  }

  const loaderNames: Record<string, string> = { fabric: 'Fabric', forge: 'Forge', neoforge: 'NeoForge' };
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Modpack</h2>
      <p>Macht aus den Mods dieses Servers ein Modrinth-Pack (.mrpack). Spieler importieren es in die Modrinth App und spielen sofort mit denselben Mods, auf Wunsch mit Grafik- und Komfort-Mods für sich.</p>
    </div>
    {#if info?.supported}
      <button class="btn primary" onclick={openDialog} disabled={!info.loaderVersion}><Package /> Modpack erstellen</button>
    {/if}
  </div>

  {#if loadError}
    <div class="notice danger"><TriangleAlert /><span>{loadError}</span></div>
  {:else if info && !info.supported}
    <div class="panel">
      <div class="empty">
        <Package />
        <h3>Nur für Mod-Server</h3>
        <p>Modpacks gibt es für Fabric-, Forge- und NeoForge-Server. Bei Plugin-Servern wie Paper spielen alle mit dem normalen Minecraft.</p>
      </div>
    </div>
  {:else if info}
    {#if !info.loaderVersion}
      <div class="notice warn">
        <TriangleAlert />
        <span>Die {loaderNames[info.flavor]}-Version dieses Servers ist unbekannt. Installiere den Server unter „Start &amp; Version“ einmal neu, dann kann das Panel ein Pack bauen.</span>
      </div>
    {/if}
    {#if info.modsError}
      <div class="notice warn"><TriangleAlert /><span>{info.modsError}. Ohne Modrinth wird jede Mod als Datei ins Pack gepackt.</span></div>
    {/if}

    <div class="top">
      <section class="panel lastpack">
        {#if last}
          <div class="lp-head">
            <span class="lp-icon"><FileArchive /></span>
            <div class="lp-title">
              <strong class="ellipsis">{info.config.name}</strong>
              <span class="small muted">Version {last.version}, {bytes(last.size)}, {ago(last.createdAt)}{last.by ? ` von ${last.by}` : ''}</span>
            </div>
            <button class="btn" onclick={download}><Download /> Herunterladen</button>
          </div>
          <p class="small muted lp-sum">
            {last.report.mods} {last.report.mods === 1 ? 'Mod' : 'Mods'} vom Server{#if last.report.clientMods}{' '}und {last.report.clientMods} nur für Spieler{/if}.
            {#if last.report.bundled}{last.report.bundled} {last.report.bundled === 1 ? 'Datei steckt' : 'Dateien stecken'} direkt im Pack.{/if}
          </p>
          {#if last.report.warnings.length || last.report.skipped.length}
            <div class="notice warn small">
              <TriangleAlert />
              <div class="stack tight">
                {#each last.report.warnings as wmsg, i (i)}<span>{wmsg}</span>{/each}
                {#if last.report.skipped.length}<span>Nicht im Pack, weil nicht auf Modrinth: {last.report.skipped.join(', ')}</span>{/if}
              </div>
            </div>
          {/if}
          <div class="lp-public">
            <Toggle
              checked={info.config.public}
              label="Auf der öffentlichen Seite anbieten"
              hint={info.publicPage ? 'Spieler laden das Pack dann selbst von der Statusseite des Servers.' : 'Dafür zuerst die öffentliche Seite des Servers einschalten.'}
              disabled={!info.publicPage}
              onchange={setPublic}
            />
            {#if info.publicUrl && info.config.public}
              <a class="small" href={info.publicUrl} target="_blank" rel="noopener">Statusseite öffnen <ExternalLink size={13} /></a>
            {/if}
          </div>
        {:else}
          <div class="lp-empty">
            <span class="lp-icon"><Package /></span>
            <div class="stack tight">
              <strong>Noch kein Pack erstellt</strong>
              <span class="small muted">Beim Erstellen kannst du noch Client-Mods wie Sodium oder eine Minikarte dazulegen.</span>
            </div>
          </div>
        {/if}
      </section>

      <aside class="panel howto">
        <h3><InfoIc size={15} /> So nutzen Spieler das Pack</h3>
        <ol>
          <li>Die Datei <span class="mono">.mrpack</span> herunterladen.</li>
          <li>In der Modrinth App eine neue Instanz aus dieser Datei anlegen. Prism Launcher kann .mrpack ebenfalls importieren.</li>
          <li>Spielen. {info.config.serverList ? 'Der Server steht schon in der Serverliste.' : 'Den Server über die Adresse hinzufügen.'}</li>
        </ol>
        <p class="tiny faint">{flavorLabel(info.flavor, info.mcVersion ?? '')}{info.loaderVersion ? `, Loader ${info.loaderVersion}` : ''}</p>
      </aside>
    </div>

    <section class="panel">
      <div class="panel-head">
        <ServerIc size={16} />
        <h3>Mods vom Server</h3>
        <span class="spacer"></span>
        <span class="small muted num">{info.mods.length}{#if info.mods.length}{' '}({fromModrinth} über Modrinth){/if}</span>
      </div>
      {#if info.mods.length === 0}
        <p class="panel-body small muted">Im Ordner mods/ liegen keine aktiven Mods. Das Pack enthält dann nur den Loader und deine Client-Mods.</p>
      {:else}
        <ul class="mlist">
          {#each info.mods as m (m.file)}
            <li>
              <span class="micon">{#if m.iconUrl}<img src={m.iconUrl} alt="" width="34" height="34" loading="lazy" />{:else}<Package size={16} />{/if}</span>
              <div class="minfo">
                <span class="mname">
                  {#if m.slug}<a href="https://modrinth.com/mod/{m.slug}" target="_blank" rel="noopener noreferrer">{m.name || m.file}</a>{:else}<strong>{m.name || m.file}</strong>{/if}
                  {#if m.version}<span class="small muted">{m.version}</span>{/if}
                </span>
                <span class="tiny faint ellipsis">{m.file}, {bytes(m.size)}</span>
              </div>
              <div class="mtags">
                {#if m.client === 'unsupported'}<span class="tag" title="Wird bei Spielern nicht installiert">Nur Server</span>{/if}
                {#if m.source === 'modrinth'}
                  <span class="tag accent" title="Die Modrinth App lädt die Datei direkt von Modrinth">Modrinth</span>
                {:else}
                  <span class="tag warn" title="Nicht auf Modrinth gefunden">Als Datei</span>
                {/if}
              </div>
            </li>
          {/each}
        </ul>
        {#if unknown.length}
          <p class="tiny faint mnote">
            {unknown.length === 1 ? 'Eine Mod gibt' : `${unknown.length} Mods gibt`} es nicht auf Modrinth. Sie {unknown.length === 1 ? 'kommt' : 'kommen'} als Datei ins Pack. Gib das Pack nur weiter, wenn deren Lizenzen das erlauben.
          </p>
        {/if}
      {/if}
    </section>

    {#if info.clientMods.length}
      <section class="panel">
        <div class="panel-head">
          <Monitor size={16} />
          <h3>Nur für Spieler</h3>
          <span class="spacer"></span>
          <button class="btn sm" onclick={openDialog} disabled={!info.loaderVersion}>Ändern</button>
        </div>
        <ul class="mlist">
          {#each info.clientMods as c (c.projectId)}
            <li>
              <span class="micon">{#if c.iconUrl}<img src={c.iconUrl} alt="" width="34" height="34" loading="lazy" />{:else}<Package size={16} />{/if}</span>
              <div class="minfo">
                <a class="mname" href="https://modrinth.com/mod/{c.slug}" target="_blank" rel="noopener noreferrer">{c.title}</a>
                {#if c.description}<span class="tiny faint ellipsis">{c.description}</span>{/if}
              </div>
            </li>
          {/each}
        </ul>
        {#if last && last.report.clientMods > info.clientMods.length}
          {@const deps = last.report.clientMods - info.clientMods.length}
          <p class="tiny faint mnote">Dazu {deps === 1 ? 'kommt eine benötigte Abhängigkeit' : `kommen ${deps} benötigte Abhängigkeiten`}.</p>
        {/if}
      </section>
    {/if}
  {:else}
    <div class="panel"><p class="panel-body small muted">Mods werden geprüft …</p></div>
  {/if}
</div>

<Modal bind:open title="Modpack erstellen" size="xl">
  {#if info}
    <div class="dlg">
      <div class="q">
        <h3>Client-Mods hinzufügen?</h3>
        <p class="small muted">Mods, die nur auf dem Rechner der Spieler laufen, zum Beispiel für mehr FPS, Shader oder eine Minikarte. Der Server braucht sie nicht. Benötigte Abhängigkeiten kommen automatisch mit.</p>
      </div>

      <div class="search">
        <Search size={15} />
        <input class="input" placeholder="Client-Mods auf Modrinth suchen" bind:value={query} oninput={onQuery} aria-label="Client-Mods suchen" />
      </div>

      {#if selected.length}
        <div class="chips" aria-label="Ausgewählt">
          {#each selected as c (c.projectId)}
            <span class="chip">
              {#if c.iconUrl}<img src={c.iconUrl} alt="" width="18" height="18" />{/if}
              {c.title}
              <button class="chip-x" aria-label="{c.title} entfernen" onclick={() => (selected = selected.filter((x) => x.projectId !== c.projectId))}><X size={13} /></button>
            </span>
          {/each}
        </div>
      {/if}

      <p class="tiny faint">{query ? 'Suchergebnisse' : 'Beliebt'} für {loaderNames[info.flavor]} {info.mcVersion}</p>
      {#if searchError}
        <div class="notice danger"><TriangleAlert /><span>{searchError}</span></div>
      {/if}
      <div class="cgrid" role="group" aria-label="Client-Mods">
        {#each hits as h (h.project_id)}
          {@const onServer = serverProjects.has(h.project_id)}
          {@const on = selectedIds.has(h.project_id)}
          <button class="cmod" class:on disabled={onServer} aria-pressed={on} onclick={() => toggleMod(h)} title={onServer ? 'Ist schon auf dem Server und damit im Pack' : h.description}>
            <span class="cicon">{#if h.icon_url}<img src={h.icon_url} alt="" width="36" height="36" loading="lazy" />{:else}<Package size={16} />{/if}</span>
            <span class="ctext">
              <strong class="ellipsis">{h.title}</strong>
              <span class="tiny faint ellipsis">{onServer ? 'Schon auf dem Server' : `${num(h.downloads)} Downloads`}</span>
            </span>
            <span class="ccheck" aria-hidden="true">{#if on || onServer}<Check size={14} />{/if}</span>
          </button>
        {/each}
      </div>
      {#if !searching && hits.length === 0 && !searchError}
        <p class="small muted">Nichts gefunden{query ? ` für „${query}“` : ''}.</p>
      {/if}
      {#if hits.length < hitTotal}
        <div class="more"><button class="btn sm" onclick={() => search(true)} disabled={searching}>Mehr laden</button></div>
      {/if}

      <div class="opts">
        <div class="field">
          <label for="mp-name">Name des Packs</label>
          <input id="mp-name" class="input" bind:value={name} maxlength="80" />
        </div>
        <div class="stack tight">
          <Toggle bind:checked={serverList} label="Server in die Serverliste eintragen" hint="Spieler finden den Server dann sofort unter Mehrspieler." />
          {#if serverList}
            <input class="input addr" bind:value={address} placeholder={info.defaultAddress || 'mc.example.de'} aria-label="Serveradresse" />
          {/if}
        </div>
        {#if unknown.length}
          <Toggle
            bind:checked={bundleUnknown}
            label="Mods ohne Modrinth-Eintrag mitliefern"
            hint="{unknown.length} {unknown.length === 1 ? 'Mod gibt' : 'Mods gibt'} es nicht auf Modrinth. Eingeschaltet kommen sie als Datei ins Pack."
          />
        {/if}
      </div>
    </div>
  {/if}
  {#snippet footer()}
    <button class="btn" onclick={() => (open = false)}>Abbrechen</button>
    <button class="btn primary" onclick={build} disabled={building || !name.trim()}>
      {#if building}Wird erstellt …{:else if selected.length}Mit {selected.length} Client-{selected.length === 1 ? 'Mod' : 'Mods'} erstellen{:else}Ohne Client-Mods erstellen{/if}
    </button>
  {/snippet}
</Modal>

<style>
  .top {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 320px;
    gap: 16px;
  }
  .lastpack {
    padding: 18px 20px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .lp-head,
  .lp-empty {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .lp-icon {
    width: 46px;
    height: 46px;
    flex: none;
    display: grid;
    place-items: center;
    border-radius: 12px;
    background: var(--accent-soft);
    color: var(--accent);
  }
  .lp-title {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .lp-title strong {
    font-family: var(--font-display);
    font-size: 17px;
  }
  .lp-sum {
    margin: 0;
  }
  .lp-public {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 12px;
    border-top: 1px solid var(--line);
  }
  .lp-public a {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .howto {
    padding: 18px 20px;
  }
  .howto h3 {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    font-family: var(--font-display);
    margin-bottom: 10px;
  }
  .howto ol {
    margin: 0 0 10px;
    padding-left: 20px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13.5px;
    color: var(--text-2);
  }
  .mlist {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .mlist li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 18px;
    border-bottom: 1px solid var(--line);
  }
  .mlist li:last-child {
    border-bottom: none;
  }
  .micon,
  .cicon {
    width: 34px;
    height: 34px;
    flex: none;
    display: grid;
    place-items: center;
    border-radius: 8px;
    background: var(--surface-2);
    color: var(--text-3);
    overflow: hidden;
  }
  .micon img,
  .cicon img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .minfo {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .mname {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
    font-weight: 650;
    color: var(--text);
  }
  .mtags {
    display: flex;
    gap: 6px;
  }
  .mnote {
    padding: 10px 18px 14px;
    margin: 0;
    border-top: 1px solid var(--line);
  }
  .dlg {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .q h3 {
    font-family: var(--font-display);
    font-size: 18px;
    margin-bottom: 4px;
  }
  .q p {
    max-width: 70ch;
  }
  .search {
    position: relative;
    max-width: 460px;
  }
  .search :global(svg) {
    position: absolute;
    left: 11px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-3);
  }
  .search .input {
    padding-left: 34px;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 30px;
    padding: 0 4px 0 10px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 13px;
    font-weight: 700;
  }
  .chip img {
    border-radius: 4px;
  }
  .chip-x {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: 0;
    border-radius: 50%;
    background: transparent;
    color: inherit;
    cursor: pointer;
  }
  .chip-x:hover {
    background: var(--accent);
    color: var(--accent-ink);
  }
  .cgrid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 8px;
  }
  .cmod {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border: 1px solid var(--line-2);
    border-radius: var(--r-m);
    background: var(--surface);
    color: var(--text);
    text-align: left;
    cursor: pointer;
    font: inherit;
  }
  .cmod:hover:not(:disabled) {
    border-color: var(--text-3);
  }
  .cmod.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .cmod:disabled {
    cursor: default;
    opacity: 0.6;
  }
  .cmod:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .ctext {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .ctext strong {
    font-size: 13.5px;
  }
  .ccheck {
    width: 22px;
    height: 22px;
    flex: none;
    display: grid;
    place-items: center;
    border-radius: 50%;
    border: 1.5px solid var(--line-2);
    color: var(--accent-ink);
  }
  .cmod.on .ccheck {
    background: var(--accent);
    border-color: var(--accent);
  }
  .cmod:disabled .ccheck {
    color: var(--text-3);
  }
  .more {
    display: flex;
    justify-content: center;
  }
  .opts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 16px 22px;
    padding-top: 16px;
    border-top: 1px solid var(--line);
  }
  .addr {
    max-width: 320px;
  }
  @media (max-width: 900px) {
    .top {
      grid-template-columns: 1fr;
    }
  }
</style>
