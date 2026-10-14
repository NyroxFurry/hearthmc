<script lang="ts">
  // Time series chart with hover readout. Points: { t: unix seconds, v: value }.
  interface Point {
    t: number;
    v: number;
  }
  let {
    points,
    height = 160,
    color = 'var(--accent)',
    format = (v: number) => String(Math.round(v)),
    max = 0,
    label = '',
    step = false,
    integer = false,
  }: {
    points: Point[];
    height?: number;
    color?: string;
    format?: (v: number) => string;
    max?: number;
    label?: string;
    step?: boolean;
    /** whole-number values (e.g. player counts): ticks land on integers */
    integer?: boolean;
  } = $props();

  let width = $state(600);
  let hover = $state<number | null>(null);
  const padL = 44,
    padB = 22,
    padT = 10,
    padR = 8;

  let geo = $derived.by(() => {
    if (points.length === 0) return null;
    const t0 = points[0].t,
      t1 = points[points.length - 1].t || t0 + 1;
    const raw = Math.max(max, ...points.map((p) => p.v), 1e-9);
    const top = integer ? Math.max(2, Math.ceil(raw / 2) * 2) : raw;
    const vmax = top * 1.08;
    const w = Math.max(10, width - padL - padR),
      h = height - padT - padB;
    const x = (t: number) => padL + ((t - t0) / Math.max(1, t1 - t0)) * w;
    const y = (v: number) => padT + h - (Math.max(0, v) / vmax) * h;
    let line = '';
    points.forEach((p, i) => {
      if (i === 0) line += `M${x(p.t)},${y(p.v)}`;
      else if (step) line += `H${x(p.t)}V${y(p.v)}`;
      else line += `L${x(p.t)},${y(p.v)}`;
    });
    const area = `${line}L${x(t1)},${padT + h}L${x(t0)},${padT + h}Z`;
    const ticks = [0, 0.5, 1].map((f) => ({ y: y(top * f), v: top * f }));
    const span = t1 - t0;
    const xt = [0, 0.25, 0.5, 0.75, 1].map((f) => {
      const t = t0 + f * span;
      const d = new Date(t * 1000);
      const lbl =
        span > 2 * 86400
          ? d.toLocaleDateString('de-DE', { day: '2-digit', month: '2-digit' })
          : d.toLocaleTimeString('de-DE', { hour: '2-digit', minute: '2-digit' });
      return { x: x(t), label: lbl };
    });
    return { line, area, ticks, xt, x, y, w, h };
  });

  function move(e: PointerEvent) {
    if (!geo || points.length === 0) return;
    const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
    const px = e.clientX - rect.left;
    let best = 0,
      bd = Infinity;
    points.forEach((p, i) => {
      const d = Math.abs(geo!.x(p.t) - px);
      if (d < bd) {
        bd = d;
        best = i;
      }
    });
    hover = best;
  }
</script>

<div class="chart" bind:clientWidth={width}>
  {#if geo}
    <svg {width} {height} role="img" aria-label={label} onpointermove={move} onpointerleave={() => (hover = null)}>
      {#each geo.ticks as t (t.y)}
        <line x1={padL} x2={width - padR} y1={t.y} y2={t.y} class="grid" />
        <text x={padL - 8} y={t.y + 4} class="axis" text-anchor="end">{format(t.v)}</text>
      {/each}
      {#each geo.xt as t (t.x)}
        <text x={t.x} y={height - 5} class="axis" text-anchor="middle">{t.label}</text>
      {/each}
      <path d={geo.area} fill={color} opacity="0.14" />
      <path d={geo.line} fill="none" stroke={color} stroke-width="1.8" stroke-linejoin="round" />
      {#if hover !== null}
        {@const p = points[hover]}
        <line x1={geo.x(p.t)} x2={geo.x(p.t)} y1={padT} y2={height - padB} class="cursor" />
        <circle cx={geo.x(p.t)} cy={geo.y(p.v)} r="3.5" fill={color} />
      {/if}
    </svg>
    {#if hover !== null}
      {@const p = points[hover]}
      <div class="tip" style="left:{Math.min(width - 150, Math.max(0, geo.x(p.t) - 60))}px">
        <strong>{format(p.v)}</strong>
        <span>{new Date(p.t * 1000).toLocaleString('de-DE', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })}</span>
      </div>
    {/if}
  {:else}
    <div class="nodata" style="height:{height}px">Noch keine Daten</div>
  {/if}
</div>

<style>
  .chart {
    position: relative;
    width: 100%;
  }
  svg {
    display: block;
    overflow: visible;
  }
  .grid {
    stroke: var(--line);
    stroke-dasharray: 2 4;
  }
  .axis {
    fill: var(--text-3);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .cursor {
    stroke: var(--text-3);
    stroke-width: 1;
  }
  .tip {
    position: absolute;
    top: 0;
    display: flex;
    flex-direction: column;
    padding: 5px 9px;
    border-radius: 6px;
    background: var(--surface-3);
    border: 1px solid var(--line-2);
    font-size: 12px;
    pointer-events: none;
    white-space: nowrap;
  }
  .tip span {
    color: var(--text-3);
  }
  .nodata {
    display: grid;
    place-items: center;
    color: var(--text-3);
    font-size: 13px;
    border: 1px dashed var(--line);
    border-radius: var(--r-m);
  }
</style>
