<script lang="ts">
  // Anmeldung, Ersteinrichtung und Einladung: die ganze Seite im Farbband,
  // das Formular als helles Blatt darauf.
  import type { Snippet } from 'svelte';
  import Flame from './Flame.svelte';
  import { session } from '../lib/session.svelte';

  let {
    title,
    subtitle = '',
    kicker = 'Gameserver-Panel',
    children,
    wide = false,
  }: { title: string; subtitle?: string; kicker?: string; children: Snippet; wide?: boolean } = $props();
</script>

<div class="auth">
  <div class="glow" aria-hidden="true"></div>
  <header class="top">
    <span class="logo"><Flame size={34} /><span>{session.panel.name || 'Hearth'}</span></span>
  </header>
  <main class="center">
    <p class="kick">{kicker}</p>
    <h1>{title}</h1>
    {#if subtitle}<p class="sub">{subtitle}</p>{/if}
    <div class="card" class:wide>
      {@render children()}
    </div>
  </main>
  <footer class="foot">
    <p class="meta">
      <span>Hearth {session.panel.version}</span>
      <a href="/licenses/THIRD_PARTY_LICENSES.txt" target="_blank" rel="noopener">Lizenzen</a>
    </p>
    <p>Kein offizielles Minecraft-Produkt. Nicht von Mojang oder Microsoft genehmigt oder mit ihnen verbunden.</p>
  </footer>
</div>

<style>
  .auth {
    position: relative;
    min-height: 100vh;
    min-height: 100svh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--band);
    color: var(--band-ink);
    padding: 0 clamp(16px, 4vw, 32px);
  }
  .glow {
    position: absolute;
    left: 50%;
    top: 58%;
    width: min(560px, 90vw);
    aspect-ratio: 1;
    border-radius: 50%;
    transform: translate(-50%, -50%);
    background: var(--flavor);
    opacity: 0.3;
    filter: blur(70px);
    pointer-events: none;
  }
  .top {
    position: relative;
    display: flex;
    align-items: center;
    padding-block: 20px;
    max-width: 1280px;
    width: 100%;
    margin: 0 auto;
  }
  .logo {
    display: flex;
    align-items: center;
    gap: 12px;
    font: 800 22px/1 var(--font-display);
    letter-spacing: -0.01em;
  }
  .center {
    position: relative;
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 16px 0 40px;
  }
  .kick {
    margin: 0 0 12px;
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.22em;
    text-transform: uppercase;
    color: var(--band-soft);
  }
  h1 {
    font-size: clamp(30px, 5vw, 50px);
    line-height: 1.05;
    letter-spacing: -0.02em;
    max-width: 16ch;
  }
  .sub {
    margin-top: 12px;
    max-width: 46ch;
    font-size: 16.5px;
    color: var(--band-soft);
    text-wrap: pretty;
  }
  .card {
    width: 100%;
    max-width: 420px;
    margin-top: 30px;
    padding: 28px;
    text-align: left;
    background: var(--surface);
    color: var(--text);
    border-radius: 18px;
    box-shadow: 0 30px 60px -30px rgba(0, 0, 0, 0.6);
  }
  .card.wide {
    max-width: 540px;
  }
  .foot {
    position: relative;
    text-align: center;
    padding: 0 0 18px;
    font-size: 12.5px;
    color: var(--band-soft);
  }
  .foot p {
    margin: 0 auto;
    max-width: 60ch;
    text-wrap: balance;
  }
  .meta {
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 18px;
  }
  .meta a {
    display: inline-flex;
    align-items: center;
    min-height: 40px;
    color: var(--band-ink);
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  @media (max-width: 520px) {
    .card {
      padding: 22px 18px;
    }
  }
</style>
