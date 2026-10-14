<script lang="ts">
  import PageBand from '../components/PageBand.svelte';
  import { onMount } from 'svelte';
  import { RefreshCw, TriangleAlert, Container, Cpu } from '@lucide/svelte';
  import Toggle from '../components/Toggle.svelte';
  import { api, qs } from '../lib/api';
  import { session, servers } from '../lib/session.svelte';
  import { go } from '../lib/router.svelte';
  import { toastError, toastOk } from '../lib/toast.svelte';
  import { mb } from '../lib/format';
  import type { Flavor, User } from '../lib/types';

  const meta = $derived(session.meta);
  let game = $state('minecraft');
  let flavor = $state('paper');
  let version = $state('');
  let build = $state('');
  let snapshots = $state(false);
  let versions = $state<{ id: string; type: string; stable: boolean; java?: number }[]>([]);
  let builds = $state<{ id: string; stable: boolean; channel?: string }[]>([]);
  let versionError = $state('');
  let loadingVersions = $state(false);
  let manualVersion = $state(false);

  let name = $state('');
  let description = $state('');
  let memory = $state(session.meta?.defaultMemory ?? 4096);
  let port = $state<number | ''>('');
  let runtime = $state(session.meta?.defaultRuntime === 'docker' && session.meta?.docker ? 'docker' : 'native');
  let dockerImage = $state('');
  let ownerId = $state(0);
  let autostart = $state(true);
  let eula = $state(false);
  let startAfter = $state(true);
  let advanced = $state(false);
  let users = $state<User[]>([]);

  // generic
  let command = $state('');
  let stopCommand = $state('');
  let readyRegex = $state('');

  let busy = $state(false);

  // map plugin installed together with the server
  let mapPlugin = $state('');
  let mapPlugins = $derived((meta?.mapPlugins ?? []).filter((p) => p.flavors.includes(flavor)));
  $effect(() => {
    if (mapPlugin && !mapPlugins.some((p) => p.id === mapPlugin)) mapPlugin = '';
  });

  const memPresets = [1024, 2048, 4096, 6144, 8192, 12288, 16384];
  let selectedFlavor = $derived(meta?.flavors.find((f) => f.id === flavor));

  async function loadVersions() {
    if (flavor === 'custom') {
      loadingVersions = true;
    }
    loadingVersions = true;
    versionError = '';
    try {
      versions = await api.get(`/api/meta/mc/versions${qs({ flavor: flavor === 'custom' ? 'vanilla' : flavor, snapshots: snapshots ? 1 : '' })}`);
      const first = versions.find((v) => v.stable) ?? versions[0];
      if (!versions.some((v) => v.id === version)) version = first?.id ?? '';
      manualVersion = false;
    } catch (e: any) {
      versions = [];
      versionError = e.message;
      manualVersion = true;
    } finally {
      loadingVersions = false;
    }
  }

  async function loadBuilds() {
    builds = [];
    build = '';
    if (!version || !['paper', 'purpur', 'fabric', 'forge', 'neoforge'].includes(flavor)) return;
    try {
      builds = await api.get(`/api/meta/mc/builds${qs({ flavor, version })}`);
    } catch {}
  }

  $effect(() => {
    flavor;
    snapshots;
    if (game === 'minecraft') loadVersions();
  });

  $effect(() => {
    if (advanced && version) loadBuilds();
  });

  onMount(async () => {
    if (session.user?.isAdmin) {
      try {
        users = await api.get('/api/admin/users');
      } catch {}
    }
  });

  const flavorKind: Record<string, string> = { plugins: 'Plugins', mods: 'Mods', vanilla: 'Ohne Erweiterungen', custom: 'Eigene Datei' };

  async function submit(e: Event) {
    e.preventDefault();
    if (game === 'minecraft' && !eula) {
      toastError('Bitte die Minecraft-EULA akzeptieren.');
      return;
    }
    busy = true;
    try {
      const settings =
        game === 'minecraft'
          ? { flavor, version, build, eula, trackChat: false, ...(mapPlugin ? { map: { plugin: mapPlugin } } : {}) }
          : { command, stopCommand, readyRegex };
      const srv = await api.post('/api/servers', {
        name,
        description,
        game,
        runtime,
        memoryMb: memory,
        port: port || 0,
        ownerId: ownerId || 0,
        autostart,
        dockerImage,
        settings,
        startAfterInstall: game === 'minecraft' && startAfter && flavor !== 'custom',
      });
      servers.put(srv);
      toastOk(`„${srv.name}“ wird installiert`);
      go(`/servers/${srv.id}`);
    } catch (err) {
      toastError(err);
    } finally {
      busy = false;
    }
  }
</script>

<PageBand title="Neuer Server" kicker={session.panel.name || 'Hearth'}>
  {#snippet sub()}Das Panel lädt die Serversoftware und die passende Java-Version automatisch herunter.{/snippet}
</PageBand>

<div class="page narrow">

  <form class="layout" onsubmit={submit}>
    <div class="stack main">
      {#if meta && meta.games.length > 1 && session.user?.isAdmin}
        <section class="panel">
          <div class="panel-head"><h3>Spiel</h3></div>
          <div class="panel-body choices two">
            {#each meta.games as g (g.id)}
              <label class="choice" class:on={game === g.id}>
                <input type="radio" name="game" value={g.id} bind:group={game} />
                <span class="ct">{g.name}</span>
                <span class="cd">{g.id === 'minecraft' ? 'Voll integriert: Spieler, Statistiken, Karte, Plugins' : 'Beliebiges Programm mit Konsole, Dateien und Backups'}</span>
              </label>
            {/each}
          </div>
        </section>
      {/if}

      {#if game === 'minecraft' && meta}
        <section class="panel">
          <div class="panel-head"><h3>Server-Art</h3></div>
          <div class="panel-body choices">
            {#each meta.flavors as f (f.id)}
              <label class="choice" class:on={flavor === f.id}>
                <input type="radio" name="flavor" value={f.id} bind:group={flavor} />
                <span class="ct">{f.name}{#if f.id === 'paper'} <span class="tag accent">Empfohlen</span>{/if}</span>
                <span class="cd">{f.description}</span>
                <span class="kind tiny faint">{flavorKind[f.kind]}</span>
              </label>
            {/each}
          </div>
        </section>

        <section class="panel">
          <div class="panel-head">
            <h3>Version</h3>
            <div class="spacer"></div>
            <Toggle bind:checked={snapshots} label="Vorabversionen" />
          </div>
          <div class="panel-body stack">
            {#if versionError}
              <div class="notice warn">
                <TriangleAlert />
                <div class="stack tight">
                  <span>Versionsliste konnte nicht geladen werden: {versionError}</span>
                  <span class="small muted">Du kannst die Version auch von Hand eintragen.</span>
                </div>
                <button type="button" class="btn sm" onclick={loadVersions}><RefreshCw /> Erneut</button>
              </div>
            {/if}
            <div class="form-grid">
              <div class="field">
                <label for="ver">Minecraft-Version</label>
                {#if manualVersion || flavor === 'custom' && versions.length === 0}
                  <input id="ver" class="input" bind:value={version} placeholder="z. B. 1.21.10 oder 26.3" required={flavor !== 'custom'} />
                {:else}
                  <select id="ver" class="select" bind:value={version} disabled={loadingVersions}>
                    {#each versions as v (v.id)}
                      <option value={v.id}>{v.id}{v.type === 'snapshot' ? ' (Vorschau)' : ''}</option>
                    {/each}
                  </select>
                {/if}
                {#if flavor === 'custom'}<p class="hint">Nur zur Auswahl der passenden Java-Version. Die JAR lädst du danach im Dateimanager hoch.</p>{/if}
              </div>
              {#if advanced && builds.length}
                <div class="field">
                  <label for="build">{flavor === 'paper' || flavor === 'purpur' ? 'Build' : 'Loader-Version'}</label>
                  <select id="build" class="select" bind:value={build}>
                    <option value="">Neueste stabile</option>
                    {#each builds as b (b.id)}
                      <option value={b.id}>{b.id}{b.stable ? '' : ' (experimentell)'}</option>
                    {/each}
                  </select>
                </div>
              {/if}
            </div>
          </div>
        </section>
        {#if mapPlugins.length}
          <section class="panel">
            <div class="panel-head"><h3>Karte</h3></div>
            <div class="panel-body stack">
              <div class="choices">
                <label class="choice" class:on={mapPlugin === ''}>
                  <input type="radio" name="mapplugin" value="" bind:group={mapPlugin} />
                  <span class="ct">Nur Hearth-Karte</span>
                  <span class="cd">Eingebaut und ohne Plugin: Das Panel rendert die Welt selbst und zeigt Spieler live.</span>
                </label>
                {#each mapPlugins as p (p.id)}
                  <label class="choice" class:on={mapPlugin === p.id}>
                    <input type="radio" name="mapplugin" value={p.id} bind:group={mapPlugin} />
                    <span class="ct">{p.name} dazu installieren</span>
                    <span class="cd">{p.description}</span>
                  </label>
                {/each}
              </div>
              <p class="hint">
                Die Hearth-Karte gibt es immer. Ein Plugin lädt das Panel bei der Installation aus Modrinth und richtet es ein{mapPlugin === 'bluemap' ? '; mit BlueMap bestätigst du dabei die Minecraft-EULA für den Download der Texturen' : ''}. Wechseln kannst du später unter „Karte“.
              </p>
            </div>
          </section>
        {/if}
      {:else if game !== 'minecraft'}
        <section class="panel">
          <div class="panel-head"><h3>Start</h3></div>
          <div class="panel-body stack">
            <div class="field">
              <label for="cmd">Startbefehl</label>
              <input id="cmd" class="input mono" bind:value={command} placeholder="./TerrariaServer -config serverconfig.txt -port {'{{port}}'}" required />
              <p class="hint">Wird im Serverordner ausgeführt. Platzhalter: <code>{'{{port}}'}</code>, <code>{'{{memory}}'}</code></p>
            </div>
            <div class="form-grid">
              <div class="field">
                <label for="stop">Stopp-Befehl <span class="faint">(optional)</span></label>
                <input id="stop" class="input mono" bind:value={stopCommand} placeholder="exit" />
              </div>
              <div class="field">
                <label for="ready">Zeile für „bereit“ <span class="faint">(Regex, optional)</span></label>
                <input id="ready" class="input mono" bind:value={readyRegex} placeholder="Server started" />
              </div>
            </div>
          </div>
        </section>
      {/if}

      <section class="panel">
        <div class="panel-head"><h3>Details</h3></div>
        <div class="panel-body stack">
          <div class="form-grid">
            <div class="field">
              <label for="name">Name</label>
              <input id="name" class="input" bind:value={name} maxlength="60" placeholder="Survival" required />
            </div>
            <div class="field">
              <label for="port">Port</label>
              <input id="port" class="input num" type="number" min="1024" max="65535" bind:value={port} placeholder="automatisch" />
            </div>
          </div>
          <div class="field">
            <label for="desc">Beschreibung <span class="faint">(optional)</span></label>
            <input id="desc" class="input" bind:value={description} maxlength="500" />
          </div>
          <div class="field">
            <label for="mem">Arbeitsspeicher: <strong>{mb(memory)}</strong></label>
            <input id="mem" type="range" min="512" max="32768" step="512" bind:value={memory} class="range" />
            <div class="row wrap">
              {#each memPresets as p (p)}
                <button type="button" class="btn sm" class:primary={memory === p} onclick={() => (memory = p)}>{mb(p)}</button>
              {/each}
            </div>
            {#if session.user && !session.user.isAdmin && session.user.maxMemoryMb > 0}
              <p class="hint">Dein Kontingent: {mb(session.user.maxMemoryMb)} insgesamt.</p>
            {/if}
          </div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-head">
          <h3>Betrieb</h3>
          <div class="spacer"></div>
          <button type="button" class="btn ghost sm" onclick={() => (advanced = !advanced)}>{advanced ? 'Weniger Optionen' : 'Mehr Optionen'}</button>
        </div>
        <div class="panel-body stack">
          <div class="choices two">
            <label class="choice" class:on={runtime === 'native'}>
              <input type="radio" name="rt" value="native" bind:group={runtime} />
              <span class="ct"><Cpu /> Direkt auf dem Host</span>
              <span class="cd">Kein Docker nötig. Java wird automatisch bereitgestellt.</span>
            </label>
            <label class="choice" class:on={runtime === 'docker'} class:disabled={!meta?.docker}>
              <input type="radio" name="rt" value="docker" bind:group={runtime} disabled={!meta?.docker} />
              <span class="ct"><Container /> Docker-Container</span>
              <span class="cd">{meta?.docker ? 'Abgeschottet mit festem RAM-Limit. Empfohlen für fremde Plugins.' : 'Docker ist auf diesem Host nicht verfügbar.'}</span>
            </label>
          </div>
          {#if advanced && runtime === 'docker' && session.user?.isAdmin}
            <div class="field">
              <label for="img">Docker-Image <span class="faint">(optional)</span></label>
              <input id="img" class="input mono" bind:value={dockerImage} placeholder={game === 'minecraft' ? 'automatisch: eclipse-temurin:<Java>-jre' : 'z. B. ubuntu:24.04'} required={game !== 'minecraft'} />
            </div>
          {/if}
          {#if session.user?.isAdmin && users.length > 1}
            <div class="field">
              <label for="owner">Besitzer</label>
              <select id="owner" class="select" bind:value={ownerId}>
                <option value={0}>Ich ({session.user.username})</option>
                {#each users.filter((u) => u.id !== session.user?.id) as u (u.id)}
                  <option value={u.id}>{u.username}</option>
                {/each}
              </select>
            </div>
          {/if}
          <Toggle bind:checked={autostart} label="Mit dem Panel starten" hint="Startet automatisch, wenn das Panel oder der Host neu startet." />
        </div>
      </section>
    </div>

    <aside class="side">
      <div class="panel summary">
        <div class="panel-body stack">
          <h3>{name || 'Neuer Server'}</h3>
          <dl>
            {#if game === 'minecraft'}
              <dt>Software</dt>
              <dd>{selectedFlavor?.name ?? flavor} {version}</dd>
            {/if}
            <dt>Speicher</dt>
            <dd>{mb(memory)}</dd>
            <dt>Port</dt>
            <dd>{port || 'automatisch'}</dd>
            <dt>Laufzeit</dt>
            <dd>{runtime === 'docker' ? 'Docker' : 'Direkt'}</dd>
          </dl>
          {#if game === 'minecraft'}
            <label class="check small">
              <input type="checkbox" bind:checked={eula} />
              <span>Ich akzeptiere die <a href="https://aka.ms/MinecraftEULA" target="_blank" rel="noopener noreferrer">Minecraft-EULA</a>.</span>
            </label>
            {#if flavor !== 'custom'}
              <label class="check small"><input type="checkbox" bind:checked={startAfter} /> Nach der Installation starten</label>
            {/if}
          {/if}
          <button class="btn primary" type="submit" disabled={busy || !name.trim() || (game === 'minecraft' && (!eula || (!version && flavor !== 'custom')))}>
            Server anlegen
          </button>
        </div>
      </div>
    </aside>
  </form>
</div>

<style>
  .layout {
    display: grid;
    grid-template-columns: 1fr 270px;
    gap: 20px;
    align-items: start;
  }
  .side {
    position: sticky;
    top: 20px;
  }
  .choices {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
    gap: 10px;
  }
  .choices.two {
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  }
  .choice {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 12px 14px;
    border-radius: var(--r-m);
    border: 1px solid var(--line-2);
    cursor: pointer;
    transition: border-color 0.12s, background 0.12s;
  }
  .choice:hover {
    background: var(--surface-2);
  }
  .choice.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .choice.disabled {
    opacity: 0.5;
    cursor: default;
  }
  .choice input {
    position: absolute;
    opacity: 0;
  }
  .choice:has(input:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .ct {
    display: flex;
    align-items: center;
    gap: 7px;
    font-weight: 650;
  }
  .ct :global(svg) {
    width: 16px;
    height: 16px;
    color: var(--text-2);
  }
  .cd {
    font-size: 13px;
    color: var(--text-2);
    line-height: 1.4;
  }
  .kind {
    margin-top: 2px;
  }
  .range {
    width: 100%;
    accent-color: var(--accent);
  }
  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 6px 12px;
    margin: 0;
    font-size: 14px;
  }
  dt {
    color: var(--text-3);
  }
  dd {
    margin: 0;
    text-align: right;
    font-weight: 560;
  }
  @media (max-width: 900px) {
    .layout {
      grid-template-columns: 1fr;
    }
    .side {
      position: static;
    }
  }
</style>
