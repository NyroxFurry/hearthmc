<script lang="ts">
  let {
    items,
    value = $bindable(),
    onchange,
  }: { items: { id: string; label: string; count?: number }[]; value: string; onchange?: (id: string) => void } = $props();
</script>

<div class="tabs" role="tablist">
  {#each items as it (it.id)}
    <button
      role="tab"
      aria-selected={value === it.id}
      class:active={value === it.id}
      onclick={() => {
        value = it.id;
        onchange?.(it.id);
      }}
    >
      {it.label}
      {#if it.count !== undefined}<span class="count">{it.count}</span>{/if}
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    gap: clamp(4px, 1.6vw, 18px);
    border-bottom: 1px solid var(--line);
    overflow-x: auto;
    scrollbar-width: none;
  }
  button {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 10px 4px 12px;
    background: none;
    border: none;
    color: var(--text-2);
    font: inherit;
    font-size: 14.5px;
    font-weight: 700;
    cursor: pointer;
    white-space: nowrap;
    transition: color 0.2s;
  }
  button:hover,
  button.active {
    color: var(--text);
  }
  /* Unterstrich in der Sortenfarbe, gleitet wie auf der Webseite ein */
  button::after {
    content: '';
    position: absolute;
    left: 4px;
    right: 4px;
    bottom: -1px;
    height: 2px;
    background: var(--flavor);
    transform: scaleX(0);
    transform-origin: right;
    transition: transform 0.35s var(--ease);
  }
  button:hover::after,
  button.active::after {
    transform: scaleX(1);
    transform-origin: left;
  }
  button:not(.active):hover::after {
    opacity: 0.5;
  }
  .count {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-3);
    font-variant-numeric: tabular-nums;
  }
</style>
