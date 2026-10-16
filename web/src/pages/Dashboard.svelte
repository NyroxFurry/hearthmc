<script lang="ts">
  import { onMount } from 'svelte';
  import { Plus, Play, Square, RotateCw, Server as ServerIc, Users } from '@lucide/svelte';
  import ServerIcon from '../components/ServerIcon.svelte';
  import StatusBadge from '../components/StatusBadge.svelte';
  import Sparkline from '../components/Sparkline.svelte';
  import PageBand from '../components/PageBand.svelte';
  import { servers, session, can } from '../lib/session.svelte';
  import { live } from '../lib/live.svelte';
  import { api } from '../lib/api';
  import { bytes, num1, duration, memUsage } from '../lib/format';
  import { toastError } from '../lib/toast.svelte';
  import { flavorLabel } from '../lib/mc';
  import type { Server } from '../lib/types';

  // short CPU history per server from live stats
  let cpu = $state<Record<string, number[]>>({});

  onMount(() =>
    live.on('stats', null, (e) => {
      if (!e.s) return;
      const arr = cpu[e.s] ?? [];
      arr.push(e.d.cpu || 0);
      if (arr.length > 40) arr.shift();
      cpu[e.s] = arr;
    }),
  );

  const st = (s: Server) => live.status[s.id] ?? s.status;

  let running = $derived(servers.list.filter((s) => st(s).state === 'running').length);
  let players = $derived(servers.list.reduce((a, s) => a + Math.max(0, st(s).state === 'running' ? st(s).players : 0), 0));

  async function power(s: Server, action: string) {
    try {
      await api.post(`/api/servers/${s.id}/power`, { action });
    } catch (e) {
      toastError(e);
    }
  }

  function address(s: Server) {
    return s.displayAddress || `Port ${s.port}`;
  }

  let now = $state(Date.now() / 1000);
  onMount(() => {
    const t = setInterval(() => (now = Date.now() / 1000), 10000);
    return () => clearInterval(t);
  });
</script>

<PageBand title="Server" kicker={session.panel.name || 'Hearth'}>
  {#snippet sub()}
    {#if servers.loaded && servers.list.length}
      {running} von {servers.list.length} laufen{#if players > 0}, {players} Spieler online{/if}.
    {:else if servers.loaded}
      Hier erscheinen alle Server, auf die du Zugriff hast.
    {/if}
  {/snippet}
  {#snippet actions()}
    {#if session.user?.isAdmin || session.user?.canCreateServers}
      <a class="btn primary" href="/servers/new"><Plus /> Neuer Server</a>
    {/if}
  {/snippet}
</PageBand>

<div class="page">
  {#if servers.loaded && servers.list.length === 0}
    <div class="panel">
      <div class="empty">
        <ServerIc />
        <h3>Noch keine Server</h3>
        {#if session.user?.isAdmin || session.user?.canCreateServers}
          <p>Lege deinen ersten Minecraft-Server an: Version wählen, Speicher festlegen, fertig.</p>
          <a class="btn primary" href="/servers/new" style="margin-top:8px"><Plus /> Server anlegen</a>
        {:else}
          <p>Dir wurde noch kein Server freigegeben. Frag einen Administrator nach Zugriff.</p>
        {/if}
      </div>
    </div>
  {:else}
    <ul class="cards">
      {#each servers.list as s (s.id)}
        {@const status = st(s)}
        {@const on = status.state === 'running'}
        {@const memory = memUsage(status, s.memoryMb)}
        <li class="srow">
          <a class="ident" href="/servers/{s.id}">
            <ServerIcon id={s.id} name={s.name} color={s.color} size={48} hasIcon={s.hasIcon} version={s.updatedAt} />
            <span class="names">
              <span class="name ellipsis">{s.name}</span>
              <span class="sub small faint ellipsis">{s.game === 'minecraft' && s.settings?.flavor ? flavorLabel(s.settings.flavor, s.settings.version) : s.gameName}</span>
            </span>
          </a>
          <div class="cell state-cell">
            <StatusBadge {status} />
            <span class="tiny faint">
              {#if on && status.startedAt}seit {duration(now - status.startedAt, true)}{:else if status.state === 'installing'}{status.installStep || 'Installation'}{:else}{address(s)}{/if}
            </span>
          </div>
          <div class="cell players">
            {#if s.game === 'minecraft'}
              <Users />
              <span class="num">{on && status.players >= 0 ? status.players : '–'}<span class="faint">/{status.maxPlayers > 0 ? status.maxPlayers : '–'}</span></span>
            {/if}
          </div>
          <div class="cell res">
            <div class="cpu">
              <Sparkline values={cpu[s.id] ?? []} width={84} height={24} />
              <span class="tiny num">{on ? num1(status.cpu) + ' %' : '–'}</span>
            </div>
            <div class="mem" title={memory.heap ? 'Genutzter Java-Heap' : 'Arbeitsspeicher des Prozesses'}>
              <div class="meter" class:warn={memory.total > 0 && memory.used / memory.total > 0.85}>
                <span style="width:{on && memory.total ? Math.min(100, (memory.used / memory.total) * 100) : 0}%"></span>
              </div>
              <span class="tiny faint num">{on ? bytes(memory.used) : bytes(0)} / {bytes(memory.total, 0)}</span>
            </div>
          </div>
          <div class="cell actions">
            {#if status.state === 'offline'}
              <button class="btn sm" disabled={!can(s, 'power.start') || s.installState !== 'installed'} onclick={() => power(s, 'start')}><Play /> Starten</button>
            {:else if status.state === 'running' || status.state === 'starting'}
              <button class="btn sm icon" title="Neu starten" aria-label="Neu starten" disabled={!can(s, 'power.stop')} onclick={() => power(s, 'restart')}><RotateCw /></button>
              <button class="btn sm" disabled={!can(s, 'power.stop')} onclick={() => power(s, 'stop')}><Square /> Stoppen</button>
            {/if}
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .cards {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }
  .srow {
    display: grid;
    grid-template-columns: minmax(220px, 1.6fr) minmax(120px, 0.8fr) 80px minmax(200px, 1.1fr) 180px;
    align-items: center;
    gap: 16px;
    padding: 14px 18px 14px 14px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--r-l);
    transition: border-color 0.2s;
  }
  .srow:hover {
    border-color: var(--line-2);
  }
  .ident {
    display: flex;
    align-items: center;
    gap: 14px;
    color: inherit;
    min-width: 0;
  }
  .ident:hover {
    text-decoration: none;
  }
  .ident:hover .name {
    color: var(--accent);
  }
  .names {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .name {
    font-family: var(--font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.005em;
    transition: color 0.15s;
  }
  .cell {
    min-width: 0;
  }
  .state-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .players {
    display: flex;
    align-items: center;
    gap: 7px;
    font-weight: 700;
  }
  .players :global(svg) {
    width: 16px;
    height: 16px;
    color: var(--text-3);
  }
  .res {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 14px;
    align-items: center;
  }
  .cpu {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .mem {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
  }
  @media (max-width: 1100px) {
    .srow {
      grid-template-columns: 1fr auto;
      grid-template-areas: 'ident actions' 'state players';
      row-gap: 10px;
    }
    .ident {
      grid-area: ident;
    }
    .state-cell {
      grid-area: state;
      padding-left: 62px;
    }
    .players {
      grid-area: players;
      justify-content: flex-end;
    }
    .actions {
      grid-area: actions;
    }
    .res {
      display: none;
    }
  }
</style>
