<script lang="ts">
  import { onMount } from 'svelte';
  import L from 'leaflet';
  import 'leaflet/dist/leaflet.css';
  import type { MapInfo, Position } from '../lib/types';
  import { api } from '../lib/api';

  let {
    info,
    tileBase,
    playersUrl = null,
    height = '520px',
    rounded = true,
    focus = null,
    onplayers,
  }: {
    info: MapInfo;
    tileBase: string;
    playersUrl?: string | null;
    height?: string;
    rounded?: boolean;
    focus?: { x: number; z: number } | null;
    onplayers?: (p: Position[]) => void;
  } = $props();

  let host: HTMLDivElement;
  let map: L.Map;
  let layer: L.TileLayer | null = null;
  let markers = new Map<string, L.Marker>();
  let dim = $state('');
  let cursor = $state<{ x: number; z: number } | null>(null);
  let players = $state<Position[]>([]);

  const dims = $derived(info.dimensions);
  const current = $derived(dims.find((d) => d.key === dim) ?? dims[0]);

  const dimIds: Record<string, string> = { overworld: 'minecraft:overworld', the_nether: 'minecraft:the_nether', the_end: 'minecraft:the_end' };

  function toLatLng(x: number, z: number) {
    return L.latLng(-z, x);
  }

  function setLayer() {
    if (!map || !current) return;
    layer?.remove();
    const i = current.info;
    const bounds = L.latLngBounds(toLatLng(i.minX * 512, (i.maxZ + 1) * 512), toLatLng((i.maxX + 1) * 512, i.minZ * 512));
    layer = L.tileLayer(`${tileBase}/${current.key}/{z}/{x}/{y}.png?v=${i.lastRender}`, {
      tileSize: 512,
      minZoom: i.minZoom,
      minNativeZoom: i.minZoom,
      maxNativeZoom: 0,
      maxZoom: 4,
      noWrap: true,
      bounds: bounds.pad(0.05),
      keepBuffer: 4,
    });
    layer.addTo(map);
    map.setMinZoom(i.minZoom);
    map.setMaxBounds(bounds.pad(0.6));
    updateMarkers();
  }

  function headIcon(p: Position) {
    return L.divIcon({
      className: 'player-marker',
      html: `<img src="/api/heads/${p.uuid}?size=32" alt=""><span>${p.name.replace(/[<>&"]/g, '')}</span>`,
      iconSize: [28, 28],
      iconAnchor: [14, 14],
    });
  }

  function updateMarkers() {
    if (!map) return;
    const want = dimIds[current?.key ?? ''] ?? current?.id;
    const seen = new Set<string>();
    for (const p of players) {
      if (p.dim !== want) continue;
      seen.add(p.uuid);
      let m = markers.get(p.uuid);
      if (!m) {
        m = L.marker(toLatLng(p.x, p.z), { icon: headIcon(p), title: p.name, keyboard: false }).addTo(map);
        markers.set(p.uuid, m);
      } else {
        m.setLatLng(toLatLng(p.x, p.z));
      }
    }
    for (const [id, m] of markers) {
      if (!seen.has(id)) {
        m.remove();
        markers.delete(id);
      }
    }
  }

  async function poll() {
    if (!playersUrl) return;
    try {
      players = await api.get<Position[]>(playersUrl);
      updateMarkers();
      onplayers?.(players);
    } catch {}
  }

  onMount(() => {
    dim = dims[0]?.key ?? '';
    map = L.map(host, {
      crs: L.CRS.Simple,
      attributionControl: false,
      zoomControl: true,
      zoomSnap: 1,
      center: toLatLng(focus?.x ?? info.spawn.x, focus?.z ?? info.spawn.z),
      zoom: -1,
    });
    map.on('mousemove', (e: L.LeafletMouseEvent) => (cursor = { x: Math.floor(e.latlng.lng), z: Math.floor(-e.latlng.lat) }));
    map.on('mouseout', () => (cursor = null));
    setLayer();
    poll();
    const t = setInterval(poll, 4000);
    const ro = new ResizeObserver(() => map.invalidateSize());
    ro.observe(host);
    return () => {
      clearInterval(t);
      ro.disconnect();
      map.remove();
    };
  });

  $effect(() => {
    dim;
    info;
    setLayer();
  });

  export function flyTo(x: number, z: number, d?: string) {
    if (d) {
      const key = Object.entries(dimIds).find(([, id]) => id === d)?.[0];
      if (key && dims.some((x) => x.key === key)) dim = key;
    }
    map?.flyTo(toLatLng(x, z), 1, { duration: 0.6 });
  }
</script>

<div class="map" class:rounded style="height:{height}">
  <div class="leaflet-host" bind:this={host}></div>
  {#if dims.length > 1}
    <div class="dims">
      {#each dims as d (d.key)}
        <button class:active={d.key === (current?.key ?? '')} onclick={() => (dim = d.key)}>{d.label}</button>
      {/each}
    </div>
  {/if}
  {#if cursor}
    <div class="coords num">X {cursor.x} &nbsp; Z {cursor.z}</div>
  {/if}
</div>

<style>
  .map {
    position: relative;
    overflow: hidden;
    background: #0d1113;
    isolation: isolate;
  }
  .map.rounded {
    border-radius: var(--r-l);
    border: 1px solid var(--line);
  }
  .leaflet-host {
    position: absolute;
    inset: 0;
    background: #0d1113;
  }
  .map :global(.leaflet-tile) {
    image-rendering: pixelated;
  }
  .map :global(.leaflet-container) {
    background: #0d1113;
    font-family: var(--font);
  }
  .map :global(.leaflet-bar) {
    border: none;
    box-shadow: none;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .map :global(.leaflet-control-zoom a) {
    width: 36px;
    height: 36px;
    line-height: 34px;
    border-radius: 50% !important;
    background: rgba(13, 17, 19, 0.82);
    color: #fff;
    border: 1.5px solid rgba(255, 255, 255, 0.4);
    font: 600 18px/34px var(--font);
  }
  .map :global(.leaflet-control-zoom a:hover) {
    background: rgba(255, 255, 255, 0.16);
  }
  .map :global(.leaflet-control-zoom a.leaflet-disabled) {
    opacity: 0.4;
  }
  .map :global(.player-marker) {
    background: none;
    border: none;
  }
  .map :global(.player-marker img) {
    width: 28px;
    height: 28px;
    image-rendering: pixelated;
    border-radius: 6px;
    border: 2px solid #fff;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.5);
  }
  .map :global(.player-marker span) {
    position: absolute;
    top: 33px;
    left: 50%;
    transform: translateX(-50%);
    padding: 2px 8px;
    border-radius: 999px;
    background: rgba(13, 17, 19, 0.85);
    color: #fff;
    font-size: 11.5px;
    font-weight: 700;
    white-space: nowrap;
  }
  .dims {
    position: absolute;
    top: 10px;
    right: 10px;
    z-index: 500;
    display: flex;
    gap: 2px;
    padding: 3px;
    border-radius: 999px;
    background: rgba(13, 17, 19, 0.82);
    border: 1px solid rgba(255, 255, 255, 0.12);
  }
  .dims button {
    border: none;
    background: none;
    color: rgba(255, 255, 255, 0.75);
    font: inherit;
    font-size: 13px;
    font-weight: 700;
    padding: 5px 12px;
    border-radius: 999px;
    cursor: pointer;
  }
  .dims button:hover {
    color: #fff;
  }
  .dims button.active {
    background: var(--flavor);
    color: var(--on-flavor);
  }
  .coords {
    position: absolute;
    left: 10px;
    bottom: 10px;
    z-index: 500;
    padding: 3px 10px;
    border-radius: 999px;
    background: rgba(13, 17, 19, 0.85);
    color: #e3e8ea;
    font-size: 12px;
    font-family: var(--font-mono);
    pointer-events: none;
  }
</style>
