<script lang="ts">
  import WebhookManager from '../../components/WebhookManager.svelte';
  import type { Server } from '../../lib/types';

  let { server }: { server: Server } = $props();
</script>

<WebhookManager
  base="/api/servers/{server.id}/webhooks"
  intro="Schicke Ereignisse dieses Servers an Discord oder einen eigenen Dienst: Starts und Abstürze, Backups, Beitritte, Tode oder Fortschritte."
/>
