<script lang="ts">
  import { CircleCheck, CircleAlert, Info, X } from '@lucide/svelte';
  import { toasts } from '../lib/toast.svelte';
</script>

<div class="toasts" aria-live="polite">
  {#each toasts.list as t (t.id)}
    <div class="toast {t.kind}">
      {#if t.kind === 'ok'}<CircleCheck />{:else if t.kind === 'error'}<CircleAlert />{:else}<Info />{/if}
      <span>{t.text}</span>
      <button class="btn ghost sm icon" onclick={() => toasts.dismiss(t.id)} aria-label="Schließen"><X /></button>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    right: 18px;
    bottom: 18px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    z-index: 200;
    max-width: min(420px, calc(100vw - 36px));
  }
  .toast {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 8px 9px 13px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-left: 3px solid var(--accent);
    border-radius: var(--r-m);
    box-shadow: var(--pop-shadow);
    font-size: 14px;
    animation: slide 0.18s var(--ease);
  }
  .toast span {
    flex: 1;
  }
  .toast :global(svg) {
    width: 17px;
    height: 17px;
    flex: none;
  }
  .toast.ok > :global(svg:first-child) {
    color: var(--accent);
  }
  .toast.error {
    border-left-color: var(--danger);
  }
  .toast.error > :global(svg:first-child) {
    color: var(--danger);
  }
  .toast.info {
    border-left-color: var(--info);
  }
  .toast.info > :global(svg:first-child) {
    color: var(--info);
  }
  @keyframes slide {
    from {
      opacity: 0;
      transform: translateX(12px);
    }
  }
</style>
