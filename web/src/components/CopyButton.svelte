<script lang="ts">
  import { Copy, Check } from '@lucide/svelte';
  import type { Snippet } from 'svelte';

  let { text, label = '', cls = 'btn ghost sm', children }: { text: string; label?: string; cls?: string; children?: Snippet } = $props();
  let done = $state(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      ta.remove();
    }
    done = true;
    setTimeout(() => (done = false), 1400);
  }
</script>

<button class={cls} onclick={copy} title="Kopieren" aria-label={label || 'Kopieren'}>
  {#if children}{@render children()}{/if}
  {#if done}<Check />{:else}<Copy />{/if}
  {#if label}<span>{done ? 'Kopiert' : label}</span>{/if}
</button>
