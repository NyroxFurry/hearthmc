<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { ScrollText } from '@lucide/svelte';
  import AuditList from '../../components/AuditList.svelte';
  import { api, qs } from '../../lib/api';
  import { toastError } from '../../lib/toast.svelte';
  import type { AuditEntry, Server } from '../../lib/types';

  let { server }: { server: Server } = $props();

  let entries = $state<AuditEntry[]>([]);
  let loaded = $state(false);
  let done = $state(false);
  let action = $state('');

  const filters = [
    { id: '', label: 'Alles' },
    { id: 'server.', label: 'Server' },
    { id: 'files.', label: 'Dateien' },
    { id: 'backup.', label: 'Backups' },
    { id: 'mc.', label: 'Minecraft' },
    { id: 'member.', label: 'Zugriff' },
    { id: 'schedule.', label: 'Zeitpläne' },
  ];

  async function load(more = false) {
    try {
      const before = more && entries.length ? entries[entries.length - 1].id : undefined;
      const list = await api.get<AuditEntry[]>(`/api/servers/${server.id}/activity${qs({ limit: 100, before, action })}`);
      entries = more ? [...entries, ...list.filter((e) => !entries.some((x) => x.id === e.id))] : list;
      done = list.length < 100;
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }

  $effect(() => {
    action;
    untrack(() => load());
  });
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Aktivität</h2>
      <p>Wer wann was an diesem Server geändert hat: Starts, Befehle, Dateien, Backups und Rechte.</p>
    </div>
    <div class="seg" role="group" aria-label="Filter">
      {#each filters as f (f.id)}
        <button class:on={action === f.id} onclick={() => (action = f.id)}>{f.label}</button>
      {/each}
    </div>
  </div>

  <div class="panel">
    {#if loaded && entries.length === 0}
      <div class="empty"><ScrollText /><p>Noch keine Einträge.</p></div>
    {:else}
      <AuditList {entries} />
      {#if !done}
        <div class="more"><button class="btn" onclick={() => load(true)}>Ältere laden</button></div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .more {
    display: flex;
    justify-content: center;
    padding: 12px;
    border-top: 1px solid var(--line);
  }
</style>
