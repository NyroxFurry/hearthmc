<script lang="ts">
  import { onMount } from 'svelte';
  import { Copy, Check, ExternalLink, ArrowLeft, Menu, X } from '@lucide/svelte';
  import Flame from '../components/Flame.svelte';
  import PlayerHead from '../components/PlayerHead.svelte';
  import PlayerProfile from '../components/PlayerProfile.svelte';
  import EventFeed from '../components/EventFeed.svelte';
  import AreaChart from '../components/AreaChart.svelte';
  import Leaderboard from './Leaderboard.svelte';
  import { api, qs } from '../lib/api';
  import { mcToHtml, stripCodes } from '../lib/mctext';
  import { markdown } from '../lib/markdown';
  import { ago, duration, hours, num, distance, time } from '../lib/format';
  import { flavorLabel } from '../lib/mc';
  import { fillGaps } from '../lib/series';
  import { flavorFor, flavorVars } from '../lib/flavors';
  import type { MapInfo, MCEvent, OnlinePlayer, PlayerDetail } from '../lib/types';

  interface Info {
    server: {
      name: string;
      description: string;
      accent: string;
      color: string;
      links: { label: string; url: string }[];
      game: string;
      panelName: string;
      address?: string;
      version?: string;
      flavor?: string;
      hasIcon?: boolean;
    };
    status: { online: boolean; state: string; motd?: string; players?: number; maxPlayers?: number; latencyMs?: number; versionName?: string; hasIcon?: boolean };
    sections: Record<string, any>;
    online?: OnlinePlayer[];
    totals?: { players: number; playSeconds: number; deaths: number; mobKills: number; blocksMined: number; distanceCm: number; advancements: number };
    map?: { enabled: boolean; externalUrl: string; pluginUrl?: string; pluginName?: string };
    startedAt: number;
  }

  const m = location.pathname.match(/^\/s\/([^/]+)(?:\/p\/([^/]+))?/);
  const slug = m ? decodeURIComponent(m[1]) : '';
  let player = $state(m?.[2] ? decodeURIComponent(m[2]) : '');

  let info = $state<Info | null>(null);
  let missing = $state(false);
  let deaths = $state<MCEvent[]>([]);
  let advancements = $state<MCEvent[]>([]);
  let chat = $state<MCEvent[]>([]);
  let causeLabels = $state<Record<string, string>>({});
  let causes30 = $state<Record<string, number>>({});
  let mapInfo = $state<MapInfo | null>(null);
  let range = $state<'24h' | '7d' | '30d'>('7d');
  let activity = $state<{ t: number; v: number }[]>([]);
  let detail = $state<PlayerDetail | null>(null);
  let detailError = $state('');
  let copied = $state(false);
  let iconFailed = $state(false);
  let menuOpen = $state(false);
  let now = $state(Date.now() / 1000);

  const base = `/api/public/${encodeURIComponent(slug)}`;
  let s = $derived(info?.sections ?? {});

  async function loadInfo() {
    try {
      info = await api.get<Info>(base);
      missing = false;
    } catch (e: any) {
      if (e.status === 404) missing = true;
    }
  }

  async function loadEvents() {
    if (!info) return;
    const get = (types: string, limit: number) =>
      api.get<{ events: MCEvent[]; causeLabels: Record<string, string>; deathCauses30d?: Record<string, number> }>(`${base}/events${qs({ types, limit })}`);
    try {
      if (s.showDeaths) {
        const r = await get('death', 12);
        deaths = r.events;
        causeLabels = r.causeLabels;
        causes30 = r.deathCauses30d ?? {};
      }
      if (s.showAdvancements) advancements = (await get('advancement', 10)).events;
      if (s.showChat) chat = (await get('chat', 20)).events;
    } catch {}
  }

  async function loadActivity() {
    if (!s.showActivity) return;
    try {
      const r = await api.get<{ points: { t: number; players: number }[]; bucket: number }>(`${base}/activity${qs({ range })}`);
      activity = fillGaps(
        r.points.map((p) => ({ t: p.t, v: Math.max(0, p.players) })),
        r.bucket,
        (t) => ({ t, v: 0 }),
      );
    } catch {}
  }

  async function loadMap() {
    if (!s.showMap) return;
    try {
      mapInfo = await api.get<MapInfo>(`${base}/map`);
    } catch {}
  }

  async function loadPlayer(uuid: string) {
    detail = null;
    detailError = '';
    try {
      detail = await api.get<PlayerDetail>(`${base}/players/${uuid}`);
    } catch (e: any) {
      detailError = e.status === 404 ? 'Dieses Profil ist nicht öffentlich.' : e.message;
    }
  }

  function openPlayer(uuid: string) {
    history.pushState(null, '', `/s/${slug}/p/${uuid}`);
    player = uuid;
    scrollTo(0, 0);
  }

  function back() {
    history.pushState(null, '', `/s/${slug}`);
    player = '';
  }

  $effect(() => {
    if (player) loadPlayer(player);
  });

  $effect(() => {
    range;
    if (info) loadActivity();
  });

  // Farbe der Seite: Band, Lichthof und Punkte in der Sorte des Servers
  let flavor = $derived(flavorFor(info?.server.color ?? ''));
  $effect(() => {
    const root = document.documentElement;
    for (const part of flavorVars(flavor).split(';')) {
      const [k, v] = part.split(':');
      root.style.setProperty(k, v);
    }
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', flavor.bg);
  });

  $effect(() => {
    if (info) document.title = player && detail ? `${detail.player.name} – ${info.server.name}` : info.server.name;
  });

  onMount(() => {
    (async () => {
      await loadInfo();
      if (info) {
        loadEvents();
        loadMap();
        loadActivity();
      }
    })();
    const pop = () => {
      const mm = location.pathname.match(/^\/s\/[^/]+(?:\/p\/([^/]+))?/);
      player = mm?.[1] ?? '';
    };
    addEventListener('popstate', pop);
    const t1 = setInterval(loadInfo, 30000);
    const t2 = setInterval(() => !player && loadEvents(), 60000);
    const t3 = setInterval(() => (now = Date.now() / 1000), 10000);
    return () => {
      removeEventListener('popstate', pop);
      clearInterval(t1);
      clearInterval(t2);
      clearInterval(t3);
    };
  });

  async function copyAddress() {
    const a = info?.server.address;
    if (!a) return;
    try {
      await navigator.clipboard.writeText(a);
    } catch {
      const ta = document.createElement('textarea');
      ta.value = a;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      ta.remove();
    }
    copied = true;
    setTimeout(() => (copied = false), 5000);
  }

  let online = $derived(info?.status.online ?? false);
  let starting = $derived(info?.status.state === 'starting');
  let version = $derived(info?.status.versionName || (info?.server.version ? flavorLabel(info.server.flavor ?? '', info.server.version) : ''));
  let motd = $derived.by(() => {
    const raw = info?.status.motd ?? '';
    const plain = stripCodes(raw).trim();
    return plain && plain !== 'A Minecraft Server' ? raw : '';
  });
  let causes = $derived(
    Object.entries(causes30)
      .map(([k, n]) => ({ k, n, label: causeLabels[k] ?? k }))
      .sort((a, b) => b.n - a.n)
      .slice(0, 8),
  );
  let causeMax = $derived(Math.max(1, ...causes.map((c) => c.n)));
  let hasMap = $derived(!!mapInfo && mapInfo.dimensions.length > 0);
  let external = $derived(info?.map?.externalUrl ?? '');
  let pluginMap = $derived(online ? (info?.map?.pluginUrl ?? '') : '');
  let mapMode = $state<'plugin' | 'hearth' | ''>('');
  let mapShown = $derived(mapMode || (pluginMap ? 'plugin' : 'hearth'));
  let mapStamp = $derived(mapInfo ? Math.max(0, ...mapInfo.dimensions.map((d) => d.info.lastRender)) : 0);
  let showMap = $derived(!!s.showMap && (hasMap || !!external || !!pluginMap));
  let showActivity = $derived(!!s.showActivity && activity.length > 1);
  let nav = $derived(
    [
      { id: 'karte', label: 'Karte', show: showMap },
      { id: 'bestenliste', label: 'Bestenliste', show: !!s.showLeaderboard },
      { id: 'tode', label: 'Tode', show: !!s.showDeaths },
      { id: 'aktivitaet', label: 'Aktivität', show: showActivity },
    ].filter((n) => n.show),
  );
  let year = new Date().getFullYear();
</script>

{#snippet header()}
  <header class="hdr" class:open={menuOpen}>
    <a class="logo" href="/s/{slug}" onclick={(e) => { if (player) { e.preventDefault(); back(); } }}>
      <Flame size={32} />
      <span class="ellipsis">{info?.server.panelName || 'Hearth'}</span>
    </a>
    {#if !player && (nav.length || info?.server.links.length)}
      <nav class="navwrap" aria-label="Seitenbereiche">
        <ul class="nav">
          {#each nav as n (n.id)}
            <li><a href="#{n.id}" onclick={() => (menuOpen = false)}>{n.label}</a></li>
          {/each}
          {#each info?.server.links ?? [] as l (l.url)}
            <li><a href={l.url} target="_blank" rel="noopener noreferrer nofollow">{l.label} <ExternalLink size={14} /></a></li>
          {/each}
        </ul>
      </nav>
      <button class="menu" onclick={() => (menuOpen = !menuOpen)} aria-expanded={menuOpen} aria-label="Menü">
        {#if menuOpen}<X size={22} />{:else}<Menu size={22} />{/if}
      </button>
    {/if}
  </header>
{/snippet}

{#snippet shead(kicker: string, title: string)}
  <div class="shead">
    {#if kicker}<p class="kicker">{kicker}</p>{/if}
    <h2>{title}</h2>
  </div>
{/snippet}

{#if missing}
  <div class="intro small-intro">
    <div class="glow" aria-hidden="true"></div>
    <div class="wrap"><header class="hdr"><span class="logo"><Flame size={32} /><span>Hearth</span></span></header></div>
    <main class="wrap missing">
      <p class="kick">Statusseite</p>
      <h1>Diese Seite gibt es nicht</h1>
      <p class="lead">Der Server ist nicht mehr öffentlich, oder die Adresse ist falsch geschrieben.</p>
    </main>
  </div>
{:else if info}
  {#if player}
    <div class="intro sub">
      <div class="wrap">{@render header()}</div>
    </div>
    <main class="paper">
      <div class="wrap subinner">
        <button class="backlink" onclick={back}><ArrowLeft size={16} /> {info.server.name}</button>
        {#if detailError}
          <p class="lead">{detailError}</p>
        {:else if detail}
          <PlayerProfile {detail} />
        {:else}
          <p class="lead">Lade Profil …</p>
        {/if}
      </div>
    </main>
  {:else}
    <div class="intro">
      <div class="wrap">{@render header()}</div>
      <main class="hero wrap">
        <p class="kick">{info.server.game === 'minecraft' ? 'Minecraft-Server' : 'Gameserver'}</p>
        <div class="stage">
          <div class="glow" aria-hidden="true"></div>
          <span class="picon">
            {#if info.server.hasIcon && !iconFailed}
              <img src="{base}/icon.png" alt="" width="120" height="120" onerror={() => (iconFailed = true)} />
            {:else}
              <span class="letter">{info.server.name.slice(0, 1).toUpperCase()}</span>
            {/if}
          </span>
        </div>
        <h1>{info.server.name}</h1>
        {#if online && motd}<p class="motd">{@html mcToHtml(motd)}</p>{/if}

        <div class="controls">
          <span class="status" class:on={online} class:starting>
            <span class="beacon" aria-hidden="true"></span>
            {#if online}
              <strong>Online</strong>
              <span class="soft num">{num(info.status.players ?? 0)} von {num(info.status.maxPlayers ?? 0)} Spielern</span>
            {:else if starting}
              <strong>Startet gerade</strong>
            {:else}
              <strong>Offline</strong>
            {/if}
          </span>
          {#if info.server.address}
            <button class="join" onclick={copyAddress} aria-label="Adresse {info.server.address} kopieren">
              <span class="atext">{info.server.address}</span>
              <span class="cta">{#if copied}<Check size={16} /> Kopiert{:else}<Copy size={16} /> Kopieren{/if}</span>
            </button>
          {/if}
        </div>
        <p class="hint" aria-live="polite">
          {#if copied}
            In Minecraft unter Mehrspieler auf „Server hinzufügen“ gehen und die Adresse einfügen.
          {:else if online && info.startedAt}
            Läuft seit {duration(now - info.startedAt, true)}{#if version}{' '}mit {version}{/if}.
          {:else if version}
            {version}
          {/if}
        </p>

        {#if s.showOnline && online && info.online?.length}
          <ul class="online" aria-label="Spieler online">
            {#each info.online as p (p.uuid)}
              <li>
                <button onclick={() => s.showProfiles && openPlayer(p.uuid)} disabled={!s.showProfiles} title="Online seit {time(p.since)}">
                  <PlayerHead uuid={p.uuid} name={p.name} size={24} />
                  <span>{p.name}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </main>
    </div>

    <main class="paper">
      {#if info.totals && info.totals.players > 0}
        <section class="totals" aria-label="Gemeinsame Statistik">
          <div class="wrap trow">
            <div><span class="v num">{num(info.totals.players)}</span><span class="l">Spieler bisher</span></div>
            <div><span class="v num">{hours(info.totals.playSeconds)}</span><span class="l">zusammen gespielt</span></div>
            <div><span class="v num">{num(info.totals.blocksMined)}</span><span class="l">Blöcke abgebaut</span></div>
            <div><span class="v num">{distance(info.totals.distanceCm)}</span><span class="l">zurückgelegt</span></div>
            <div><span class="v num">{num(info.totals.deaths)}</span><span class="l">Tode</span></div>
          </div>
        </section>
      {/if}

      {#if info.server.description}
        <section class="psec about">
          <div class="wrap agrid">
            <div>
              <p class="kicker">Über den Server</p>
              <h2>Willkommen auf {info.server.name}</h2>
            </div>
            <div class="atext2">{@html markdown(info.server.description)}</div>
          </div>
        </section>
      {/if}

      {#if showMap}
        <section class="psec" id="karte">
          <div class="wrap">
            <div class="srow">
              {@render shead(
                pluginMap && mapShown === 'plugin' ? `Live mit ${info.map?.pluginName}` : mapInfo?.showPlayers && online ? 'Mit Spielern live' : mapStamp ? `Stand ${ago(mapStamp)}` : '',
                'Karte',
              )}
              {#if pluginMap && hasMap}
                <div class="seg" role="group" aria-label="Kartenansicht">
                  <button class:on={mapShown === 'plugin'} onclick={() => (mapMode = 'plugin')}>{info.map?.pluginName}</button>
                  <button class:on={mapShown === 'hearth'} onclick={() => (mapMode = 'hearth')}>Übersicht</button>
                </div>
              {/if}
              {#if pluginMap && mapShown === 'plugin'}
                <a class="btn" href={pluginMap} target="_blank" rel="noopener noreferrer">Vollbild <ExternalLink /></a>
              {:else if external}
                <a class="btn" href={external} target="_blank" rel="noopener noreferrer">Live-Karte öffnen <ExternalLink /></a>
              {/if}
            </div>
            {#if pluginMap && mapShown === 'plugin'}
              <iframe class="extmap" src={pluginMap} title="Karte von {info.map?.pluginName}" referrerpolicy="no-referrer" allow="fullscreen"></iframe>
            {:else if hasMap && mapInfo}
              {#await import('../components/MapView.svelte') then { default: MapView }}
                <MapView info={mapInfo} tileBase="{base}/map/tiles" playersUrl={mapInfo.showPlayers ? `${base}/map/players` : null} height="min(70vh, 660px)" />
              {/await}
            {:else}
              <iframe class="extmap" src={external} title="Karte" referrerpolicy="no-referrer"></iframe>
            {/if}
          </div>
        </section>
      {/if}

      {#if s.showLeaderboard || s.showDeaths || (s.showAdvancements && advancements.length) || (s.showChat && chat.length)}
        <section class="psec">
          <div class="wrap cols">
            <div class="col">
              {#if s.showLeaderboard}
                <div class="block" id="bestenliste">
                  {@render shead('Wer vorne liegt', 'Bestenliste')}
                  <div class="card"><Leaderboard {slug} profiles={!!s.showProfiles} onplayer={openPlayer} /></div>
                </div>
              {/if}
              {#if s.showAdvancements && advancements.length}
                <div class="block">
                  {@render shead('Zuletzt erreicht', 'Fortschritte')}
                  <div class="card"><EventFeed events={advancements} playerHref={null} dense /></div>
                </div>
              {/if}
            </div>
            <div class="col">
              {#if s.showDeaths}
                <div class="block" id="tode">
                  {@render shead('Die letzten 30 Tage', 'Tode')}
                  <div class="card">
                    {#if causes.length}
                      <ul class="causes" aria-label="Häufigste Todesursachen">
                        {#each causes as c (c.k)}
                          <li>
                            <span class="cl">{c.label}</span>
                            <span class="ct"><span style="width:{(c.n / causeMax) * 100}%"></span></span>
                            <span class="cn num">{num(c.n)}</span>
                          </li>
                        {/each}
                      </ul>
                    {/if}
                    {#if deaths.length}
                      <EventFeed events={deaths} {causeLabels} playerHref={null} dense />
                    {:else}
                      <p class="none">Bisher ist niemand gestorben. Erstaunlich.</p>
                    {/if}
                  </div>
                </div>
              {/if}
              {#if s.showChat && chat.length}
                <div class="block">
                  {@render shead('Zuletzt geschrieben', 'Chat')}
                  <div class="card"><EventFeed events={chat} playerHref={null} dense /></div>
                </div>
              {/if}
            </div>
          </div>
        </section>
      {/if}

      {#if showActivity}
        <section class="psec" id="aktivitaet">
          <div class="wrap">
            <div class="srow">
              {@render shead('Spieler gleichzeitig online', 'Aktivität')}
              <div class="seg" role="group" aria-label="Zeitraum">
                <button class:on={range === '24h'} onclick={() => (range = '24h')}>24 Std.</button>
                <button class:on={range === '7d'} onclick={() => (range = '7d')}>7 Tage</button>
                <button class:on={range === '30d'} onclick={() => (range = '30d')}>30 Tage</button>
              </div>
            </div>
            <div class="card chart">
              <AreaChart points={activity} step integer format={(v) => String(Math.round(v))} max={1} height={200} label="Spieler online" color="var(--chart)" />
            </div>
          </div>
        </section>
      {/if}
    </main>
  {/if}

  <footer class="foot">
    <div class="wrap frow">
      <span>© {year} {info.server.panelName}</span>
      {#if info.server.links.length}
        <nav aria-label="Links">
          {#each info.server.links as l (l.url)}
            <a href={l.url} target="_blank" rel="noopener noreferrer nofollow">{l.label}</a>
          {/each}
        </nav>
      {/if}
    </div>
    <div class="wrap fnote">
      {#if info.server.game === 'minecraft'}
        <p>Kein offizielles Minecraft-Produkt. Nicht von Mojang oder Microsoft genehmigt oder mit ihnen verbunden.</p>
      {/if}
      <a href="/licenses/THIRD_PARTY_LICENSES.txt" target="_blank" rel="noopener">Lizenzen</a>
    </div>
  </footer>
{/if}

<style>
  :global(body.public) {
    background: var(--surface);
  }
  .wrap {
    width: 100%;
    max-width: 1280px;
    margin: 0 auto;
    padding-inline: clamp(16px, 4vw, 32px);
  }

  /* ---------- Band oben ---------- */
  .intro {
    position: relative;
    background: var(--band);
    color: #fff;
    overflow-x: clip;
    transition: background-color 0.9s ease;
  }
  .intro :focus-visible {
    outline-color: #fff;
  }
  .hdr {
    position: relative;
    z-index: 2;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px clamp(12px, 2.4vw, 28px);
    padding-block: 20px;
  }
  .logo {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-right: auto;
    min-width: 0;
    color: #fff;
    font: 800 22px/1 var(--font-display);
    letter-spacing: -0.01em;
    text-decoration: none !important;
  }
  .nav {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: clamp(6px, 2.4vw, 28px);
    list-style: none;
    margin: 0;
    padding: 0;
    font-weight: 700;
    font-size: 16px;
  }
  .nav a {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 44px;
    padding-inline: 4px;
    color: #fff;
    text-decoration: none;
    opacity: 0.85;
    transition: opacity 0.2s;
  }
  .nav a::after {
    content: '';
    position: absolute;
    left: 4px;
    right: 4px;
    bottom: 8px;
    height: 2px;
    background: var(--flavor);
    transform: scaleX(0);
    transform-origin: right;
    transition: transform 0.35s var(--ease);
  }
  .nav a:hover {
    opacity: 1;
  }
  .nav a:hover::after {
    transform: scaleX(1);
    transform-origin: left;
  }
  .menu {
    display: none;
    width: 44px;
    height: 44px;
    border-radius: 50%;
    border: 1.5px solid var(--band-line);
    background: transparent;
    color: #fff;
    align-items: center;
    justify-content: center;
    cursor: pointer;
  }
  .menu:hover {
    background: var(--band-hover);
  }

  .hero {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding-block: 0 56px;
  }
  .kick {
    margin: 0;
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.22em;
    text-transform: uppercase;
    color: var(--band-soft);
  }
  .stage {
    position: relative;
    display: grid;
    place-items: center;
    width: 100%;
    height: clamp(200px, 30vh, 300px);
  }
  .glow {
    position: absolute;
    left: 50%;
    top: 50%;
    width: min(440px, 80%);
    aspect-ratio: 1;
    border-radius: 50%;
    transform: translate(-50%, -50%);
    background: var(--flavor);
    opacity: 0.3;
    filter: blur(64px);
    pointer-events: none;
    transition: background-color 0.9s ease;
  }
  .picon {
    position: relative;
    display: grid;
    place-items: center;
    width: 120px;
    height: 120px;
    border-radius: 32px;
    overflow: hidden;
    background: var(--flavor);
    box-shadow: inset -12px 0 16px rgba(0, 0, 0, 0.18), 0 30px 50px -24px rgba(0, 0, 0, 0.7);
  }
  .picon img {
    width: 100%;
    height: 100%;
    image-rendering: pixelated;
  }
  .letter {
    font: 800 54px/1 var(--font-display);
    color: var(--on-flavor);
  }
  .hero h1 {
    margin: 0;
    font: 800 clamp(30px, 5.4vw, 60px) / 1.04 var(--font-display);
    letter-spacing: -0.02em;
    text-wrap: balance;
    overflow-wrap: anywhere;
    max-width: 18ch;
  }
  .motd {
    margin: 12px auto 0;
    max-width: 52ch;
    font-size: 17px;
    line-height: 1.5;
    color: var(--band-soft);
    white-space: pre-line;
  }
  .controls {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    align-items: center;
    gap: 12px;
    margin-top: 26px;
  }
  .status {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    min-height: 52px;
    padding: 0 20px 0 18px;
    border-radius: 999px;
    border: 1.5px solid var(--band-line);
    font-size: 15px;
  }
  .status strong {
    font-weight: 700;
  }
  .soft {
    color: var(--band-soft);
  }
  .beacon {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: rgba(255, 255, 255, 0.45);
  }
  .status.on .beacon {
    background: #3fd6a4;
    box-shadow: 0 0 0 4px rgba(63, 214, 164, 0.25);
  }
  .status.starting .beacon {
    background: #f7b32b;
  }
  .join {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    min-height: 52px;
    padding: 4px 4px 4px 22px;
    border-radius: 999px;
    border: 1.5px solid var(--band-line);
    background: transparent;
    color: #fff;
    font: inherit;
    cursor: pointer;
    transition: background-color 0.2s;
  }
  .join:hover {
    background: var(--band-hover);
  }
  .atext {
    font-family: var(--font-mono);
    font-size: 17px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding-right: 10px;
  }
  .cta {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 42px;
    padding: 0 18px;
    border-radius: 999px;
    background: var(--flavor);
    color: var(--on-flavor);
    font-weight: 700;
    font-size: 14.5px;
    white-space: nowrap;
  }
  .hint {
    margin: 12px 0 0;
    min-height: 1.5em;
    font-size: 14px;
    color: var(--band-soft);
  }
  .online {
    list-style: none;
    margin: 22px 0 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 8px;
    max-width: 900px;
  }
  .online button {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 4px 14px 4px 4px;
    border-radius: 999px;
    border: 1.5px solid var(--band-line);
    background: transparent;
    color: #fff;
    font: inherit;
    font-size: 14px;
    font-weight: 700;
    cursor: pointer;
  }
  .online button:disabled {
    cursor: default;
  }
  .online button:not(:disabled):hover {
    background: var(--band-hover);
  }
  .online :global(img) {
    border-radius: 50%;
  }

  .intro.sub .hdr {
    padding-block: 20px;
  }
  .small-intro {
    min-height: 100svh;
  }
  .missing {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 14px;
    padding-top: 16vh;
  }
  .missing h1 {
    font: 800 clamp(30px, 5vw, 52px) / 1.05 var(--font-display);
    letter-spacing: -0.015em;
  }
  .missing .lead {
    color: var(--band-soft);
  }
  .small-intro .glow {
    top: 60%;
  }

  /* ---------- Heller Bereich (Papier) ---------- */
  .paper {
    background: var(--surface);
    color: var(--text);
  }
  .totals {
    border-bottom: 1px solid var(--line);
  }
  .trow {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  }
  .trow > div {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 30px 0 28px;
  }
  .trow .v {
    font: 800 clamp(24px, 2.6vw, 34px) / 1.05 var(--font-display);
    letter-spacing: -0.015em;
  }
  .trow .l {
    font-size: 14px;
    color: var(--text-2);
  }
  .psec {
    padding-block: clamp(48px, 8vh, 96px) 0;
  }
  .psec:last-child {
    padding-bottom: clamp(56px, 9vh, 110px);
  }
  .about {
    padding-bottom: clamp(16px, 3vh, 30px);
  }
  .agrid {
    display: grid;
    grid-template-columns: minmax(0, 5fr) minmax(0, 7fr);
    gap: clamp(24px, 6vw, 96px);
    align-items: start;
  }
  .agrid h2 {
    margin-top: 14px;
    font: 800 clamp(30px, 4.6vw, 60px) / 1.02 var(--font-display);
    letter-spacing: -0.02em;
    text-wrap: balance;
    overflow-wrap: anywhere;
  }
  .atext2 {
    font-size: clamp(16px, 1.5vw, 19px);
    line-height: 1.65;
    color: var(--text-2);
    max-width: 62ch;
  }
  .atext2 :global(p + p) {
    margin-top: 12px;
  }
  .atext2 :global(a) {
    color: var(--text);
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  .srow {
    display: flex;
    align-items: flex-end;
    gap: 14px;
    flex-wrap: wrap;
    margin-bottom: 20px;
  }
  .srow .shead {
    margin-bottom: 0;
  }
  .shead {
    flex: 1;
    min-width: 220px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin-bottom: 20px;
  }
  .shead h2 {
    font: 800 clamp(28px, 3.4vw, 44px) / 1.04 var(--font-display);
    letter-spacing: -0.02em;
  }
  .cols {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: clamp(28px, 4vw, 56px);
    align-items: start;
  }
  .col {
    display: flex;
    flex-direction: column;
    gap: clamp(40px, 6vh, 72px);
    min-width: 0;
  }
  .card {
    border: 1px solid var(--line);
    border-radius: 18px;
    background: var(--surface);
    overflow: hidden;
  }
  .causes {
    list-style: none;
    margin: 0;
    padding: 18px 20px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-bottom: 1px solid var(--line);
  }
  .causes li {
    display: grid;
    grid-template-columns: 120px 1fr 40px;
    align-items: center;
    gap: 12px;
    font-size: 14px;
  }
  .ct {
    height: 8px;
    border-radius: 999px;
    background: var(--surface-2);
    overflow: hidden;
  }
  .ct span {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: var(--band);
  }
  .cn {
    text-align: right;
    font-weight: 700;
  }
  .none {
    padding: 18px 20px 22px;
    color: var(--text-2);
  }
  .chart {
    padding: 16px 20px 18px;
    --chart: color-mix(in srgb, var(--flavor) 60%, var(--band));
  }
  .extmap {
    display: block;
    width: 100%;
    height: min(70vh, 660px);
    border: 1px solid var(--line);
    border-radius: 18px;
  }
  .subinner {
    padding-block: clamp(32px, 6vw, 64px) clamp(56px, 9vw, 110px);
    display: flex;
    flex-direction: column;
    gap: 24px;
  }
  .backlink {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 40px;
    padding: 0 16px 0 12px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: none;
    color: var(--text);
    font: inherit;
    font-weight: 700;
    cursor: pointer;
  }
  .backlink:hover {
    border-color: var(--text);
  }
  .lead {
    font-size: 18px;
    color: var(--text-2);
  }

  /* ---------- Fußzeile ---------- */
  .foot {
    background: var(--surface);
    border-top: 1px solid var(--line);
    padding-block: 26px 36px;
  }
  .frow {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    gap: 8px 24px;
    font-size: 14px;
    color: var(--text-2);
  }
  .frow nav {
    display: flex;
    flex-wrap: wrap;
    gap: 0 20px;
  }
  .frow a {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    color: var(--text);
    text-decoration: underline;
    text-underline-offset: 4px;
    font-weight: 500;
  }
  .fnote {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    gap: 0 24px;
    margin-top: 6px;
    font-size: 12.5px;
    color: var(--text-2);
  }
  .fnote p {
    margin: 0;
    flex: 1 1 40ch;
    text-wrap: pretty;
  }
  .fnote a {
    display: inline-flex;
    align-items: center;
    min-height: 40px;
    color: var(--text-2);
    text-decoration: underline;
    text-underline-offset: 3px;
  }

  @media (max-width: 860px) {
    .cols,
    .agrid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  /* Handy: Logo und Menüknopf, Navigation klappt darunter auf */
  @media (max-width: 760px) {
    .hdr {
      padding-block: 14px;
      gap: 10px;
    }
    .logo {
      font-size: 19px;
    }
    .menu {
      display: inline-flex;
    }
    .navwrap {
      order: 3;
      flex-basis: 100%;
      display: none;
    }
    .hdr.open .navwrap {
      display: block;
    }
    .nav {
      flex-direction: column;
      align-items: stretch;
      gap: 0;
      font-size: 17px;
      padding-block: 4px 8px;
      border-top: 1px solid var(--band-line);
    }
    .nav a {
      width: 100%;
      min-height: 48px;
      border-bottom: 1px solid var(--band-line);
    }
    .nav li:last-child a {
      border-bottom: 0;
    }
    .nav a::after {
      display: none;
    }
    .stage {
      height: 190px;
    }
    .picon {
      width: 96px;
      height: 96px;
      border-radius: 26px;
    }
    .join {
      width: 100%;
      justify-content: space-between;
    }
    .atext {
      font-size: 15px;
    }
    .causes li {
      grid-template-columns: 96px 1fr 34px;
    }
    .trow > div {
      padding: 18px 0 16px;
    }
  }
</style>
