<script lang="ts" module>
  export interface MenuItem {
    label: string;
    icon?: any;
    onclick?: () => void;
    href?: string;
    danger?: boolean;
    disabled?: boolean;
    divider?: boolean;
  }
</script>

<script lang="ts">
  import { EllipsisVertical, ChevronDown } from '@lucide/svelte';

  let {
    items,
    label = 'Aktionen',
    align = 'right',
    text = '',
    icon = null,
    cls = 'btn',
  }: { items: MenuItem[]; label?: string; align?: 'left' | 'right'; text?: string; icon?: any; cls?: string } = $props();
  let open = $state(false);
  let root: HTMLDivElement;
  let up = $state(false);

  function toggle(e: MouseEvent) {
    e.stopPropagation();
    open = !open;
    if (open) {
      const r = root.getBoundingClientRect();
      up = r.bottom + 260 > innerHeight && r.top > 260;
    }
  }

  function outside(e: MouseEvent) {
    if (open && !root.contains(e.target as Node)) open = false;
  }
</script>

<svelte:window onclick={outside} onkeydown={(e) => e.key === 'Escape' && (open = false)} />

<div class="menu" bind:this={root}>
  {#if text}
    {@const Icon = icon}
    <button class={cls} aria-haspopup="menu" aria-expanded={open} onclick={toggle}>{#if Icon}<Icon />{/if}{text}<ChevronDown class="chev" /></button>
  {:else}
    <button class="btn ghost sm icon" aria-label={label} aria-haspopup="menu" aria-expanded={open} onclick={toggle}><EllipsisVertical /></button>
  {/if}
  {#if open}
    <div class="items {align}" class:up role="menu">
      {#each items as it, i (i)}
        {#if it.divider}
          <hr class="sep" />
        {:else if it.href}
          <a role="menuitem" class="item" class:danger={it.danger} href={it.href} data-external download onclick={() => (open = false)}>
            {#if it.icon}<it.icon />{/if}{it.label}
          </a>
        {:else}
          <button
            role="menuitem"
            class="item"
            class:danger={it.danger}
            disabled={it.disabled}
            onclick={(e) => {
              e.stopPropagation();
              open = false;
              it.onclick?.();
            }}
          >
            {#if it.icon}<it.icon />{/if}{it.label}
          </button>
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .menu {
    position: relative;
    display: inline-flex;
  }
  .menu :global(.chev) {
    width: 14px !important;
    height: 14px !important;
    margin-right: -3px;
    color: var(--text-3);
  }
  .items {
    position: absolute;
    top: calc(100% + 4px);
    z-index: 60;
    min-width: 190px;
    padding: 5px;
    background: var(--surface);
    border: 1px solid var(--line-2);
    border-radius: var(--r-m);
    box-shadow: var(--pop-shadow);
    display: flex;
    flex-direction: column;
  }
  .items.up {
    top: auto;
    bottom: calc(100% + 4px);
  }
  .items.right {
    right: 0;
  }
  .items.left {
    left: 0;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 7px 9px;
    border: none;
    background: none;
    border-radius: 6px;
    color: var(--text);
    font: inherit;
    font-size: 14px;
    text-align: left;
    cursor: pointer;
    white-space: nowrap;
  }
  .item:hover:not(:disabled) {
    background: var(--surface-2);
    text-decoration: none;
  }
  .item:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .item :global(svg) {
    width: 16px;
    height: 16px;
    color: var(--text-2);
  }
  .item.danger,
  .item.danger :global(svg) {
    color: var(--danger);
  }
  hr {
    margin: 4px 2px;
  }
</style>
