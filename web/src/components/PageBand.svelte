<script lang="ts">
  // Farbiges Band oben auf einer Seite: Titel in Unbounded, helle Schrift,
  // Lichthof in der Sortenfarbe.
  import type { Snippet } from 'svelte';

  let {
    title,
    kicker = '',
    sub,
    actions,
    children,
  }: { title: string; kicker?: string; sub?: Snippet; actions?: Snippet; children?: Snippet } = $props();
</script>

<header class="pband band">
  <div class="glow" aria-hidden="true"></div>
  <div class="inner">
    <div class="titles">
      {#if kicker}<p class="kicker">{kicker}</p>{/if}
      <h1>{title}</h1>
      {#if sub}<p class="sub">{@render sub()}</p>{/if}
    </div>
    {#if actions}<div class="actions">{@render actions()}</div>{/if}
  </div>
  {#if children}<div class="extra">{@render children()}</div>{/if}
</header>

<style>
  .pband {
    position: relative;
    overflow: hidden;
    padding: 30px 38px 30px;
    transition: background-color 0.9s ease;
  }
  .glow {
    width: 420px;
    height: 420px;
    right: 6%;
    top: -260px;
  }
  .inner {
    position: relative;
    display: flex;
    align-items: flex-end;
    gap: 16px;
    flex-wrap: wrap;
    max-width: 1320px;
  }
  .titles {
    flex: 1;
    min-width: 240px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .kicker {
    color: var(--band-soft);
  }
  h1 {
    font-size: clamp(28px, 3.4vw, 42px);
    line-height: 1.05;
    letter-spacing: -0.02em;
  }
  .sub {
    color: var(--band-soft);
    font-size: 15.5px;
    max-width: 62ch;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .extra {
    position: relative;
    max-width: 1320px;
  }
  @media (max-width: 860px) {
    .pband {
      padding: 22px 16px 22px;
    }
  }
</style>
