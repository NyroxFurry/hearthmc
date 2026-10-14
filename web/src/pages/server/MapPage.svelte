<script lang="ts">
  import { onMount } from 'svelte';
  import { Map as MapIc, RefreshCw, Settings2, ExternalLink, LocateFixed, Mountain, TriangleAlert, Puzzle, Info, Download } from '@lucide/svelte';
  import type MapView from '../../components/MapView.svelte';
  import Modal from '../../components/Modal.svelte';
  import Toggle from '../../components/Toggle.svelte';
  import PlayerHead from '../../components/PlayerHead.svelte';
  import Menu from '../../components/Menu.svelte';
  import { api } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { can, session } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { ago, num } from '../../lib/format';
  import { dimLabels } from '../../lib/mc';
  import type { MapInfo, MapSettings, Position, Server, Status } from '../../lib/types';

  let { server, status }: { server: Server; status: Status } = $props();

  const mcUrl = $derived(`/api/servers/${server.id}/mc`);
  const canManage = $derived(can(server, 'map.manage'));
  const canPlugins = $derived(canManage && can(server, 'plugins.manage'));
  let info = $state<MapInfo | null>(null);
  let error = $state('');
  let positions = $state<Position[]>([]);
  let view = $state<'map' | 'plugin' | 'external' | ''>('');
  let mapView = $state<ReturnType<typeof MapView> | null>(null);
  let version = $state(0);
  let portBlocked = $state(false);

  async function load() {
    try {
      info = await api.get<MapInfo>(`${mcUrl}/map`);
      error = '';
      version++;
      if (!view) view = info.plugin?.file ? 'plugin' : 'map';
      probe();
    } catch (e: any) {
      error = e.message;
    }
  }

  // Is the map port reachable from this browser (firewall)?
  async function probe() {
    const base = info?.mapPort?.url;
    if (!base || !info?.plugin) return;
    try {
      await fetch(`${base}/healthz`, { mode: 'no-cors', cache: 'no-store' });
      portBlocked = false;
    } catch {
      portBlocked = true;
    }
  }

  let progress = $derived(live.mapProgress[server.id] ?? info?.render);
  let running = $derived(!!progress?.running);

  onMount(() => {
    load();
    const off = live.on('map', server.id, (e) => {
      if (!e.d.running && e.d.finishedAt) {
        if (e.d.error) toastError('Karte: ' + e.d.error);
        load();
      }
    });
    // after a (re)start the plugin needs a moment; refresh its state
    const t = setInterval(() => info?.plugin && !info.plugin.listening && status.state === 'running' && load(), 8000);
    return () => {
      off();
      clearInterval(t);
    };
  });

  async function render(full: boolean) {
    try {
      await api.post(`${mcUrl}/map/render`, { full });
      toastOk(full ? 'Karte wird komplett neu gerendert' : 'Karte wird aktualisiert');
    } catch (e) {
      toastError(e);
    }
  }

  // ---- settings ----
  let settingsOpen = $state(false);
  let draft = $state<MapSettings>({ enabled: true, dimensions: [], intervalMinutes: 30, externalUrl: '', livePlayers: true });
  let worldDims = $state<{ key: string; label: string }[]>([]);
  let saving = $state(false);

  async function openSettings() {
    if (!info) return;
    draft = { ...info.settings, dimensions: [...(info.settings.dimensions ?? [])] };
    if (!draft.intervalMinutes) draft.intervalMinutes = 30;
    worldDims = [
      { key: 'overworld', label: dimLabels.overworld },
      { key: 'the_nether', label: dimLabels.the_nether },
      { key: 'the_end', label: dimLabels.the_end },
    ];
    for (const d of info.world?.dimensions ?? []) {
      if (!worldDims.some((x) => x.key === d.key)) worldDims.push({ key: d.key, label: dimLabels[d.key] ?? d.label });
    }
    if (!draft.dimensions.length) draft.dimensions = worldDims.map((d) => d.key);
    settingsOpen = true;
  }

  function toggleDim(k: string, on: boolean) {
    draft.dimensions = on ? [...new Set([...draft.dimensions, k])] : draft.dimensions.filter((d) => d !== k);
  }

  async function saveSettings() {
    saving = true;
    try {
      info = await api.put<MapInfo>(`${mcUrl}/map/settings`, { ...draft, intervalMinutes: Number(draft.intervalMinutes) || 0 });
      settingsOpen = false;
      version++;
      toastOk('Karteneinstellungen gespeichert');
    } catch (e) {
      toastError(e);
    } finally {
      saving = false;
    }
  }

  // ---- map plugin ----
  let pluginOpen = $state(false);
  let pluginChoice = $state('');
  let installing = $state(false);
  let flavor = $derived(server.settings?.flavor ?? '');
  let available = $derived((session.meta?.mapPlugins ?? []).filter((p) => p.flavors.includes(flavor)));

  function openPlugin() {
    pluginChoice = info?.plugin?.id ?? '';
    pluginOpen = true;
  }

  async function applyPlugin(choice = pluginChoice) {
    installing = true;
    try {
      const r = await api.put<{ info: MapInfo; restartRequired: boolean }>(`${mcUrl}/map/plugin`, { plugin: choice });
      info = r.info;
      pluginOpen = false;
      view = choice ? 'plugin' : 'map';
      const name = available.find((p) => p.id === choice)?.name ?? '';
      toastOk(choice ? `${name} ist eingerichtet${r.restartRequired ? '. Starte den Server neu, damit die Karte erscheint.' : '.'}` : 'Karten-Plugin ausgeschaltet');
      probe();
    } catch (e) {
      toastError(e);
    } finally {
      installing = false;
    }
  }

  let lastRender = $derived(info ? Math.max(0, ...info.dimensions.map((d) => d.info.lastRender)) : 0);
  let regions = $derived(info ? info.dimensions.reduce((a, d) => a + d.info.regions, 0) : 0);
  let worldRegions = $derived(info?.world?.dimensions.reduce((a, d) => a + d.regions, 0) ?? 0);
  let external = $derived(info?.settings.externalUrl ?? '');
  let plugin = $derived(info?.plugin);
  let online = $derived(status.state === 'running');

  function dimName(id: string) {
    const key = id.replace(/^minecraft:/, '');
    return dimLabels[key] ?? key;
  }
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Karte</h2>
      <p>
        {#if view === 'plugin' && plugin}
          Die Karte von {plugin.name}, live aus dem laufenden Server.
        {:else}
          Eine Draufsicht der Welt, direkt aus den Regionsdateien gerendert.
          {#if lastRender}Stand {ago(lastRender)}, {num(regions)} Regionen.{/if}
        {/if}
      </p>
    </div>
    {#if plugin || external}
      <div class="seg" role="group" aria-label="Kartenansicht">
        <button class:on={view === 'map'} onclick={() => (view = 'map')}>Hearth-Karte</button>
        {#if plugin}<button class:on={view === 'plugin'} onclick={() => (view = 'plugin')}>{plugin.name}</button>{/if}
        {#if external}<button class:on={view === 'external'} onclick={() => (view = 'external')}>Externe Karte</button>{/if}
      </div>
    {/if}
    {#if view === 'plugin' && plugin?.url && plugin.listening}
      <a class="btn icon" href={plugin.url} target="_blank" rel="noopener noreferrer" title="In neuem Tab öffnen" aria-label="In neuem Tab öffnen"><ExternalLink /></a>
    {:else if view === 'external' && external}
      <a class="btn icon" href={external} target="_blank" rel="noopener noreferrer" title="In neuem Tab öffnen" aria-label="In neuem Tab öffnen"><ExternalLink /></a>
    {/if}
    {#if canManage}
      {#if canPlugins && available.length}
        <button class="btn" onclick={openPlugin} disabled={!info}><Puzzle /> Karten-Plugin</button>
      {/if}
      {#if view === 'map'}
        <button class="btn" onclick={openSettings} disabled={!info}><Settings2 /> Einstellungen</button>
        <button class="btn primary" onclick={() => render(false)} disabled={running}><RefreshCw class={running ? 'spin' : ''} /> {running ? 'Wird gerendert …' : 'Aktualisieren'}</button>
        <Menu items={[{ label: 'Komplett neu rendern', icon: Mountain, onclick: () => render(true), disabled: running }]} />
      {/if}
    {/if}
  </div>

  {#if progress?.running && view === 'map'}
    <div class="panel prog">
      <div class="row small">
        <span>Rendere {dimLabels[progress.dim] ?? progress.dim}</span>
        <span class="spacer"></span>
        <span class="num muted">{num(progress.done)} von {num(progress.total)} Regionen</span>
      </div>
      <div class="meter"><span style="width:{progress.total ? (progress.done / progress.total) * 100 : 0}%"></span></div>
    </div>
  {/if}

  {#if error}
    <div class="notice danger"><TriangleAlert /><span>{error}</span></div>
  {:else if info && view === 'external' && external}
    <iframe class="frame" src={external} title="Externe Karte" referrerpolicy="no-referrer"></iframe>
  {:else if info && view === 'plugin' && plugin}
    {#if !plugin.file}
      <div class="panel">
        <div class="empty">
          <Puzzle />
          <h3>{plugin.name} ist nicht installiert</h3>
          <p>Die Datei des Plugins fehlt im Ordner {server.settings?.flavor === 'paper' || server.settings?.flavor === 'purpur' ? 'plugins' : 'mods'}. Das Panel kann sie neu aus Modrinth laden.</p>
          {#if canPlugins}<button class="btn primary" onclick={() => applyPlugin(plugin.id)} disabled={installing}><Download /> {installing ? 'Wird installiert …' : 'Jetzt installieren'}</button>{/if}
        </div>
      </div>
    {:else if !plugin.enabled}
      <div class="notice warn"><TriangleAlert /><span>{plugin.name} ist unter „Plugins“ deaktiviert. Aktiviere es dort, damit die Karte erscheint.</span></div>
    {:else if !plugin.url}
      <div class="notice warn">
        <TriangleAlert />
        <span>
          {#if info.mapPort?.disabled}
            Der Kartenport ist ausgeschaltet. Ein Administrator kann ihn mit <code>--map-listen</code> oder <code>HEARTH_MAP_LISTEN</code> einschalten.
          {:else}
            Der Kartenport{info.mapPort?.port ? ` ${info.mapPort.port}` : ''} konnte nicht geöffnet werden{info.mapPort?.error ? `: ${info.mapPort.error}` : ''}.
          {/if}
        </span>
      </div>
    {:else if !online}
      <div class="panel">
        <div class="empty">
          <MapIc />
          <h3>Die Karte ist da, sobald der Server läuft</h3>
          <p>{plugin.name} läuft im Server mit. Starte ihn, dann erscheint die Karte hier und auf der öffentlichen Seite.</p>
        </div>
      </div>
    {:else}
      {#if info.mapPort?.mixed}
        <div class="notice warn"><TriangleAlert /><span>Das Panel läuft über HTTPS, der Kartenport aber nicht. Browser blockieren die Karte deshalb. Trage unter Administration, Einstellungen eine HTTPS-Kartenadresse ein.</span></div>
      {:else if portBlocked}
        <div class="notice warn">
          <TriangleAlert />
          <span>Dein Browser erreicht den Kartenport {info.mapPort?.port} nicht. Gib ihn in der Firewall frei, zum Beispiel mit <code>sudo ufw allow {info.mapPort?.port}/tcp</code>.</span>
        </div>
      {:else if !plugin.listening}
        <div class="notice info"><Info /><span>{plugin.name} startet noch. Beim ersten Start kann es eine Weile dauern, bis die Welt gerendert ist.</span></div>
      {/if}
      <iframe class="frame" src={plugin.url} title="Karte von {plugin.name}" referrerpolicy="no-referrer" allow="fullscreen"></iframe>
    {/if}
  {:else if info && info.dimensions.length === 0}
    <div class="panel">
      <div class="empty">
        <MapIc />
        <h3>Noch keine Karte</h3>
        {#if !info.world?.exists}
          <p>Es gibt noch keine Welt. Starte den Server einmal, dann legt Minecraft sie an und die Karte entsteht automatisch.</p>
        {:else if worldRegions === 0}
          <p>
            Die Welt „{info.world.name}“ ist da, aber noch nicht auf die Festplatte geschrieben.
            {#if online}Das passiert beim ersten Speichern; die Karte erscheint dann von selbst innerhalb weniger Minuten.{:else}Starte den Server, damit sie gespeichert wird.{/if}
          </p>
        {:else}
          <p>
            {num(worldRegions)} Regionsdateien gefunden.
            {#if info.settings.enabled && info.settings.intervalMinutes > 0 && online}Die erste Karte wird gleich automatisch gerendert.{:else if canManage}Rendere sie jetzt, um die Karte zu sehen.{/if}
          </p>
        {/if}
        {#if info.render?.error}<p class="error-text">Letzter Versuch: {info.render.error}</p>{/if}
        {#if canManage && worldRegions > 0}<button class="btn primary" onclick={() => render(false)} disabled={running}><RefreshCw /> Jetzt rendern</button>{/if}
        {#if canPlugins && available.length && !plugin}
          <p class="small faint alt">Lieber eine Live-Karte mit Plugin? Unter <button class="link" onclick={openPlugin}>Karten-Plugin</button> richtet das Panel BlueMap, squaremap oder Dynmap ein.</p>
        {/if}
      </div>
    </div>
  {:else if info}
    {#if info.render?.error && !running}
      <div class="notice warn"><TriangleAlert /><span>Letztes Rendern mit Fehlern: {info.render.error}</span></div>
    {/if}
    <div class="mapgrid">
      {#key version}
        {#await import('../../components/MapView.svelte')}
          <div class="maploading"></div>
        {:then { default: MapCanvas }}
          <MapCanvas bind:this={mapView} {info} tileBase="{mcUrl}/map/tiles" playersUrl="{mcUrl}/map/players" height="calc(100vh - var(--head-h, 93px) - 150px)" onplayers={(p) => (positions = p)} />
        {/await}
      {/key}
      <aside class="panel who">
        <div class="panel-head"><h3>Spieler</h3><span class="spacer"></span><span class="small muted num">{positions.length}</span></div>
        {#if positions.length}
          <ul>
            {#each positions as p (p.uuid)}
              <li>
                <button onclick={() => mapView?.flyTo(p.x, p.z, p.dim)} title="Auf der Karte zeigen">
                  <PlayerHead uuid={p.uuid} name={p.name} size={24} />
                  <span class="pn">
                    <span class="ellipsis">{p.name}</span>
                    <span class="tiny faint num">{dimName(p.dim)}, {Math.round(p.x)} / {Math.round(p.z)}{p.live ? '' : ', zuletzt'}</span>
                  </span>
                  <LocateFixed size={15} class="loc" />
                </button>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="small muted none">
            {online ? (info.settings.livePlayers ? 'Gerade ist niemand online.' : 'Live-Positionen sind in den Einstellungen ausgeschaltet.') : 'Positionen erscheinen, wenn der Server läuft.'}
          </p>
        {/if}
      </aside>
    </div>
  {/if}
</div>

<Modal bind:open={settingsOpen} title="Karteneinstellungen" size="m">
  <div class="stack">
    <Toggle bind:checked={draft.enabled} label="Automatisch aktualisieren" hint="Rendert geänderte Regionen regelmäßig, solange der Server läuft, und einmal nach dem Stoppen." />
    {#if draft.enabled}
      <div class="field">
        <label for="iv">Intervall in Minuten</label>
        <input id="iv" class="input num" type="number" min="5" max="1440" bind:value={draft.intervalMinutes} style="max-width:140px" />
      </div>
    {/if}
    <div class="field">
      <span class="label">Dimensionen</span>
      <div class="dims">
        {#each worldDims as d (d.key)}
          <label class="check"><input type="checkbox" checked={draft.dimensions.includes(d.key)} onchange={(e) => toggleDim(d.key, e.currentTarget.checked)} /> {d.label}</label>
        {/each}
      </div>
    </div>
    <Toggle bind:checked={draft.livePlayers} label="Spielerpositionen live anzeigen" hint="Fragt die Positionen über RCON ab. Auch auf der öffentlichen Seite, falls dort eingeschaltet." />
    <div class="field">
      <label for="ext">Externe Karte <span class="faint">(optional)</span></label>
      <input id="ext" class="input" type="url" bind:value={draft.externalUrl} placeholder="https://map.example.de" />
      <p class="hint">Für eine Karte, die woanders läuft. Für BlueMap, squaremap oder Dynmap auf diesem Server nimm besser „Karten-Plugin“, dann richtet das Panel alles selbst ein.</p>
    </div>
  </div>
  {#snippet footer()}
    <button class="btn" onclick={() => (settingsOpen = false)}>Abbrechen</button>
    <button class="btn primary" onclick={saveSettings} disabled={saving}>Speichern</button>
  {/snippet}
</Modal>

<Modal bind:open={pluginOpen} title="Karten-Plugin" size="m">
  <div class="stack">
    <p class="muted small">
      Das Panel lädt das Plugin aus Modrinth, stellt seinen Webserver ein und zeigt die Karte hier und auf der öffentlichen Seite.
    </p>
    <div class="choices" role="radiogroup" aria-label="Karten-Plugin">
      <label class="choice" class:on={pluginChoice === ''}>
        <input type="radio" name="mp" value="" bind:group={pluginChoice} />
        <span class="ct">Keins</span>
        <span class="cd">Nur die eingebaute Hearth-Karte. Sie braucht kein Plugin und belastet den Server nicht.</span>
      </label>
      {#each available as p (p.id)}
        <label class="choice" class:on={pluginChoice === p.id}>
          <input type="radio" name="mp" value={p.id} bind:group={pluginChoice} />
          <span class="ct">{p.name}{#if plugin?.id === p.id}<span class="tag accent">Aktiv</span>{/if}</span>
          <span class="cd">{p.description}</span>
        </label>
      {/each}
    </div>
    {#if pluginChoice === 'bluemap'}
      <p class="hint">Mit BlueMap bestätigst du die Minecraft-EULA, denn das Plugin lädt einmalig die Client-Texturen von Mojang.</p>
    {/if}
    {#if plugin && pluginChoice !== plugin.id}
      <p class="hint">{plugin.name} wird dabei deaktiviert, seine Daten bleiben erhalten.</p>
    {/if}
    {#if info?.mapPort?.port && !info.mapPort.custom}
      <p class="hint">Browser laden die Karte über den Kartenport {info.mapPort.port} des Panels. Gib ihn in deiner Firewall frei, falls du eine nutzt.</p>
    {/if}
  </div>
  {#snippet footer()}
    <button class="btn" onclick={() => (pluginOpen = false)}>Abbrechen</button>
    <button class="btn primary" onclick={() => applyPlugin()} disabled={installing || pluginChoice === (plugin?.id ?? '')}>
      {installing ? 'Wird eingerichtet …' : pluginChoice ? 'Installieren und einrichten' : 'Plugin ausschalten'}
    </button>
  {/snippet}
</Modal>

<style>
  .prog {
    padding: 12px 16px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .mapgrid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 250px;
    gap: 14px;
    align-items: start;
  }
  .maploading {
    height: calc(100vh - var(--head-h, 93px) - 150px);
    border-radius: var(--r-l);
    border: 1px solid var(--line);
    background: #0d1113;
  }
  .who ul {
    list-style: none;
    margin: 0;
    padding: 6px;
    max-height: calc(100vh - var(--head-h, 93px) - 220px);
    overflow-y: auto;
  }
  .who li button {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 7px 8px;
    border: none;
    background: none;
    border-radius: var(--r-s);
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .who li button:hover {
    background: var(--surface-2);
  }
  .who :global(.loc) {
    color: var(--text-3);
    flex: none;
  }
  .pn {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    font-size: 14px;
    font-weight: 700;
  }
  .none {
    padding: 14px 16px 18px;
  }
  .frame {
    display: block;
    width: 100%;
    height: calc(100vh - var(--head-h, 93px) - 150px);
    min-height: 420px;
    border: 1px solid var(--line);
    border-radius: var(--r-l);
    background: #0d1113;
  }
  .dims {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 18px;
  }
  .alt {
    margin-top: 6px;
  }
  .link {
    border: none;
    background: none;
    padding: 0;
    font: inherit;
    font-weight: 700;
    color: var(--text);
    text-decoration: underline;
    text-underline-offset: 3px;
    cursor: pointer;
  }
  .choices {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .choice {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px 12px;
    padding: 12px 14px;
    border: 1.5px solid var(--line);
    border-radius: var(--r-m);
    cursor: pointer;
    transition: border-color 0.15s, background-color 0.15s;
  }
  .choice:hover {
    border-color: var(--line-2);
  }
  .choice.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .choice input {
    grid-row: span 2;
    margin-top: 3px;
    accent-color: var(--accent);
  }
  .ct {
    font-weight: 700;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .cd {
    grid-column: 2;
    font-size: 13.5px;
    color: var(--text-2);
  }
  @media (max-width: 1000px) {
    .mapgrid {
      grid-template-columns: 1fr;
    }
  }
</style>
