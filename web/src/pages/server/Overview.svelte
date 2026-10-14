<script lang="ts">
  import { onMount } from 'svelte';
  import { Cpu, MemoryStick, HardDrive, Clock, Users, ExternalLink, Gauge, Megaphone } from '@lucide/svelte';
  import Sparkline from '../../components/Sparkline.svelte';
  import AreaChart from '../../components/AreaChart.svelte';
  import PlayerHead from '../../components/PlayerHead.svelte';
  import EventFeed from '../../components/EventFeed.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import { api, qs } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { can } from '../../lib/session.svelte';
  import { bytes, duration, num1, mb, time } from '../../lib/format';
  import { flavorLabel } from '../../lib/mc';
  import { fillGaps } from '../../lib/series';
  import type { MCEvent, OnlinePlayer, Server, Status } from '../../lib/types';

  let { server, status }: { server: Server; status: Status } = $props();

  const mc = $derived(server.game === 'minecraft');
  const canConsole = $derived(can(server, 'console.read'));
  const canPlayers = $derived(can(server, 'players.read'));
  const canMetrics = $derived(can(server, 'console.read') || can(server, 'settings.read'));
  const running = $derived(status.state === 'running');
  const active = $derived(status.state !== 'offline' && status.state !== 'installing');

  let cpuHist = $state<number[]>([]);
  let memHist = $state<number[]>([]);
  let online = $state<OnlinePlayer[]>([]);
  let tps = $state<number[]>([]);
  let detected = $state('');
  let events = $state<MCEvent[]>([]);
  let causeLabels = $state<Record<string, string>>({});

  type Range = '1h' | '6h' | '24h' | '7d' | '30d';
  let range = $state<Range>('24h');
  // svelte-ignore state_referenced_locally
  let metric = $state<'players' | 'cpu' | 'mem'>(mc ? 'players' : 'cpu');
  let points = $state<{ t: number; cpu: number; mem: number; players: number }[]>([]);

  let now = $state(Date.now() / 1000);

  async function loadStatus() {
    if (!mc) return;
    try {
      const r = await api.get<{ info: { online: OnlinePlayer[]; tps?: number[]; detectedVersion?: string } }>(`/api/servers/${server.id}/mc/status`);
      online = r.info.online ?? [];
      tps = r.info.tps ?? [];
      detected = r.info.detectedVersion ?? '';
    } catch {}
  }

  async function loadEvents() {
    if (!mc || !canPlayers) return;
    try {
      const r = await api.get<{ events: MCEvent[]; causeLabels: Record<string, string> }>(`/api/servers/${server.id}/mc/events?limit=8`);
      events = r.events;
      causeLabels = r.causeLabels;
    } catch {}
  }

  async function loadMetrics() {
    try {
      const r = await api.get<{ points: typeof points; bucket: number }>(`/api/servers/${server.id}/metrics${qs({ range })}`);
      points = fillGaps(r.points, r.bucket, (t) => ({ t, cpu: 0, mem: 0, players: 0 }));
    } catch {}
  }

  $effect(() => {
    range;
    loadMetrics();
  });

  onMount(() => {
    loadStatus();
    loadEvents();
    const offs = [
      live.on('stats', server.id, (e) => {
        cpuHist = [...cpuHist.slice(-59), e.d.cpu || 0];
        memHist = [...memHist.slice(-59), e.d.mem || 0];
      }),
      live.on('players', server.id, (e) => (online = e.d ?? [])),
      live.on('mcevent', server.id, (e) => (events = [e.d, ...events].slice(0, 8))),
      live.on('status', server.id, (e) => {
        if (e.d.state === 'running') setTimeout(loadStatus, 1500);
        if (e.d.state === 'offline') {
          online = [];
          tps = [];
        }
      }),
    ];
    const t = setInterval(() => (now = Date.now() / 1000), 5000);
    const slow = setInterval(() => {
      if (running) loadStatus();
    }, 30000);
    const m = setInterval(loadMetrics, 60000);
    return () => {
      offs.forEach((f) => f());
      clearInterval(t);
      clearInterval(slow);
      clearInterval(m);
    };
  });

  let memLimit = $derived(status.memLimit || server.memoryMb * 1048576);
  let memPct = $derived(memLimit ? Math.min(100, (status.mem / memLimit) * 100) : 0);
  let diskLimit = $derived(server.diskLimitMb > 0 ? server.diskLimitMb * 1048576 : 0);
  let diskPct = $derived(diskLimit ? Math.min(100, (status.disk / diskLimit) * 100) : 0);
  let address = $derived(server.displayAddress || `${location.hostname}:${server.port}`);
  let settings = $derived(server.settings ?? {});

  const ranges: { id: Range; label: string }[] = [
    { id: '1h', label: '1 Std.' },
    { id: '6h', label: '6 Std.' },
    { id: '24h', label: '24 Std.' },
    { id: '7d', label: '7 Tage' },
    { id: '30d', label: '30 Tage' },
  ];

  let chart = $derived.by(() => {
    if (metric === 'players') return { pts: points.map((p) => ({ t: p.t, v: Math.max(0, p.players) })), fmt: (v: number) => String(Math.round(v)), color: 'var(--accent)', step: true, max: 1 };
    if (metric === 'cpu') return { pts: points.map((p) => ({ t: p.t, v: p.cpu })), fmt: (v: number) => num1(v) + ' %', color: 'var(--info)', step: false, max: 5 };
    return { pts: points.map((p) => ({ t: p.t, v: p.mem })), fmt: (v: number) => bytes(v, 1), color: 'var(--copper)', step: false, max: memLimit };
  });

  function tpsClass(v: number) {
    return v >= 19 ? 'good' : v >= 15 ? 'warn' : 'bad';
  }
</script>

<div class="overview">
  <div class="top" class:noconsole={!canConsole}>
    {#if canConsole}
      <div class="console-wrap">
        {#await import('../../components/Terminal.svelte') then { default: Terminal }}
          <Terminal serverId={server.id} canSend={can(server, 'console.command')} running={active} />
        {/await}
      </div>
    {/if}

    <div class="side">
      <section class="panel res">
        <div class="metric">
          <div class="mh"><Cpu /><span>Prozessor</span><span class="spacer"></span><strong class="num">{active ? num1(status.cpu) + ' %' : '–'}</strong></div>
          <Sparkline values={cpuHist} width={240} height={30} color="var(--info)" max={10} />
        </div>
        <div class="metric">
          <div class="mh">
            <MemoryStick /><span>Arbeitsspeicher</span><span class="spacer"></span>
            <strong class="num">{active ? bytes(status.mem) : '–'}</strong><span class="faint small num">&nbsp;/ {bytes(memLimit, 0)}</span>
          </div>
          <div class="meter" class:warn={memPct > 85} class:danger={memPct > 95}><span style="width:{active ? memPct : 0}%"></span></div>
        </div>
        <div class="metric">
          <div class="mh">
            <HardDrive /><span>Speicherplatz</span><span class="spacer"></span>
            <strong class="num">{bytes(status.disk)}</strong>{#if diskLimit}<span class="faint small num">&nbsp;/ {bytes(diskLimit, 0)}</span>{/if}
          </div>
          {#if diskLimit}<div class="meter" class:warn={diskPct > 85} class:danger={diskPct > 95}><span style="width:{diskPct}%"></span></div>{/if}
        </div>
        <div class="metric row">
          <Clock /><span>Laufzeit</span><span class="spacer"></span>
          <strong class="num">{running && status.startedAt ? duration(now - status.startedAt, true) : '–'}</strong>
        </div>
        {#if tps.length}
          <div class="metric row">
            <Gauge /><span>TPS</span><span class="spacer"></span>
            {#each tps.slice(0, 3) as v, i (i)}
              <strong class="num tps {tpsClass(v)}" title={['1 Minute', '5 Minuten', '15 Minuten'][i]}>{num1(v)}</strong>
            {/each}
          </div>
        {/if}
      </section>

      {#if mc && canPlayers}
        <section class="panel players">
          <div class="panel-head">
            <Users />
            <h3>Online</h3>
            <span class="spacer"></span>
            <span class="num small muted">{running ? online.length : 0}{status.maxPlayers > 0 ? ' / ' + status.maxPlayers : ''}</span>
          </div>
          {#if running && online.length}
            <ul class="online">
              {#each online as p (p.uuid)}
                <li>
                  <a href="/servers/{server.id}/players/{p.uuid}">
                    <PlayerHead uuid={p.uuid} name={p.name} size={26} />
                    <span class="ellipsis">{p.name}</span>
                  </a>
                  <span class="tiny faint num" title="Online seit {time(p.since)}">{duration(now - p.since, true)}</span>
                </li>
              {/each}
            </ul>
          {:else}
            <p class="none small muted">{running ? 'Gerade ist niemand online.' : 'Der Server ist offline.'}</p>
          {/if}
        </section>
      {/if}
    </div>
  </div>

  <div class="lower">
    {#if canMetrics || mc}
      <section class="panel history">
        <div class="panel-head wrap">
          <h3>Verlauf</h3>
          <div class="seg" role="group" aria-label="Messwert">
            {#if mc}<button class:on={metric === 'players'} onclick={() => (metric = 'players')}>Spieler</button>{/if}
            {#if canMetrics}
              <button class:on={metric === 'cpu'} onclick={() => (metric = 'cpu')}>Prozessor</button>
              <button class:on={metric === 'mem'} onclick={() => (metric = 'mem')}>Speicher</button>
            {/if}
          </div>
          <span class="spacer"></span>
          <div class="seg" role="group" aria-label="Zeitraum">
            {#each ranges as r (r.id)}
              <button class:on={range === r.id} onclick={() => (range = r.id)}>{r.label}</button>
            {/each}
          </div>
        </div>
        <div class="panel-body">
          {#if chart.pts.length > 1}
            <AreaChart points={chart.pts} format={chart.fmt} color={chart.color} step={chart.step} max={chart.max} integer={metric === 'players'} height={190} />
          {:else}
            <p class="muted small chart-empty">Für diesen Zeitraum gibt es noch keine Messwerte. Sie werden jede Minute gesammelt, solange das Panel läuft.</p>
          {/if}
        </div>
      </section>
    {/if}

    <div class="cols">
      <section class="panel details">
        <div class="panel-head"><h3>Details</h3></div>
        <dl>
          <dt>Adresse</dt>
          <dd class="addr"><span class="mono ellipsis">{address}</span><CopyButton text={address} cls="btn ghost sm icon" /></dd>
          {#if mc}
            <dt>Software</dt>
            <dd>
              {flavorLabel(settings.flavor, settings.version)}
              {#if settings.installedBuild && settings.flavor !== 'vanilla'}<span class="faint">, Build {settings.installedBuild}</span>{/if}
            </dd>
            {#if detected && detected !== settings.version}
              <dt>Erkannt</dt>
              <dd>{detected}</dd>
            {/if}
            {#if settings.javaMajor}
              <dt>Java</dt>
              <dd>Version {settings.javaMajor}{settings.java && settings.java !== 'auto' ? (settings.java === 'system' ? ' (System)' : ' (eigener Pfad)') : ''}</dd>
            {/if}
          {:else}
            <dt>Typ</dt>
            <dd>{server.gameName}</dd>
          {/if}
          <dt>Ausführung</dt>
          <dd>{server.runtime === 'docker' ? 'Docker-Container' : 'Direkt auf dem Host'}</dd>
          <dt>Speicherlimit</dt>
          <dd>{mb(server.memoryMb)}{server.cpuLimit > 0 ? `, ${num1(server.cpuLimit)} Kerne` : ''}</dd>
          <dt>Besitzer</dt>
          <dd>{server.ownerName || '–'}</dd>
          <dt>Autostart</dt>
          <dd>{server.autostart ? 'Startet mit dem Panel' : 'Aus'}{server.autoRestart ? ', Neustart nach Absturz' : ''}</dd>
          {#if server.publicUrl}
            <dt>Öffentlich</dt>
            <dd><a href={server.publicUrl} target="_blank" rel="noopener" class="row tight-link"><Megaphone size={14} /> Statusseite <ExternalLink size={13} /></a></dd>
          {/if}
        </dl>
      </section>

      {#if mc && canPlayers}
        <section class="panel recent">
          <div class="panel-head">
            <h3>Letzte Ereignisse</h3>
            <span class="spacer"></span>
            <a class="small" href="/servers/{server.id}/players?tab=events">Alle</a>
          </div>
          {#if events.length}
            <EventFeed {events} {causeLabels} playerHref={(u) => `/servers/${server.id}/players/${u}`} dense />
          {:else}
            <p class="panel-body small muted">Beitritte, Tode und Fortschritte erscheinen hier, sobald jemand spielt.</p>
          {/if}
        </section>
      {/if}
    </div>
  </div>
</div>

<style>
  .overview {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .top {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 300px;
    gap: 18px;
    align-items: stretch;
  }
  .top.noconsole {
    grid-template-columns: 1fr 1fr;
  }
  .console-wrap {
    height: 470px;
    min-width: 0;
  }
  .side {
    display: flex;
    flex-direction: column;
    gap: 18px;
    min-width: 0;
  }
  .res {
    padding: 6px 0;
  }
  .metric {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 11px 16px;
  }
  .metric + .metric {
    border-top: 1px solid var(--line);
  }
  .metric.row {
    flex-direction: row;
    align-items: center;
    gap: 8px;
    font-size: 14px;
  }
  .mh {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
  }
  .mh > :global(svg),
  .metric.row > :global(svg) {
    width: 16px;
    height: 16px;
    color: var(--text-3);
    flex: none;
  }
  .tps {
    margin-left: 8px;
  }
  .tps.good {
    color: var(--accent);
  }
  .tps.warn {
    color: var(--warn);
  }
  .tps.bad {
    color: var(--danger);
  }
  .players {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .players .panel-head :global(svg) {
    width: 16px;
    height: 16px;
    color: var(--text-3);
  }
  .online {
    list-style: none;
    margin: 0;
    padding: 6px 0;
    overflow-y: auto;
    max-height: 260px;
  }
  .online li {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 5px 16px;
  }
  .online a {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text);
    font-weight: 560;
  }
  .online a:hover {
    color: var(--accent);
    text-decoration: none;
  }
  .none {
    padding: 14px 16px 18px;
  }
  .panel-head.wrap {
    flex-wrap: wrap;
    row-gap: 10px;
  }
  .chart-empty {
    padding: 50px 10px;
    text-align: center;
  }
  .cols {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.25fr);
    gap: 18px;
    align-items: start;
    margin-top: 18px;
  }
  .details dl {
    display: grid;
    grid-template-columns: 120px 1fr;
    gap: 9px 14px;
    margin: 0;
    padding: 14px 18px 16px;
    font-size: 14px;
  }
  .details dt {
    color: var(--text-3);
  }
  .details dd {
    margin: 0;
    min-width: 0;
  }
  .addr {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: -4px !important;
  }
  .tight-link {
    display: inline-flex;
    gap: 6px;
  }
  @media (max-width: 1150px) {
    .top,
    .top.noconsole {
      grid-template-columns: 1fr;
    }
    .cols {
      grid-template-columns: 1fr;
    }
  }
</style>
