<script lang="ts">
  // Auswahl einer Sorte: tiefer Grund mit leuchtendem Kern, wie Band und Lichthof.
  import { Check } from '@lucide/svelte';
  import { flavors, flavorFor } from '../lib/flavors';

  let {
    value = $bindable(''),
    disabled = false,
    autoLabel = 'Automatisch',
    seed = '',
    autoColor = '',
    custom = false,
  }: { value: string; disabled?: boolean; autoLabel?: string; seed?: string; autoColor?: string; custom?: boolean } = $props();

  // Vorschau für „automatisch“: die Sorte, die ohne eigene Wahl gilt
  let auto = $derived(flavorFor(autoColor, seed));
  // gespeicherte Farbe als Sorte (ältere Farben werden der passenden Sorte zugeordnet)
  let current = $derived(value ? flavorFor(value) : null);
  let isCustom = $derived(current?.name === 'Eigene Farbe');
</script>

<div class="flavors" role="radiogroup" aria-label="Farbe">
  {#if autoLabel}
    <button type="button" class="fl" class:on={!value} role="radio" aria-checked={!value} {disabled} onclick={() => (value = '')} title={autoLabel}>
      <span class="sw auto" style="--bg:{auto.bg}; --c:{auto.color}"><span class="core"></span>{#if !value}<Check class="ck" />{/if}</span>
      <span class="name">{autoLabel}</span>
    </button>
  {/if}
  {#each flavors as f (f.color)}
    {@const on = current?.color === f.color}
    <button type="button" class="fl" class:on role="radio" aria-checked={on} {disabled} onclick={() => (value = f.color)} title={f.name}>
      <span class="sw" style="--bg:{f.bg}; --c:{f.color}"><span class="core"></span>{#if on}<Check class="ck" />{/if}</span>
      <span class="name">{f.name}</span>
    </button>
  {/each}
  {#if custom}
    <label class="fl" class:on={isCustom} title="Eigene Farbe">
      <span class="sw custom" style={isCustom ? `--bg:${flavorFor(value).bg}; --c:${value}` : ''}>
        <span class="core"></span>
        <input type="color" value={isCustom ? value : '#3fd6a4'} {disabled} oninput={(e) => (value = e.currentTarget.value)} aria-label="Eigene Farbe wählen" />
      </span>
      <span class="name">Eigene</span>
    </label>
  {/if}
</div>

<style>
  .flavors {
    display: flex;
    flex-wrap: wrap;
    gap: 10px 6px;
  }
  .fl {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    width: 66px;
    padding: 0;
    border: none;
    background: none;
    color: var(--text-2);
    font: inherit;
    cursor: pointer;
  }
  .fl:disabled {
    cursor: default;
    opacity: 0.6;
  }
  .sw {
    position: relative;
    display: grid;
    place-items: center;
    width: 46px;
    height: 46px;
    border-radius: 14px;
    background: var(--bg);
    overflow: hidden;
    box-shadow: inset -5px 0 7px rgba(0, 0, 0, 0.22);
    transition: transform 0.2s var(--ease), box-shadow 0.2s;
  }
  .core {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--c);
    box-shadow: 0 0 14px 4px color-mix(in srgb, var(--c) 55%, transparent);
  }
  .sw.auto .core {
    opacity: 0.55;
  }
  .sw.auto::after {
    content: 'A';
    position: absolute;
    font: 800 13px/1 var(--font-display);
    color: #fff;
  }
  .sw.custom {
    background: conic-gradient(from 90deg, #e0457b, #f7b32b, #a6d83a, #3fd6a4, #3aa7f0, #a98bf0, #e0457b);
  }
  .sw.custom[style] {
    background: var(--bg);
  }
  .sw.custom input {
    position: absolute;
    inset: 0;
    opacity: 0;
    cursor: pointer;
  }
  .sw :global(.ck) {
    position: absolute;
    width: 16px;
    height: 16px;
    color: var(--bg);
    stroke-width: 3;
  }
  .sw.auto :global(.ck) {
    color: #fff;
  }
  .fl:hover:not(:disabled) .sw {
    transform: translateY(-2px);
  }
  .fl.on .sw {
    box-shadow: inset -5px 0 7px rgba(0, 0, 0, 0.22), 0 0 0 2px var(--surface), 0 0 0 4px var(--text);
  }
  .fl.on .sw.auto::after {
    content: none;
  }
  .name {
    font-size: 12px;
    font-weight: 700;
    white-space: nowrap;
  }
  .fl.on .name {
    color: var(--text);
  }
</style>
