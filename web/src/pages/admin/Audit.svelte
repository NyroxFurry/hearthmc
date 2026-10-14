<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { ScrollText } from '@lucide/svelte';
  import AuditList from '../../components/AuditList.svelte';
  import { api, qs } from '../../lib/api';
  import { servers } from '../../lib/session.svelte';
  import { toastError } from '../../lib/toast.svelte';
  import type { AuditEntry, User } from '../../lib/types';

  let entries = $state<AuditEntry[]>([]);
  let users = $state<User[]>([]);
  let loaded = $state(false);
  let done = $state(false);
  let server = $state('');
  let user = $state(0);
  let action = $state('');

  let names = $derived(Object.fromEntries(servers.list.map((s) => [s.id, s.name])));

  const filters = [
    { id: '', label: 'Alles' },
    { id: 'auth.', label: 'Anmeldungen' },
    { id: 'user.', label: 'Benutzer' },
    { id: 'server.', label: 'Server' },
    { id: 'files.', label: 'Dateien' },
    { id: 'backup.', label: 'Backups' },
    { id: 'settings.', label: 'Panel' },
  ];

  async function load(more = false) {
    try {
      const before = more && entries.length ? entries[entries.length - 1].id : undefined;
      const list = await api.get<AuditEntry[]>(`/api/admin/audit${qs({ server, user: user || undefined, action, before, limit: 100 })}`);
      entries = more ? [...entries, ...list] : list;
      done = list.length < 100;
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }

  $effect(() => {
    server;
    user;
    action;
    untrack(() => load());
  });

  onMount(async () => {
    try {
      users = await api.get<User[]>('/api/admin/users');
    } catch {}
  });
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Protokoll</h2>
      <p>Alle Anmeldungen und Änderungen im Panel. Einträge werden nach der eingestellten Aufbewahrungsdauer gelöscht.</p>
    </div>
  </div>
  <div class="filters">
    <div class="seg" role="group" aria-label="Art">
      {#each filters as f (f.id)}
        <button class:on={action === f.id} onclick={() => (action = f.id)}>{f.label}</button>
      {/each}
    </div>
    <span class="spacer"></span>
    <select class="select" bind:value={server} aria-label="Server">
      <option value="">Alle Server</option>
      {#each servers.list as s (s.id)}<option value={s.id}>{s.name}</option>{/each}
    </select>
    <select class="select" bind:value={user} aria-label="Benutzer">
      <option value={0}>Alle Benutzer</option>
      {#each users as u (u.id)}<option value={u.id}>{u.username}</option>{/each}
    </select>
  </div>
  <div class="panel">
    {#if loaded && entries.length === 0}
      <div class="empty"><ScrollText /><p>Keine Einträge für diese Auswahl.</p></div>
    {:else}
      <AuditList {entries} serverNames={names} />
      {#if !done}
        <div class="more"><button class="btn" onclick={() => load(true)}>Ältere laden</button></div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .filters {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .filters .select {
    width: auto;
    min-width: 160px;
  }
  .more {
    display: flex;
    justify-content: center;
    padding: 12px;
    border-top: 1px solid var(--line);
  }
</style>
