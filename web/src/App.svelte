<script lang="ts">
  import { onMount } from 'svelte';
  import { router, defineRoutes, go } from './lib/router.svelte';
  import { session, servers } from './lib/session.svelte';
  import { live } from './lib/live.svelte';
  import { setUnauthorizedHandler } from './lib/api';
  import { brandFlavor, flavorFor, flavorVars } from './lib/flavors';
  import Toasts from './components/Toasts.svelte';
  import Dialogs from './components/Dialogs.svelte';
  import Rail from './components/Rail.svelte';
  import Setup from './pages/Setup.svelte';
  import Login from './pages/Login.svelte';
  import Invite from './pages/Invite.svelte';
  import Dashboard from './pages/Dashboard.svelte';
  import CreateServer from './pages/CreateServer.svelte';
  import Account from './pages/Account.svelte';
  import Admin from './pages/admin/Admin.svelte';
  import ServerShell from './pages/server/ServerShell.svelte';

  defineRoutes({
    setup: '/setup',
    login: '/login',
    invite: '/invite/:token',
    dashboard: '/',
    create: '/servers/new',
    server: '/servers/:id',
    section: '/servers/:id/:section',
    player: '/servers/:id/players/:uuid',
    account: '/account',
    admin: '/admin',
    adminTab: '/admin/:tab',
  });

  const authRoutes = ['setup', 'login', 'invite'];
  let route = $derived(router.route);

  onMount(() => {
    router.init();
    setUnauthorizedHandler(() => {
      session.user = null;
      live.disconnect();
      if (!authRoutes.includes(router.route.name)) go('/login?next=' + encodeURIComponent(location.pathname), true);
    });
    session.load();
  });

  // routing guards
  $effect(() => {
    if (!session.ready) return;
    const name = route.name;
    if (session.needsSetup) {
      if (name !== 'setup') go('/setup', true);
      return;
    }
    if (!session.user) {
      if (!authRoutes.includes(name)) go('/login' + (route.path !== '/' ? '?next=' + encodeURIComponent(route.path) : ''), true);
      return;
    }
    if (name === 'login' || name === 'setup') go('/', true);
  });

  $effect(() => {
    if (session.user) {
      live.connect();
      servers.load();
    }
  });

  // keep the server list fresh when servers are created, changed or removed
  onMount(() =>
    live.on('servermeta', null, () => {
      servers.load();
    }),
  );

  $effect(() => {
    const name = session.panel.name || 'Hearth';
    const srv = route.params.id ? servers.get(route.params.id) : undefined;
    document.title = srv ? `${srv.name} – ${name}` : name;
  });

  // Das Band folgt der aktiven Sorte: im Server dessen Farbe, sonst Minze.
  let activeServer = $derived(route.params.id ? servers.get(route.params.id) : undefined);
  let flavor = $derived(activeServer ? flavorFor(activeServer.color, activeServer.id) : brandFlavor);
  $effect(() => {
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', flavor.bg);
  });
</script>

{#if !session.ready}
  <div class="boot" aria-busy="true"></div>
{:else if authRoutes.includes(route.name) || !session.user}
  {#if route.name === 'setup'}
    <Setup />
  {:else if route.name === 'invite'}
    <Invite token={route.params.token} />
  {:else}
    <Login />
  {/if}
{:else}
  <div class="shell" style={flavorVars(flavor)}>
    <Rail />
    <main class="main">
      {#if route.name === 'dashboard'}
        <Dashboard />
      {:else if route.name === 'create'}
        <CreateServer />
      {:else if route.name === 'server' || route.name === 'section' || route.name === 'player'}
        {#key route.params.id}
          <ServerShell id={route.params.id} section={route.name === 'player' ? 'players' : route.params.section || 'overview'} player={route.params.uuid} />
        {/key}
      {:else if route.name === 'account'}
        <Account />
      {:else if route.name === 'admin' || route.name === 'adminTab'}
        <Admin tab={route.params.tab || 'users'} />
      {:else}
        <div class="empty">
          <h3>Seite nicht gefunden</h3>
          <a href="/">Zur Übersicht</a>
        </div>
      {/if}
    </main>
  </div>
{/if}

<Toasts />
<Dialogs />

<style>
  .boot {
    height: 100vh;
  }
  .shell {
    display: grid;
    grid-template-columns: var(--rail-w) 1fr;
    min-height: 100vh;
    /* hält die Leistenfarbe auch unterhalb der mitlaufenden Leiste */
    background: linear-gradient(90deg, var(--band) var(--rail-w), transparent var(--rail-w));
  }
  .main {
    min-width: 0;
  }
  @media (max-width: 860px) {
    .shell {
      grid-template-columns: 1fr;
      grid-template-rows: auto 1fr;
      background: none;
    }
  }
</style>
