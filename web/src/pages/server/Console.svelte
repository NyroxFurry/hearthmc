<script lang="ts">
  import { live } from '../../lib/live.svelte';
  import { api } from '../../lib/api';
  import { can } from '../../lib/session.svelte';
  import { toastError } from '../../lib/toast.svelte';
  import type { Server, Status } from '../../lib/types';

  let { server, status }: { server: Server; status: Status } = $props();

  const active = $derived(status.state !== 'offline' && status.state !== 'installing');
  const canSend = $derived(can(server, 'console.command'));
  const flavor = $derived(server.settings?.flavor ?? '');

  let quick = $derived.by(() => {
    if (server.game !== 'minecraft') return [];
    const q = [
      { cmd: 'list', label: 'Spielerliste' },
      { cmd: 'save-all', label: 'Welt speichern' },
    ];
    if (flavor === 'paper' || flavor === 'purpur') q.push({ cmd: 'tps', label: 'TPS' }, { cmd: 'plugins', label: 'Plugins' });
    if (flavor === 'forge') q.push({ cmd: 'forge tps', label: 'TPS' });
    if (flavor === 'neoforge') q.push({ cmd: 'neoforge tps', label: 'TPS' });
    return q;
  });

  async function run(cmd: string) {
    if (live.connected) live.command(server.id, cmd);
    else {
      try {
        await api.post(`/api/servers/${server.id}/command`, { command: cmd });
      } catch (e) {
        toastError(e);
      }
    }
  }
</script>

<div class="wrap">
  {#if canSend && quick.length}
    <div class="quick">
      <span class="small faint">Schnellbefehle</span>
      {#each quick as q (q.cmd)}
        <button class="btn sm" disabled={!active} onclick={() => run(q.cmd)} title={q.cmd}>{q.label}</button>
      {/each}
    </div>
  {/if}
  <div class="term">
    {#await import('../../components/Terminal.svelte') then { default: Terminal }}
      <Terminal serverId={server.id} {canSend} running={active} fill />
    {/await}
  </div>
</div>

<style>
  .wrap {
    display: flex;
    flex-direction: column;
    gap: 10px;
    height: calc(100vh - var(--head-h, 93px) - 52px);
    min-height: 420px;
  }
  .quick {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .quick .small {
    margin-right: 4px;
  }
  .term {
    flex: 1;
    min-height: 0;
  }
  @media (max-width: 1000px) {
    .wrap {
      height: calc(100vh - var(--head-h, 93px) - 96px);
    }
  }
</style>
