<script lang="ts">
  // Ein Server erscheint als Kachel: sein server-icon.png in scharfen Pixeln
  // oder eine Kachel in seiner Sortenfarbe mit dem Anfangsbuchstaben.
  import { flavorFor } from '../lib/flavors';

  let {
    id,
    name,
    color = '',
    size = 40,
    version = 0,
    hasIcon = true,
  }: { id: string; name: string; color?: string; size?: number; version?: number; hasIcon?: boolean } = $props();
  let failed = $state(false);
  let flavor = $derived(flavorFor(color, id));
  let initial = $derived((name.trim()[0] || '?').toUpperCase());
  $effect(() => {
    version;
    id;
    failed = false;
  });
</script>

<span class="icon" style="--s:{size}px; --c:{flavor.color}; --ink:{flavor.ink}">
  {#if hasIcon && !failed}
    <img src="/api/servers/{id}/icon.png{version ? `?v=${version}` : ''}" alt="" onerror={() => (failed = true)} />
  {:else}
    <span class="letter">{initial}</span>
  {/if}
</span>

<style>
  .icon {
    width: var(--s);
    height: var(--s);
    flex: none;
    display: grid;
    place-items: center;
    border-radius: calc(var(--s) * 0.26);
    overflow: hidden;
    background: var(--c);
    /* leichter Schatten an der Kante, damit die Kachel etwas Tiefe bekommt */
    box-shadow: inset calc(var(--s) * -0.1) 0 calc(var(--s) * 0.14) rgba(0, 0, 0, 0.16);
  }
  img {
    width: 100%;
    height: 100%;
    image-rendering: pixelated;
    display: block;
    background: var(--surface-3);
  }
  .letter {
    color: var(--ink);
    font-family: var(--font-display);
    font-weight: 800;
    font-size: calc(var(--s) * 0.42);
    line-height: 1;
    margin-right: calc(var(--s) * 0.04);
  }
</style>
