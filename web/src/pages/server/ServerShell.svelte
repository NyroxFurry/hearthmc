<script lang="ts">
  import { onMount } from 'svelte';
  import {
    Play, Square, RotateCw, Skull, Gauge, TerminalSquare, FolderOpen, Archive, CalendarClock, Users, Puzzle, Map as MapIc,
    Globe2, SlidersHorizontal, FileCog, KeyRound, Megaphone, Bell, ScrollText, Settings, TriangleAlert, RefreshCw, Lock, Package,
  } from '@lucide/svelte';
  import ServerIcon from '../../components/ServerIcon.svelte';
  import StatusBadge from '../../components/StatusBadge.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import { api } from '../../lib/api';
  import { servers, can } from '../../lib/session.svelte';
  import { live } from '../../lib/live.svelte';
  import { toastError } from '../../lib/toast.svelte';
  import { confirmDialog } from '../../lib/dialogs.svelte';
  import type { Server } from '../../lib/types';
  import { flavorLabel } from '../../lib/mc';
  import Overview from './Overview.svelte';
  import Console from './Console.svelte';
  import Files from './Files.svelte';
  import Backups from './Backups.svelte';
  import Schedules from './Schedules.svelte';
  import Players from './Players.svelte';
  import Plugins from './Plugins.svelte';
  import Modpack from './Modpack.svelte';
  import MapPage from './MapPage.svelte';
  import World from './World.svelte';
  import Properties from './Properties.svelte';
  import Startup from './Startup.svelte';
  import Members from './Members.svelte';
  import PublicPage from './PublicPage.svelte';
  import Notifications from './Notifications.svelte';
  import Activity from './Activity.svelte';
  import ServerSettings from './ServerSettings.svelte';

  let { id, section, player = '' }: { id: string; section: string; player?: string } = $props();

  // svelte-ignore state_referenced_locally
  let server = $state<Server | null>(servers.get(id) ?? null);
  let notFound = $state(false);
  let headH = $state(0);
  let status = $derived(live.status[id] ?? server?.status);
  let mc = $derived(server?.game === 'minecraft');
  let modKind = $derived.by(() => {
    const f = server?.settings?.flavor;
    if (f === 'fabric' || f === 'forge' || f === 'neoforge') return 'Mods';
    if (f === 'paper' || f === 'purpur' || f === 'custom') return 'Plugins';
    return '';
  });

  async function reload() {
    try {
      server = await api.get<Server>(`/api/servers/${id}`);
      servers.put(server);
    } catch (e: any) {
      if (e.status === 404) notFound = true;
    }
  }

  onMount(() => {
    reload();
    const off = live.on('servermeta', id, () => reload());
    return off;
  });

  type NavItem = { id: string; label: string; icon: any; perm?: string; show?: boolean };
  let groups = $derived.by((): { label: string; items: NavItem[] }[] => {
    const s = server;
    if (!s) return [];
    const g = [
      { label: '', items: [{ id: 'overview', label: 'Übersicht', icon: Gauge }] },
      {
        label: 'Betrieb',
        items: [
          { id: 'console', label: 'Konsole', icon: TerminalSquare, perm: 'console.read' },
          { id: 'files', label: 'Dateien', icon: FolderOpen, perm: 'files.read' },
          { id: 'backups', label: 'Backups', icon: Archive, perm: 'backups.read' },
          { id: 'schedules', label: 'Zeitpläne', icon: CalendarClock, perm: 'schedules.read' },
        ],
      },
      {
        label: 'Spiel',
        items: [
          { id: 'players', label: 'Spieler', icon: Users, perm: 'players.read', show: mc },
          { id: 'plugins', label: modKind || 'Plugins', icon: Puzzle, perm: 'plugins.manage', show: mc && !!modKind },
          { id: 'modpack', label: 'Modpack', icon: Package, perm: 'plugins.manage', show: mc && modKind === 'Mods' },
          { id: 'map', label: 'Karte', icon: MapIc, show: mc },
          { id: 'world', label: 'Welt', icon: Globe2, perm: 'settings.read', show: mc },
        ],
      },
      {
        label: 'Einstellungen',
        items: [
          { id: 'startup', label: 'Start & Version', icon: SlidersHorizontal, perm: 'settings.read' },
          { id: 'properties', label: 'server.properties', icon: FileCog, perm: 'settings.read', show: mc },
          { id: 'members', label: 'Zugriff', icon: KeyRound, perm: 'members.manage' },
          { id: 'public', label: 'Öffentliche Seite', icon: Megaphone, perm: 'public.manage', show: mc },
          { id: 'notifications', label: 'Benachrichtigungen', icon: Bell, perm: 'webhooks.manage' },
          { id: 'activity', label: 'Aktivität', icon: ScrollText, perm: 'activity.read' },
          { id: 'settings', label: 'Allgemein', icon: Settings },
        ],
      },
    ];
    return g
      .map((grp) => ({ ...grp, items: grp.items.filter((i: NavItem) => i.show !== false && (!i.perm || can(s, i.perm))) }))
      .filter((grp) => grp.items.length);
  });

  async function power(action: string) {
    if (action === 'kill') {
      const ok = await confirmDialog({
        title: 'Prozess hart beenden?',
        text: 'Ungespeicherte Weltdaten gehen verloren. Nur verwenden, wenn der Server nicht mehr reagiert.',
        confirm: 'Beenden erzwingen',
        danger: true,
      });
      if (!ok) return;
    }
    try {
      await api.post(`/api/servers/${id}/power`, { action });
    } catch (e) {
      toastError(e);
    }
  }

  async function reinstall() {
    try {
      await api.post(`/api/servers/${id}/reinstall`);
    } catch (e) {
      toastError(e);
    }
  }

  let address = $derived(server ? server.displayAddress || `${location.hostname}:${server.port}` : '');
</script>

{#if notFound}
  <div class="page"><div class="empty"><h3>Server nicht gefunden</h3><a href="/">Zur Übersicht</a></div></div>
{:else if server && status}
  <div class="server" style="--head-h:{headH}px">
    <header class="head band" bind:offsetHeight={headH}>
      <div class="glow" aria-hidden="true"></div>
      <div class="hicon">
        <ServerIcon id={server.id} name={server.name} color={server.color} size={60} version={server.updatedAt} hasIcon={server.hasIcon} />
      </div>
      <div class="ident">
        <p class="kicker">{mc && server.settings?.flavor ? flavorLabel(server.settings.flavor, server.settings.version) : server.gameName}</p>
        <div class="row">
          <h1 class="ellipsis">{server.name}</h1>
          {#if server.suspended}<span class="tag danger"><Lock size={12} /> Gesperrt</span>{/if}
        </div>
        <div class="meta">
          <StatusBadge {status} />
          {#if status.state === 'installing' && status.installStep}<span class="small soft">{status.installStep}</span>{/if}
          {#if mc && status.state === 'running' && status.players >= 0}
            <span class="small"><span class="num">{status.players}</span><span class="soft">/{status.maxPlayers}</span> Spieler</span>
          {/if}
          <span class="addr">
            <span class="mono">{address}</span>
            <CopyButton text={address} label="" cls="copy" />
          </span>
        </div>
      </div>
      <div class="power">
        {#if status.state === 'offline'}
          <button class="btn primary" disabled={!can(server, 'power.start') || server.installState !== 'installed' || !!status.busy} onclick={() => power('start')}><Play /> Starten</button>
        {:else if status.state !== 'installing'}
          <button class="btn" disabled={!can(server, 'power.stop')} onclick={() => power('restart')}><RotateCw /> Neustart</button>
          <button class="btn" disabled={!can(server, 'power.stop') || status.state === 'stopping'} onclick={() => power('stop')}><Square /> Stoppen</button>
          {#if can(server, 'power.kill')}
            <button class="btn danger icon" title="Hart beenden" aria-label="Hart beenden" onclick={() => power('kill')}><Skull /></button>
          {/if}
        {/if}
      </div>
    </header>

    {#if server.installState === 'failed' && status.state !== 'installing'}
      <div class="notice danger banner">
        <TriangleAlert />
        <div class="stack tight">
          <strong>Installation fehlgeschlagen</strong>
          <span class="small">{server.installError}</span>
        </div>
        {#if can(server, 'settings.edit')}<button class="btn sm" onclick={reinstall}><RefreshCw /> Erneut versuchen</button>{/if}
      </div>
    {:else if status.crashLoop}
      <div class="notice danger banner">
        <TriangleAlert />
        <span>Der Server ist mehrfach abgestürzt. Der automatische Neustart ist pausiert – sieh dir die Konsole an.</span>
      </div>
    {/if}

    <div class="body">
      <nav class="sections" aria-label="Bereiche">
        {#each groups as grp (grp.label)}
          {#if grp.label}<p class="glabel">{grp.label}</p>{/if}
          {#each grp.items as it (it.id)}
            <a class="nav" class:active={section === it.id} aria-current={section === it.id ? 'page' : undefined} href="/servers/{id}{it.id === 'overview' ? '' : '/' + it.id}">
              <it.icon />
              <span>{it.label}</span>
            </a>
          {/each}
        {/each}
      </nav>
      <section class="content">
        {#if section === 'overview'}
          <Overview {server} {status} />
        {:else if section === 'console'}
          <Console {server} {status} />
        {:else if section === 'files'}
          <Files {server} />
        {:else if section === 'backups'}
          <Backups {server} {status} />
        {:else if section === 'schedules'}
          <Schedules {server} />
        {:else if section === 'players'}
          <Players {server} {status} {player} />
        {:else if section === 'plugins'}
          <Plugins {server} {status} />
        {:else if section === 'modpack'}
          <Modpack {server} />
        {:else if section === 'map'}
          <MapPage {server} {status} />
        {:else if section === 'world'}
          <World {server} {status} />
        {:else if section === 'properties'}
          <Properties {server} {status} />
        {:else if section === 'startup'}
          <Startup {server} {status} onsaved={reload} />
        {:else if section === 'members'}
          <Members {server} />
        {:else if section === 'public'}
          <PublicPage {server} onsaved={reload} />
        {:else if section === 'notifications'}
          <Notifications {server} />
        {:else if section === 'activity'}
          <Activity {server} />
        {:else if section === 'settings'}
          <ServerSettings {server} {status} onsaved={reload} />
        {:else}
          <div class="empty"><h3>Bereich nicht gefunden</h3></div>
        {/if}
      </section>
    </div>
  </div>
{/if}

<style>
  .server {
    min-height: 100vh;
  }
  /* Kopf als Sortenband: Farbe des Servers, Lichthof hinter der Kachel */
  .head {
    position: relative;
    overflow: hidden;
    display: flex;
    align-items: center;
    gap: 20px;
    padding: 26px 34px 24px;
    transition: background-color 0.9s ease;
  }
  .glow {
    width: 300px;
    height: 300px;
    left: -60px;
    top: 50%;
    transform: translateY(-50%);
    opacity: 0.38;
  }
  .hicon {
    position: relative;
    flex: none;
    border-radius: 18px;
    box-shadow: 0 10px 30px -10px rgba(0, 0, 0, 0.55);
  }
  .ident {
    position: relative;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .ident .kicker {
    color: var(--band-soft);
    font-size: 11.5px;
  }
  .ident h1 {
    font-size: clamp(24px, 2.6vw, 32px);
    line-height: 1.08;
  }
  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 18px;
    margin-top: 2px;
  }
  .soft {
    color: var(--band-soft);
  }
  .addr {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    height: 30px;
    padding: 0 3px 0 12px;
    border: 1.5px solid var(--band-line);
    border-radius: 999px;
    font-size: 13px;
    color: var(--band-ink);
  }
  .addr :global(.copy) {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: none;
    border-radius: 50%;
    background: none;
    color: var(--band-soft);
    cursor: pointer;
  }
  .addr :global(.copy:hover) {
    background: var(--band-hover);
    color: var(--band-ink);
  }
  .addr :global(.copy svg) {
    width: 14px;
    height: 14px;
  }
  .power {
    position: relative;
    display: flex;
    gap: 8px;
  }
  .banner {
    margin: 18px 34px 0;
    align-items: center;
    flex-wrap: wrap;
  }
  .banner .stack {
    flex: 1 1 260px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .body {
    display: grid;
    grid-template-columns: var(--nav-w) 1fr;
    gap: 0;
    align-items: start;
  }
  .sections {
    position: sticky;
    top: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 18px 16px 30px 0;
    max-height: 100vh;
    overflow-y: auto;
    scrollbar-width: thin;
  }
  .glabel {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    padding: 18px 12px 8px 24px;
    font-size: 11.5px;
    font-weight: 700;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: var(--text-3);
  }
  .glabel::before {
    content: '';
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--flavor);
    flex: none;
  }
  .nav {
    display: flex;
    align-items: center;
    gap: 11px;
    padding: 8px 12px 8px 24px;
    border-radius: 0 999px 999px 0;
    color: var(--text-2);
    font-size: 14.5px;
    font-weight: 500;
    transition: color 0.15s, background-color 0.15s;
  }
  .nav :global(svg) {
    width: 17px;
    height: 17px;
    flex: none;
  }
  .nav:hover {
    color: var(--text);
    text-decoration: none;
    background: color-mix(in srgb, var(--surface) 55%, transparent);
  }
  .nav.active {
    color: var(--text);
    font-weight: 700;
    background: var(--surface);
    box-shadow: inset 3px 0 0 var(--flavor);
  }
  .content {
    min-width: 0;
    padding: 26px 34px 56px 18px;
  }
  @media (max-width: 1000px) {
    .body {
      grid-template-columns: 1fr;
    }
    .sections {
      position: static;
      flex-direction: row;
      overflow-x: auto;
      padding: 6px 16px 0;
      max-height: none;
      border-bottom: 1px solid var(--line);
      gap: 4px;
      scrollbar-width: none;
    }
    .glabel {
      display: none;
    }
    /* wie die Hauptnavigation der Webseite: Unterstrich in der Sortenfarbe */
    .nav {
      position: relative;
      white-space: nowrap;
      border-radius: 0;
      padding: 10px 10px 12px;
      background: none !important;
      box-shadow: none !important;
    }
    .nav::after {
      content: '';
      position: absolute;
      left: 10px;
      right: 10px;
      bottom: -1px;
      height: 2px;
      background: var(--flavor);
      transform: scaleX(0);
      transform-origin: right;
      transition: transform 0.35s var(--ease);
    }
    .nav:hover::after,
    .nav.active::after {
      transform: scaleX(1);
      transform-origin: left;
    }
    .content {
      padding: 20px 16px 40px;
    }
    .head {
      flex-wrap: wrap;
      padding: 20px 16px;
      gap: 14px;
    }
    .banner {
      margin: 14px 16px 0;
    }
  }
  @media (max-width: 640px) {
    .hicon :global(.icon) {
      --s: 48px !important;
    }
    .ident {
      flex-basis: calc(100% - 64px);
    }
    .power {
      width: 100%;
    }
    .power .btn:not(.icon) {
      flex: 1;
    }
  }
</style>
