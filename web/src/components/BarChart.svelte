<script lang="ts">
  // Small column chart, e.g. playtime per day. Hover shows the value of a column.
  interface Bar {
    label: string;
    value: number;
    title?: string;
  }
  let {
    bars,
    height = 120,
    color = 'var(--accent)',
    format = (v: number) => String(Math.round(v)),
    every = 7,
  }: { bars: Bar[]; height?: number; color?: string; format?: (v: number) => string; every?: number } = $props();

  let hover = $state<number | null>(null);
  let max = $derived(Math.max(1e-9, ...bars.map((b) => b.value)));
</script>

<div class="bars" style="--h:{height}px; --c:{color}" onmouseleave={() => (hover = null)} role="img" aria-label="Säulendiagramm">
  <div class="plot">
    {#each bars as b, i (i)}
      <button
        class="col"
        class:on={hover === i}
        onmouseenter={() => (hover = i)}
        onfocus={() => (hover = i)}
        aria-label="{b.title ?? b.label}: {format(b.value)}"
      >
        <span class="fill" style="height:{b.value > 0 ? Math.max(3, (b.value / max) * 100) : 0}%"></span>
      </button>
    {/each}
  </div>
  <div class="axis tiny faint">
    {#each bars as b, i (i)}
      <span class:last={i === bars.length - 1}><em>{(i % every === 0 && bars.length - 1 - i >= every / 2) || i === bars.length - 1 ? b.label : ''}</em></span>
    {/each}
  </div>
  <div class="readout small">
    {#if hover !== null && bars[hover]}
      <span class="faint">{bars[hover].title ?? bars[hover].label}</span>
      <strong class="num">{format(bars[hover].value)}</strong>
    {:else}
      &nbsp;
    {/if}
  </div>
</div>

<style>
  .plot {
    height: var(--h);
    display: flex;
    align-items: flex-end;
    gap: 3px;
    border-bottom: 1px solid var(--line);
  }
  .col {
    flex: 1;
    height: 100%;
    display: flex;
    align-items: flex-end;
    padding: 0;
    border: none;
    background: none;
    cursor: default;
    border-radius: 3px 3px 0 0;
  }
  .col:hover,
  .col.on {
    background: color-mix(in srgb, var(--surface-2) 70%, transparent);
  }
  .fill {
    display: block;
    width: 100%;
    background: var(--c);
    border-radius: 3px 3px 0 0;
    opacity: 0.85;
  }
  .col.on .fill {
    opacity: 1;
  }
  .axis {
    display: flex;
    gap: 3px;
    margin-top: 5px;
  }
  .axis span {
    flex: 1;
    white-space: nowrap;
    overflow: visible;
    min-width: 0;
  }
  .axis em {
    font-style: normal;
  }
  .axis span.last {
    position: relative;
  }
  .axis span.last em {
    position: absolute;
    right: 0;
  }
  .readout {
    display: flex;
    gap: 8px;
    margin-top: 4px;
    min-height: 20px;
  }
</style>
