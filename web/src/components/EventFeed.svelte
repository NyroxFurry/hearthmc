<script lang="ts">
  import { Skull, Trophy, MessageSquare, LogIn, LogOut } from '@lucide/svelte';
  import PlayerHead from './PlayerHead.svelte';
  import type { MCEvent } from '../lib/types';
  import { ago, dateTime, duration } from '../lib/format';
  import { advancementText } from '../lib/mc';

  let {
    events,
    causeLabels = {},
    playerHref = null,
    showHead = true,
    dense = false,
  }: {
    events: MCEvent[];
    causeLabels?: Record<string, string>;
    playerHref?: ((uuid: string) => string) | null;
    showHead?: boolean;
    dense?: boolean;
  } = $props();

  const icons: Record<string, any> = { death: Skull, advancement: Trophy, chat: MessageSquare, join: LogIn, leave: LogOut };

  function deathText(e: MCEvent): { rest: string } {
    // the raw log message starts with the player name
    const m = e.message || '';
    return { rest: m.startsWith(e.name) ? m.slice(e.name.length) : ' ' + m };
  }
</script>

<ul class="feed" class:dense>
  {#each events as e (e.id)}
    {@const Icon = icons[e.type] ?? MessageSquare}
    <li class="ev {e.type}">
      <span class="ic" aria-hidden="true"><Icon /></span>
      {#if showHead}<PlayerHead uuid={e.uuid} name={e.name} size={dense ? 20 : 24} />{/if}
      <p class="txt">
        {#snippet who()}
          {#if playerHref && e.uuid}<a class="who" href={playerHref(e.uuid)}>{e.name}</a>{:else}<strong class="who">{e.name}</strong>{/if}
        {/snippet}
        {#if e.type === 'death'}
          {@render who()}<span>{deathText(e).rest}</span>
          {#if e.data?.cause && causeLabels[e.data.cause]}<span class="tag danger cause">{causeLabels[e.data.cause]}</span>{/if}
        {:else if e.type === 'advancement'}
          {@const t = advancementText(e.data?.kind)}
          {@render who()} {t.before} <span class="adv">[{e.data?.title ?? e.message}]</span> {t.after}
        {:else if e.type === 'chat'}
          {@render who()}<span class="msg">{e.message}</span>
        {:else if e.type === 'join'}
          {@render who()} hat den Server betreten
        {:else if e.type === 'leave'}
          {@render who()} hat den Server verlassen{#if e.data?.duration > 0}{' '}<span class="faint">nach {duration(e.data.duration, true)}</span>{/if}
        {:else}
          {@render who()} {e.message}
        {/if}
      </p>
      <time class="tiny faint" datetime={new Date(e.at * 1000).toISOString()} title={dateTime(e.at)}>{ago(e.at)}</time>
    </li>
  {/each}
</ul>

<style>
  .feed {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .ev {
    display: grid;
    grid-template-columns: 18px auto 1fr auto;
    align-items: center;
    gap: 10px;
    padding: 9px 16px;
    border-bottom: 1px solid var(--line);
  }
  .ev:last-child {
    border-bottom: none;
  }
  .dense .ev {
    padding: 7px 14px;
    gap: 9px;
  }
  .feed:not(:has(img)) .ev {
    grid-template-columns: 18px 1fr auto;
  }
  .ic {
    display: grid;
    place-items: center;
    color: var(--text-3);
  }
  .ic :global(svg) {
    width: 15px;
    height: 15px;
  }
  .death .ic {
    color: var(--danger);
  }
  .advancement .ic {
    color: var(--warn);
  }
  .join .ic {
    color: var(--accent);
  }
  .chat .ic {
    color: var(--info);
  }
  .txt {
    min-width: 0;
    font-size: 14px;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }
  .dense .txt {
    font-size: 13.5px;
  }
  .who {
    font-weight: 650;
    color: var(--text);
  }
  a.who:hover {
    color: var(--accent);
  }
  .adv {
    color: var(--warn);
    font-weight: 560;
  }
  .msg {
    color: var(--text-2);
  }
  .msg::before {
    content: ': ';
    color: var(--text-3);
  }
  .cause {
    margin-left: 8px;
    height: 19px;
    font-size: 11.5px;
    vertical-align: 1px;
  }
  time {
    white-space: nowrap;
  }
</style>
