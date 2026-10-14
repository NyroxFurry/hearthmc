<script lang="ts">
  import type { Status } from '../lib/types';

  let { status, crashed = false }: { status: Status | undefined; crashed?: boolean } = $props();

  const labels: Record<string, string> = {
    running: 'Läuft',
    starting: 'Startet',
    stopping: 'Stoppt',
    installing: 'Installiert',
    offline: 'Offline',
  };

  let recentCrash = $derived(
    !!status && status.state === 'offline' && status.lastCrashAt > 0 && Date.now() / 1000 - status.lastCrashAt < 3600,
  );
  let cls = $derived(recentCrash || crashed ? 'crashed' : (status?.state ?? 'offline'));
  let label = $derived(recentCrash ? 'Abgestürzt' : labels[status?.state ?? 'offline']);
</script>

<span class="state {cls}">{label}{#if status?.busy === 'restore'}<span class="faint">&nbsp;(Wiederherstellung)</span>{/if}</span>
