<script lang="ts">
  import { Plus, Shield, LayoutGrid, LogOut, Sun, Moon, Monitor, UserRound } from '@lucide/svelte';
  import Flame from './Flame.svelte';
  import ServerIcon from './ServerIcon.svelte';
  import { router, go } from '../lib/router.svelte';
  import { session, servers, applyTheme, themePref, type ThemePref } from '../lib/session.svelte';
  import { live } from '../lib/live.svelte';

  let route = $derived(router.route);
  let activeId = $derived(route.params.id ?? '');
  let menu = $state(false);
  let theme = $state<ThemePref>(themePref());

  const stateLabels: Record<string, string> = {
    running: 'läuft',
    starting: 'startet',
    stopping: 'stoppt',
    installing: 'wird installiert',
    offline: 'offline',
  };

  function stateOf(id: string, fallback: string) {
    return live.status[id]?.state ?? fallback;
  }

  function setTheme(t: ThemePref) {
    theme = t;
    applyTheme(t);
  }

  async function logout() {
    await session.logout();
    live.disconnect();
    go('/login');
  }
</script>

<svelte:window onclick={(e) => menu && !(e.target as HTMLElement).closest('.account') && (menu = false)} />

<nav class="rail band" aria-label="Server">
  <a class="brand" href="/" title={session.panel.name} aria-label="Übersicht"><Flame size={34} /></a>
  <div class="servers">
    <a class="item" class:active={route.name === 'dashboard'} href="/" title="Alle Server" aria-current={route.name === 'dashboard' ? 'page' : undefined}>
      <span class="tile"><LayoutGrid /></span>
    </a>
    {#each servers.list as s (s.id)}
      {@const st = stateOf(s.id, s.status.state)}
      <a class="item" class:active={activeId === s.id} href="/servers/{s.id}" title="{s.name} ({stateLabels[st] ?? st})" aria-current={activeId === s.id ? 'page' : undefined}>
        <ServerIcon id={s.id} name={s.name} color={s.color} size={44} hasIcon={s.hasIcon} version={s.updatedAt} />
        <span class="dot {st}" aria-hidden="true"></span>
        <span class="sr-only">{s.name}, {stateLabels[st] ?? st}</span>
      </a>
    {/each}
    {#if session.user?.isAdmin || session.user?.canCreateServers}
      <a class="item" class:active={route.name === 'create'} href="/servers/new" title="Neuer Server">
        <span class="tile add"><Plus /></span>
      </a>
    {/if}
  </div>
  <div class="bottom">
    {#if session.user?.isAdmin}
      <a class="item" class:active={route.name.startsWith('admin')} href="/admin" title="Administration">
        <span class="tile"><Shield /></span>
      </a>
    {/if}
    <div class="account">
      <button class="avatar" class:active={route.name === 'account'} onclick={() => (menu = !menu)} aria-haspopup="menu" aria-expanded={menu} title={session.user?.username}>
        {(session.user?.username ?? '?').slice(0, 1).toUpperCase()}
      </button>
      {#if menu}
        <div class="pop" role="menu">
          <div class="who">
            <strong>{session.user?.username}</strong>
            <span class="faint small">{session.user?.isAdmin ? 'Administrator' : 'Benutzer'}</span>
          </div>
          <a class="pitem" href="/account" onclick={() => (menu = false)}><UserRound /> Konto & Sicherheit</a>
          <div class="themes" role="group" aria-label="Darstellung">
            <button class:on={theme === 'system'} onclick={() => setTheme('system')} title="Wie System" aria-label="Wie System"><Monitor /></button>
            <button class:on={theme === 'dark'} onclick={() => setTheme('dark')} title="Dunkel" aria-label="Dunkel"><Moon /></button>
            <button class:on={theme === 'light'} onclick={() => setTheme('light')} title="Hell" aria-label="Hell"><Sun /></button>
          </div>
          <button class="pitem" onclick={logout}><LogOut /> Abmelden</button>
        </div>
      {/if}
    </div>
  </div>
</nav>

<style>
  .rail {
    position: sticky;
    top: 0;
    height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 18px 0 16px;
    z-index: 30;
    transition: background-color 0.9s ease;
  }
  .brand {
    display: grid;
    place-items: center;
    width: 48px;
    height: 48px;
    color: var(--band-ink);
    margin-bottom: 6px;
  }
  .brand:hover {
    text-decoration: none;
  }
  .servers {
    flex: 1;
    min-height: 0;
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    overflow-y: auto;
    scrollbar-width: none;
    padding: 4px 0;
  }
  .item {
    position: relative;
    display: grid;
    place-items: center;
    width: 100%;
    color: var(--band-ink);
  }
  .item:hover {
    text-decoration: none;
  }
  /* Punkt wie im Sorten-Karussell: wird zur Pille, wenn die Seite aktiv ist */
  .item::before {
    content: '';
    position: absolute;
    left: 7px;
    top: 50%;
    width: 6px;
    height: 6px;
    margin-top: -3px;
    border-radius: 3px;
    background: var(--band-line);
    opacity: 0;
    transition: height 0.35s var(--ease), margin-top 0.35s var(--ease), opacity 0.2s, background-color 0.35s ease;
  }
  .item:hover::before {
    opacity: 1;
  }
  .item.active::before {
    height: 26px;
    margin-top: -13px;
    opacity: 1;
    background: var(--flavor);
  }
  .item > :global(.icon) {
    transition: transform 0.2s var(--ease);
  }
  .item:hover > :global(.icon) {
    transform: translateY(-1px);
  }
  .tile {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border-radius: 50%;
    border: 1.5px solid var(--band-line);
    color: var(--band-ink);
    transition: background-color 0.2s, color 0.2s, border-color 0.2s;
  }
  .tile :global(svg) {
    width: 19px;
    height: 19px;
  }
  .item:hover .tile {
    background: var(--band-hover);
  }
  .item.active .tile {
    background: var(--band-ink);
    border-color: var(--band-ink);
    color: var(--band);
  }
  .tile.add {
    border-style: dashed;
  }
  .dot {
    position: absolute;
    left: calc(50% + 13px);
    bottom: -4px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    border: 3px solid var(--band);
    background: #9aa3a8;
    transition: border-color 0.9s ease;
  }
  .dot.running {
    background: #3fd6a4;
  }
  .dot.starting,
  .dot.stopping {
    background: #f7b32b;
  }
  .dot.installing {
    background: #a98bf0;
  }
  .bottom {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    width: 100%;
  }
  .account {
    position: relative;
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border-radius: 50%;
    border: 1.5px solid var(--band-line);
    background: var(--band-hover);
    color: var(--band-ink);
    font-family: var(--font-display);
    font-weight: 700;
    font-size: 16px;
    cursor: pointer;
    transition: background-color 0.2s;
  }
  .avatar:hover,
  .avatar.active {
    background: rgba(255, 255, 255, 0.24);
  }
  .pop {
    position: absolute;
    left: calc(100% + 14px);
    bottom: 0;
    width: 240px;
    padding: 6px;
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: var(--r-l);
    box-shadow: var(--pop-shadow);
    z-index: 80;
  }
  .who {
    display: flex;
    flex-direction: column;
    padding: 8px 10px 10px;
    border-bottom: 1px solid var(--line);
    margin-bottom: 5px;
  }
  .who strong {
    font-family: var(--font-display);
    font-size: 15px;
  }
  .pitem {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px 10px;
    border: none;
    background: none;
    border-radius: 10px;
    color: var(--text);
    font: inherit;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    text-align: left;
  }
  .pitem:hover {
    background: var(--surface-2);
    text-decoration: none;
  }
  .pitem :global(svg) {
    width: 16px;
    height: 16px;
    color: var(--text-2);
  }
  .themes {
    display: flex;
    gap: 2px;
    margin: 4px 6px 6px;
    padding: 3px;
    border-radius: 999px;
    background: var(--surface-2);
  }
  .themes button {
    flex: 1;
    display: grid;
    place-items: center;
    height: 28px;
    border: none;
    border-radius: 999px;
    background: none;
    color: var(--text-3);
    cursor: pointer;
  }
  .themes button :global(svg) {
    width: 15px;
    height: 15px;
  }
  .themes button.on {
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.16);
  }

  @media (max-width: 860px) {
    .rail {
      height: auto;
      flex-direction: row;
      padding: 10px 14px;
      gap: 10px;
    }
    .brand {
      width: 40px;
      height: 40px;
      margin: 0;
    }
    .servers {
      flex-direction: row;
      overflow-x: auto;
      overflow-y: visible;
      padding: 4px 4px 10px;
      margin-bottom: -6px;
      gap: 10px;
    }
    .item {
      width: auto;
    }
    /* auf dem Handy liegt der Punkt unter der Kachel, wie die Karussell-Punkte */
    .item::before {
      left: 50%;
      top: auto;
      bottom: -9px;
      width: 6px;
      height: 4px;
      margin: 0 0 0 -3px;
      transition: width 0.35s var(--ease), margin-left 0.35s var(--ease), opacity 0.2s, background-color 0.35s ease;
    }
    .item.active::before {
      width: 22px;
      height: 4px;
      margin: 0 0 0 -11px;
    }
    .tile,
    .avatar {
      width: 40px;
      height: 40px;
    }
    .item > :global(.icon) {
      --s: 40px !important;
    }
    .dot {
      left: auto;
      right: -4px;
    }
    .bottom {
      flex-direction: row;
      width: auto;
      gap: 10px;
    }
    .pop {
      left: auto;
      right: 0;
      bottom: auto;
      top: calc(100% + 10px);
    }
  }
</style>
