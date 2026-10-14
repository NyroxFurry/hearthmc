<script lang="ts">
  let {
    values,
    max = 0,
    width = 96,
    height = 28,
    color = 'var(--accent)',
  }: { values: number[]; max?: number; width?: number; height?: number; color?: string } = $props();

  let path = $derived.by(() => {
    if (values.length < 2) return { line: '', area: '' };
    const m = Math.max(max, ...values, 1e-9);
    const step = width / (values.length - 1);
    const pts = values.map((v, i) => [i * step, height - 2 - (Math.max(0, v) / m) * (height - 4)]);
    const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join('');
    return { line, area: `${line}L${width},${height}L0,${height}Z` };
  });
</script>

<svg {width} {height} viewBox="0 0 {width} {height}" aria-hidden="true">
  {#if path.line}
    <path d={path.area} fill={color} opacity="0.13" />
    <path d={path.line} fill="none" stroke={color} stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round" />
  {/if}
</svg>
