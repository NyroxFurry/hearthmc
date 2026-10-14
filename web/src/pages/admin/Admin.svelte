<script lang="ts">
  import { Users, Ticket, Settings, Cpu, ScrollText, Bell } from '@lucide/svelte';
  import { session } from '../../lib/session.svelte';
  import AdminUsers from './Users.svelte';
  import AdminInvites from './Invites.svelte';
  import AdminSettings from './Settings.svelte';
  import AdminSystem from './System.svelte';
  import AdminAudit from './Audit.svelte';
  import WebhookManager from '../../components/WebhookManager.svelte';
  import PageBand from '../../components/PageBand.svelte';

  let { tab }: { tab: string } = $props();

  const tabs = [
    { id: 'users', label: 'Benutzer', icon: Users },
    { id: 'invites', label: 'Einladungen', icon: Ticket },
    { id: 'settings', label: 'Einstellungen', icon: Settings },
    { id: 'system', label: 'System & Java', icon: Cpu },
    { id: 'audit', label: 'Protokoll', icon: ScrollText },
    { id: 'webhooks', label: 'Benachrichtigungen', icon: Bell },
  ];
</script>

{#if !session.user?.isAdmin}
  <div class="page"><div class="empty"><h3>Kein Zugriff</h3><p>Dieser Bereich ist Administratoren vorbehalten.</p></div></div>
{:else}
  <PageBand title="Administration" kicker={session.panel.name || 'Hearth'}>
    {#snippet sub()}Benutzer, Einstellungen und Zustand des Panels.{/snippet}
    <nav class="tabs" aria-label="Bereiche der Administration">
      {#each tabs as t (t.id)}
        <a href="/admin/{t.id}" class:active={tab === t.id} aria-current={tab === t.id ? 'page' : undefined}><t.icon size={16} /> {t.label}</a>
      {/each}
    </nav>
  </PageBand>
  <div class="page">
    <div class="body">
      {#if tab === 'users'}
        <AdminUsers />
      {:else if tab === 'invites'}
        <AdminInvites />
      {:else if tab === 'settings'}
        <AdminSettings />
      {:else if tab === 'system'}
        <AdminSystem />
      {:else if tab === 'audit'}
        <AdminAudit />
      {:else if tab === 'webhooks'}
        <WebhookManager base="/api/admin/webhooks" intro="Diese Benachrichtigungen gelten für alle Server des Panels, zusätzlich zu denen, die einzelne Server selbst einrichten." />
      {:else}
        <div class="empty"><h3>Bereich nicht gefunden</h3></div>
      {/if}
    </div>
  </div>
{/if}

<style>
  /* Reiter im Band */
  .tabs {
    display: flex;
    gap: clamp(6px, 2vw, 22px);
    margin: 22px 0 -30px;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tabs a {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 46px;
    padding: 0 4px;
    color: var(--band-ink) !important;
    text-decoration: none !important;
    font-size: 14.5px;
    font-weight: 700;
    white-space: nowrap;
    opacity: 0.78;
    transition: opacity 0.2s;
  }
  .tabs a::after {
    content: '';
    position: absolute;
    left: 4px;
    right: 4px;
    bottom: 8px;
    height: 2px;
    background: var(--flavor);
    transform: scaleX(0);
    transform-origin: right;
    transition: transform 0.35s var(--ease);
  }
  .tabs a:hover,
  .tabs a.active {
    opacity: 1;
  }
  .tabs a:hover::after,
  .tabs a.active::after {
    transform: scaleX(1);
    transform-origin: left;
  }
  @media (max-width: 860px) {
    .tabs {
      margin-bottom: -22px;
    }
  }
</style>
