<script lang="ts">
  import { X } from '@lucide/svelte';
  import type { Snippet } from 'svelte';

  interface Props {
    open: boolean;
    title: string;
    size?: 's' | 'm' | 'l' | 'xl';
    onclose?: () => void;
    children: Snippet;
    footer?: Snippet;
  }
  let { open = $bindable(), title, size = 'm', onclose, children, footer }: Props = $props();
  let dialog: HTMLDivElement | undefined = $state();

  function close() {
    open = false;
    onclose?.();
  }

  function onkey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) {
      e.stopPropagation();
      close();
    }
  }

  $effect(() => {
    if (open && dialog) {
      const el = dialog.querySelector<HTMLElement>('input:not([type=checkbox]),textarea,select,[data-autofocus]');
      (el ?? dialog).focus();
    }
  });
</script>

<svelte:window onkeydown={onkey} />

{#if open}
  <div class="overlay" role="presentation" onmousedown={(e) => e.target === e.currentTarget && close()}>
    <div class="dialog {size}" role="dialog" aria-modal="true" aria-label={title} tabindex="-1" bind:this={dialog}>
      <header>
        <h3>{title}</h3>
        <button class="btn ghost sm icon" onclick={close} aria-label="Schließen"><X /></button>
      </header>
      <div class="body">{@render children()}</div>
      {#if footer}
        <footer>{@render footer()}</footer>
      {/if}
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: var(--overlay);
    display: grid;
    place-items: center;
    z-index: 100;
    padding: 20px;
    animation: fade 0.12s ease-out;
  }
  .dialog {
    width: 100%;
    max-height: calc(100vh - 40px);
    display: flex;
    flex-direction: column;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--r-l);
    box-shadow: var(--pop-shadow);
    outline: none;
    animation: rise 0.16s var(--ease);
  }
  .s {
    max-width: 420px;
  }
  .m {
    max-width: 560px;
  }
  .l {
    max-width: 780px;
  }
  .xl {
    max-width: 1100px;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 15px 16px 13px 20px;
    border-bottom: 1px solid var(--line);
  }
  .body {
    padding: 18px 20px;
    overflow: auto;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: 9px;
    padding: 13px 20px;
    border-top: 1px solid var(--line);
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.99);
    }
  }
</style>
