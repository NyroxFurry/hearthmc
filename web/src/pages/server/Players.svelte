<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import {
    Users, Search, RefreshCw, ArrowLeft, UserX, Ban, Crown, ShieldCheck, ShieldOff, UserPlus, Undo2, ArrowUp, ArrowDown, Gavel, Network,
  } from '@lucide/svelte';
  import Tabs from '../../components/Tabs.svelte';
  import PlayerHead from '../../components/PlayerHead.svelte';
  import PlayerProfile from '../../components/PlayerProfile.svelte';
  import EventFeed from '../../components/EventFeed.svelte';
  import Toggle from '../../components/Toggle.svelte';
  import Menu, { type MenuItem } from '../../components/Menu.svelte';
  import { api, qs } from '../../lib/api';
  import { router } from '../../lib/router.svelte';
  import { live } from '../../lib/live.svelte';
  import { can } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { promptDialog, confirmDialog } from '../../lib/dialogs.svelte';
  import { ago, dateTime, hours, num, distance } from '../../lib/format';
  import { statCols, statByKey, statValue } from '../../lib/mc';
  import type { MCEvent, MCPlayer, OnlinePlayer, PlayerDetail, Server, Status } from '../../lib/types';

  let { server, status, player = '' }: { server: Server; status: Status; player?: string } = $props();

  const mcUrl = $derived(`/api/servers/${server.id}/mc`);
  const canManage = $derived(can(server, 'players.manage'));
  const running = $derived(status.state === 'running');

  let tab = $state(router.route.query.get('tab') ?? 'board');
  function setTab(t: string) {
    router.setQuery({ tab: t === 'board' ? null : t });
  }
  $effect(() => {
    tab = router.route.query.get('tab') ?? 'board';
  });

  // ---- lists (whitelist, ops, bans) ----
  interface ListEntry {
    uuid?: string;
    name?: string;
    ip?: string;
    level?: number;
    created?: string;
    source?: string;
    expires?: string;
    reason?: string;
  }
  let lists = $state<{ whitelist: ListEntry[]; ops: ListEntry[]; bans: ListEntry[]; ipbans: ListEntry[]; whitelistEnabled: boolean; onlineMode: boolean }>({
    whitelist: [],
    ops: [],
    bans: [],
    ipbans: [],
    whitelistEnabled: false,
    onlineMode: true,
  });

  async function loadLists() {
    try {
      lists = await api.get(`${mcUrl}/lists`);
    } catch {}
  }

  const inList = (l: ListEntry[], uuid: string, name: string) =>
    l.some((e) => (e.uuid && e.uuid.toLowerCase() === uuid.toLowerCase()) || (e.name && e.name.toLowerCase() === name.toLowerCase()));

  async function action(act: string, body: { name?: string; uuid?: string; ip?: string; reason?: string }, okText: string) {
    try {
      const r = await api.post<{ result: string }>(`${mcUrl}/action`, { action: act, ...body });
      toastOk(r.result && r.result !== 'Befehl gesendet' && running ? `${okText}: ${r.result.replace(/§./g, '')}` : okText);
    } catch (e) {
      toastError(e);
    } finally {
      setTimeout(() => {
        loadLists();
        if (player) loadDetail();
        else loadBoard(true);
      }, 700);
    }
  }

  async function kick(p: { name: string; uuid: string }) {
    const reason = await promptDialog({ title: `${p.name} kicken`, label: 'Grund (optional)', confirm: 'Kicken', optional: true });
    if (reason !== null) action('kick', { name: p.name, uuid: p.uuid, reason }, `${p.name} wurde gekickt`);
  }
  async function ban(p: { name: string; uuid: string }) {
    const reason = await promptDialog({ title: `${p.name} sperren`, label: 'Grund (optional)', hint: 'Der Spieler kann den Server nicht mehr betreten, bis die Sperre aufgehoben wird.', confirm: 'Sperren', optional: true });
    if (reason !== null) action('ban', { name: p.name, uuid: p.uuid, reason }, `${p.name} wurde gesperrt`);
  }

  function playerMenu(p: { name: string; uuid: string; online?: boolean }): MenuItem[] {
    const items: MenuItem[] = [];
    const isOp = inList(lists.ops, p.uuid, p.name);
    const banned = inList(lists.bans, p.uuid, p.name);
    const wl = inList(lists.whitelist, p.uuid, p.name);
    if (p.online) items.push({ label: 'Kicken', icon: UserX, onclick: () => kick(p) });
    items.push(
      isOp
        ? { label: 'Operator-Rechte entziehen', icon: ShieldOff, onclick: () => action('deop', p, `${p.name} ist kein Operator mehr`) }
        : { label: 'Zum Operator machen', icon: Crown, onclick: () => action('op', p, `${p.name} ist jetzt Operator`) },
    );
    items.push(
      wl
        ? { label: 'Von der Whitelist entfernen', icon: ShieldOff, onclick: () => action('whitelist_remove', p, `${p.name} von der Whitelist entfernt`) }
        : { label: 'Zur Whitelist hinzufügen', icon: ShieldCheck, onclick: () => action('whitelist_add', p, `${p.name} zur Whitelist hinzugefügt`) },
    );
    items.push({ divider: true, label: '' });
    items.push(
      banned
        ? { label: 'Sperre aufheben', icon: Undo2, onclick: () => action('pardon', p, `Sperre von ${p.name} aufgehoben`) }
        : { label: 'Sperren', icon: Ban, danger: true, onclick: () => ban(p) },
    );
    return items;
  }

  // ---- leaderboard ----
  let players = $state<MCPlayer[]>([]);
  let total = $state(0);
  let totals = $state<Record<string, number> | null>(null);
  let online = $state<OnlinePlayer[]>([]);
  let q = $state('');
  let sort = $state('playtime');
  let asc = $state(false);
  let boardLoading = $state(false);
  let refreshing = $state(false);
  const pageSize = 50;

  async function loadBoard(keep = false) {
    boardLoading = true;
    try {
      const r = await api.get<{ players: MCPlayer[]; total: number; online: OnlinePlayer[]; totals: Record<string, number> }>(
        `${mcUrl}/players${qs({ sort, dir: asc ? 'asc' : 'desc', q, limit: keep ? Math.max(pageSize, players.length) : pageSize })}`,
      );
      players = r.players;
      total = r.total;
      online = r.online;
      totals = r.totals;
    } catch (e) {
      toastError(e);
    } finally {
      boardLoading = false;
    }
  }

  async function more() {
    try {
      const r = await api.get<{ players: MCPlayer[] }>(`${mcUrl}/players${qs({ sort, dir: asc ? 'asc' : 'desc', q, limit: pageSize, offset: players.length })}`);
      players = [...players, ...r.players];
    } catch (e) {
      toastError(e);
    }
  }

  let searchTimer: ReturnType<typeof setTimeout>;
  $effect(() => {
    sort;
    asc;
    q;
    if (player) return;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => untrack(() => loadBoard()), q ? 250 : 0);
  });

  function sortBy(k: string) {
    if (sort === k) asc = !asc;
    else {
      sort = k;
      asc = k === 'name';
    }
  }

  async function refreshStats() {
    refreshing = true;
    try {
      const r = await api.post<{ updated: number }>(`${mcUrl}/stats/refresh`);
      toastOk(r.updated ? `Statistiken von ${r.updated} Spielern eingelesen` : 'Statistiken sind aktuell');
      loadBoard(true);
    } catch (e) {
      toastError(e);
    } finally {
      refreshing = false;
    }
  }

  const boardCols = ['playtime', 'deaths', 'mobKills', 'blocksMined', 'distance', 'advancements'].map((k) => statByKey[k]);

  // ---- events ----
  let evType = $state('');
  let events = $state<MCEvent[]>([]);
  let causeLabels = $state<Record<string, string>>({});
  let evDone = $state(false);
  const evTypes = [
    { id: '', label: 'Alle' },
    { id: 'death', label: 'Tode' },
    { id: 'advancement', label: 'Fortschritte' },
    { id: 'chat', label: 'Chat' },
    { id: 'join,leave', label: 'Beitritte' },
  ];

  async function loadEvents(append = false) {
    try {
      const before = append && events.length ? events[events.length - 1].id : undefined;
      const r = await api.get<{ events: MCEvent[]; causeLabels: Record<string, string> }>(`${mcUrl}/events${qs({ types: evType, before, limit: 50 })}`);
      causeLabels = r.causeLabels;
      events = append ? [...events, ...r.events.filter((e) => !events.some((x) => x.id === e.id))] : r.events;
      evDone = r.events.length < 50;
    } catch (e) {
      toastError(e);
    }
  }

  $effect(() => {
    evType;
    if (tab === 'events' && !player) untrack(() => loadEvents());
  });

  // ---- detail ----
  let detail = $state<PlayerDetail | null>(null);
  let detailError = $state('');

  async function loadDetail() {
    try {
      detail = await api.get<PlayerDetail>(`${mcUrl}/players/${player}`);
      detailError = '';
    } catch (e: any) {
      detailError = e.message;
    }
  }

  $effect(() => {
    if (player) {
      detail = null;
      untrack(() => loadDetail());
    }
  });

  onMount(() => {
    loadLists();
    const offs = [
      live.on('players', server.id, (e) => {
        online = e.d ?? [];
        if (!player && tab === 'board') loadBoard(true);
      }),
      live.on('mcevent', server.id, (e) => {
        const ev = e.d as MCEvent;
        if (tab === 'events' && !player) {
          const types = evType ? evType.split(',') : null;
          if (!types || types.includes(ev.type)) events = [ev, ...events];
        }
        if (player && ev.uuid === player && (ev.type === 'death' || ev.type === 'advancement' || ev.type === 'chat')) loadDetail();
      }),
    ];
    return () => offs.forEach((f) => f());
  });

  // ---- list management ----
  let addName = $state('');
  let addReason = $state('');
  let addIp = $state('');

  async function addTo(kind: 'whitelist_add' | 'op' | 'ban' | 'ban_ip') {
    if (kind === 'ban_ip') {
      if (!addIp.trim()) return;
      await action(kind, { ip: addIp.trim(), reason: addReason }, `${addIp.trim()} gesperrt`);
      addIp = '';
    } else {
      const name = addName.trim();
      if (!name) return;
      const msgs: Record<string, string> = { whitelist_add: `${name} zur Whitelist hinzugefügt`, op: `${name} ist jetzt Operator`, ban: `${name} gesperrt` };
      await action(kind, { name, reason: kind === 'ban' ? addReason : undefined }, msgs[kind]);
      addName = '';
    }
    addReason = '';
  }

  async function setWhitelist(on: boolean) {
    if (!on || (await confirmDialog({ title: 'Whitelist einschalten?', text: 'Danach können nur noch Spieler beitreten, die auf der Whitelist stehen.', confirm: 'Einschalten' }))) {
      action(on ? 'whitelist_on' : 'whitelist_off', {}, on ? 'Whitelist eingeschaltet' : 'Whitelist ausgeschaltet');
    } else loadLists();
  }

  let tabs = $derived([
    { id: 'board', label: 'Bestenliste', count: total },
    { id: 'events', label: 'Ereignisse' },
    { id: 'whitelist', label: 'Whitelist', count: lists.whitelist.length },
    { id: 'ops', label: 'Operatoren', count: lists.ops.length },
    { id: 'bans', label: 'Sperren', count: lists.bans.length + lists.ipbans.length },
  ]);

  let onlineSet = $derived(new Set(online.map((o) => o.uuid)));
</script>

{#if player}
  <div class="sec">
    <a class="back small" href="/servers/{server.id}/players"><ArrowLeft size={15} /> Alle Spieler</a>
    {#if detailError}
      <div class="panel"><div class="empty"><h3>Spieler nicht gefunden</h3><p>{detailError}</p></div></div>
    {:else if detail}
      <PlayerProfile {detail}>
        {#snippet actions()}
          {#if canManage && detail}
            {@const p = { name: detail.player.name, uuid: detail.player.uuid, online: detail.online }}
            {#if p.online}<button class="btn" onclick={() => kick(p)}><UserX /> Kicken</button>{/if}
            <Menu text="Verwalten" icon={Gavel} items={playerMenu(p).filter((i) => i.label !== 'Kicken')} />
          {/if}
        {/snippet}
      </PlayerProfile>
    {:else}
      <div class="panel loading"><span class="muted">Lade Profil …</span></div>
    {/if}
  </div>
{:else}
  <div class="sec">
    <div class="sec-head">
      <div class="titles">
        <h2>Spieler</h2>
        <p>Alle Spieler, die jemals auf dem Server waren, mit Spielzeit und Statistiken aus der Welt.</p>
      </div>
    </div>

    <Tabs items={tabs} value={tab} onchange={setTab} />

    {#if tab === 'board'}
      {#if totals}
        <div class="totals">
          <div><span class="v num">{num(totals.players)}</span><span class="l">Spieler</span></div>
          <div><span class="v num">{hours(totals.playSeconds)}</span><span class="l">Spielzeit gesamt</span></div>
          <div><span class="v num">{num(totals.deaths)}</span><span class="l">Tode</span></div>
          <div><span class="v num">{num(totals.mobKills)}</span><span class="l">Besiegte Kreaturen</span></div>
          <div><span class="v num">{num(totals.blocksMined)}</span><span class="l">Abgebaute Blöcke</span></div>
          <div><span class="v num">{distance(totals.distanceCm)}</span><span class="l">Zurückgelegt</span></div>
        </div>
      {/if}

      <div class="tools">
        <div class="search">
          <Search size={15} />
          <input class="input" placeholder="Spieler suchen" bind:value={q} aria-label="Spieler suchen" />
        </div>
        <select class="select sortsel" bind:value={sort} aria-label="Sortieren nach">
          {#each statCols as c (c.key)}<option value={c.key}>{c.label}</option>{/each}
          <option value="lastSeen">Zuletzt online</option>
          <option value="firstSeen">Erstmals gesehen</option>
          <option value="name">Name</option>
        </select>
        <span class="spacer"></span>
        <button class="btn" onclick={refreshStats} disabled={refreshing} title="Liest die Statistik-Dateien der Welt neu ein">
          <RefreshCw class={refreshing ? 'spin' : ''} /> Statistiken einlesen
        </button>
      </div>

      <div class="panel">
        {#if players.length === 0 && !boardLoading}
          <div class="empty">
            <Users />
            <h3>{q ? 'Kein Spieler gefunden' : 'Noch keine Spieler'}</h3>
            <p>{q ? `Niemand passt zu „${q}“.` : 'Sobald jemand den Server betritt, erscheint er hier mit Spielzeit und Statistiken.'}</p>
          </div>
        {:else}
          <div class="table-wrap">
            <table class="table board">
              <thead>
                <tr>
                  <th class="rank">#</th>
                  <th class="sortable" onclick={() => sortBy('name')}>Spieler</th>
                  {#each boardCols as c (c.key)}
                    <th class="sortable r" class:on={sort === c.key} onclick={() => sortBy(c.key)}>
                      {c.short ?? c.label}
                      {#if sort === c.key}{#if asc}<ArrowUp size={12} />{:else}<ArrowDown size={12} />{/if}{/if}
                    </th>
                  {/each}
                  <th class="sortable r" class:on={sort === 'lastSeen'} onclick={() => sortBy('lastSeen')}>Zuletzt</th>
                  {#if canManage}<th></th>{/if}
                </tr>
              </thead>
              <tbody>
                {#each players as p, i (p.uuid)}
                  {@const isOnline = onlineSet.has(p.uuid)}
                  <tr>
                    <td class="rank num">{i + 1}</td>
                    <td>
                      <a class="who" href="/servers/{server.id}/players/{p.uuid}">
                        <span class="hw" class:online={isOnline}><PlayerHead uuid={p.uuid} name={p.name} size={26} /></span>
                        <span class="ellipsis">{p.name}</span>
                        {#if p.op}<span title="Operator"><Crown size={13} class="crown" /></span>{/if}
                      </a>
                    </td>
                    {#each boardCols as c (c.key)}
                      <td class="r num" class:hl={sort === c.key}>{c.fmt(statValue(p, c))}</td>
                    {/each}
                    <td class="r nowrap" title={dateTime(p.lastSeen)}>
                      {#if isOnline}<span class="state running">online</span>{:else}<span class="muted small">{ago(p.lastSeen)}</span>{/if}
                    </td>
                    {#if canManage}<td class="r"><Menu items={playerMenu({ ...p, online: isOnline })} /></td>{/if}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          {#if players.length < total}
            <div class="morewrap"><button class="btn" onclick={more}>Weitere laden ({num(total - players.length)})</button></div>
          {/if}
        {/if}
      </div>
      <p class="tiny faint">Spielzeit, Tode und Werte stammen aus den Statistik-Dateien von Minecraft. Sie werden beim Speichern der Welt aktualisiert und regelmäßig eingelesen.</p>
    {:else if tab === 'events'}
      <div class="seg" role="group" aria-label="Ereignistyp">
        {#each evTypes as t (t.id)}
          <button class:on={evType === t.id} onclick={() => (evType = t.id)}>{t.label}</button>
        {/each}
      </div>
      <div class="panel">
        {#if events.length}
          <EventFeed {events} {causeLabels} playerHref={(u) => `/servers/${server.id}/players/${u}`} />
          {#if !evDone}<div class="morewrap"><button class="btn" onclick={() => loadEvents(true)}>Ältere laden</button></div>{/if}
        {:else}
          <div class="empty"><p>Noch keine Ereignisse dieser Art. {#if evType === 'chat' && !server.settings?.trackChat}Chat-Aufzeichnung ist unter „Start &amp; Version“ ausgeschaltet.{/if}</p></div>
        {/if}
      </div>
    {:else if tab === 'whitelist'}
      <div class="panel">
        <div class="panel-body wlhead">
          <Toggle
            checked={lists.whitelistEnabled}
            label="Whitelist aktiv"
            hint={lists.whitelistEnabled ? 'Nur eingetragene Spieler können beitreten.' : 'Jeder kann beitreten. Einträge werden erst wirksam, wenn die Whitelist aktiv ist.'}
            disabled={!canManage}
            onchange={setWhitelist}
          />
        </div>
        {#if canManage}
          <form class="addrow" onsubmit={(e) => (e.preventDefault(), addTo('whitelist_add'))}>
            <input class="input" bind:value={addName} placeholder="Spielername" aria-label="Spielername" />
            <button class="btn primary" disabled={!addName.trim()}><UserPlus /> Hinzufügen</button>
          </form>
        {/if}
        {@render entryList(lists.whitelist, (e) => action('whitelist_remove', { name: e.name, uuid: e.uuid }, `${e.name} entfernt`), 'Entfernen')}
      </div>
    {:else if tab === 'ops'}
      <div class="panel">
        <p class="panel-body small muted">Operatoren dürfen alle Befehle im Spiel nutzen. Die Rechtestufe steuert, was genau erlaubt ist (4 = alles).</p>
        {#if canManage}
          <form class="addrow" onsubmit={(e) => (e.preventDefault(), addTo('op'))}>
            <input class="input" bind:value={addName} placeholder="Spielername" aria-label="Spielername" />
            <button class="btn primary" disabled={!addName.trim()}><Crown /> Zum Operator machen</button>
          </form>
        {/if}
        {@render entryList(lists.ops, (e) => action('deop', { name: e.name, uuid: e.uuid }, `${e.name} ist kein Operator mehr`), 'Rechte entziehen')}
      </div>
    {:else if tab === 'bans'}
      <div class="panel">
        <div class="panel-head"><Ban size={16} /><h3>Gesperrte Spieler</h3></div>
        {#if canManage}
          <form class="addrow" onsubmit={(e) => (e.preventDefault(), addTo('ban'))}>
            <input class="input" bind:value={addName} placeholder="Spielername" aria-label="Spielername" />
            <input class="input" bind:value={addReason} placeholder="Grund (optional)" aria-label="Grund" />
            <button class="btn danger" disabled={!addName.trim()}><Ban /> Sperren</button>
          </form>
        {/if}
        {@render entryList(lists.bans, (e) => action('pardon', { name: e.name, uuid: e.uuid }, `Sperre von ${e.name} aufgehoben`), 'Entsperren')}
      </div>
      <div class="panel">
        <div class="panel-head"><Network size={16} /><h3>Gesperrte IP-Adressen</h3></div>
        {#if canManage}
          <form class="addrow" onsubmit={(e) => (e.preventDefault(), addTo('ban_ip'))}>
            <input class="input mono" bind:value={addIp} placeholder="IP-Adresse" aria-label="IP-Adresse" />
            <input class="input" bind:value={addReason} placeholder="Grund (optional)" aria-label="Grund" />
            <button class="btn danger" disabled={!addIp.trim()}><Ban /> Sperren</button>
          </form>
        {/if}
        {@render entryList(lists.ipbans, (e) => action('pardon_ip', { ip: e.ip }, `Sperre von ${e.ip} aufgehoben`), 'Entsperren', true)}
      </div>
    {/if}
  </div>
{/if}

{#snippet entryList(entries: ListEntry[], onremove: (e: ListEntry) => void, removeLabel: string, ip = false)}
  {#if entries.length === 0}
    <p class="panel-body small faint">Keine Einträge.</p>
  {:else}
    <ul class="entries">
      {#each entries as e, i (e.uuid ?? e.ip ?? i)}
        <li>
          {#if ip}
            <span class="mono">{e.ip}</span>
          {:else}
            <PlayerHead uuid={e.uuid ?? ''} name={e.name} size={24} />
            {#if e.uuid}<a href="/servers/{server.id}/players/{e.uuid}" class="ename">{e.name}</a>{:else}<span class="ename">{e.name}</span>{/if}
          {/if}
          {#if e.level}<span class="tag">Stufe {e.level}</span>{/if}
          {#if e.reason}<span class="small muted ellipsis reason" title={e.reason}>{e.reason}</span>{/if}
          <span class="spacer"></span>
          {#if e.created}<span class="tiny faint nowrap">{e.created.slice(0, 10)}</span>{/if}
          {#if e.expires && e.expires !== 'forever'}<span class="tiny faint nowrap">bis {e.expires.slice(0, 10)}</span>{/if}
          {#if canManage}<button class="btn sm" onclick={() => onremove(e)}>{removeLabel}</button>{/if}
        </li>
      {/each}
    </ul>
  {/if}
{/snippet}

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
    align-self: flex-start;
  }
  .loading {
    padding: 60px;
    text-align: center;
  }
  .totals {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    border: 1px solid var(--line);
    border-radius: var(--r-l);
    background: var(--surface);
    overflow: hidden;
  }
  .totals > div {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 12px 16px;
    box-shadow: 1px 0 0 var(--line);
  }
  .totals .v {
    font-size: 19px;
    font-weight: 680;
  }
  .totals .l {
    font-size: 12.5px;
    color: var(--text-3);
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .search {
    position: relative;
    width: 240px;
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
    min-width: 190px;
  }
  .board th.on {
    color: var(--text);
  }
  .board th :global(svg) {
    vertical-align: -1px;
  }
  .board td.hl {
    font-weight: 650;
  }
  .rank {
    width: 44px;
    color: var(--text-3);
  }
  .who {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text);
    font-weight: 600;
    min-width: 0;
  }
  .who:hover {
    color: var(--accent);
    text-decoration: none;
  }
  .hw {
    display: inline-flex;
    border-radius: 5px;
    outline: 2px solid transparent;
    outline-offset: 1px;
  }
  .hw.online {
    outline-color: var(--accent);
  }
  .who :global(.crown) {
    color: var(--warn);
  }
  .morewrap {
    display: flex;
    justify-content: center;
    padding: 12px;
    border-top: 1px solid var(--line);
  }
  .wlhead {
    border-bottom: 1px solid var(--line);
  }
  .addrow {
    display: flex;
    gap: 8px;
    padding: 14px 18px;
    border-bottom: 1px solid var(--line);
    flex-wrap: wrap;
  }
  .addrow .input {
    flex: 1;
    min-width: 160px;
  }
  .panel-head :global(svg) {
    color: var(--text-3);
  }
  .entries {
    list-style: none;
    margin: 0;
    padding: 4px 0;
  }
  .entries li {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 18px;
    border-bottom: 1px solid var(--line);
    min-width: 0;
  }
  .entries li:last-child {
    border-bottom: none;
  }
  .ename {
    font-weight: 600;
    color: var(--text);
  }
  .reason {
    max-width: 320px;
  }
  @media (max-width: 760px) {
    .search {
      width: 100%;
    }
  }
</style>
