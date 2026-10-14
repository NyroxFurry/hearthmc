<script lang="ts">
  import { auditTitle, auditDetail } from '../lib/audit';
  import { dateTime, time } from '../lib/format';
  import type { AuditEntry } from '../lib/types';

  let {
    entries,
    serverNames = null,
  }: { entries: AuditEntry[]; serverNames?: Record<string, string> | null } = $props();

  // group by calendar day
  let days = $derived.by(() => {
    const out: { day: string; items: AuditEntry[] }[] = [];
    for (const e of entries) {
      const d = new Date(e.at * 1000).toLocaleDateString('de-DE', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
      if (!out.length || out[out.length - 1].day !== d) out.push({ day: d, items: [] });
      out[out.length - 1].items.push(e);
    }
    return out;
  });

  const tone = (a: string) =>
    a.includes('delete') || a.includes('failed') || a.includes('reset') || a === 'server.power' ? 'warn' : a.startsWith('auth') || a.startsWith('account') ? 'info' : '';
</script>

{#each days as d (d.day)}
  <h4 class="day">{d.day}</h4>
  <ul class="log">
    {#each d.items as e (e.id)}
      <li>
        <time class="tiny faint num" title={dateTime(e.at)}>{time(e.at)}</time>
        <span class="dot {tone(e.action)}" aria-hidden="true"></span>
        <div class="what">
          <span class="t">{auditTitle(e)}</span>
          {#if auditDetail(e)}<span class="d small muted mono">{auditDetail(e)}</span>{/if}
        </div>
        <div class="who small">
          {#if serverNames && e.serverId}<span class="faint">{serverNames[e.serverId] ?? e.serverId}</span>{/if}
          <span>{e.username || 'System'}</span>
          {#if e.ip}<span class="tiny faint mono">{e.ip}</span>{/if}
        </div>
      </li>
    {/each}
  </ul>
{/each}

<style>
  .day {
    font-size: 13px;
    font-weight: 620;
    color: var(--text-2);
    padding: 14px 16px 6px;
  }
  .day:first-child {
    padding-top: 12px;
  }
  .log {
    list-style: none;
    margin: 0;
    padding: 0 0 6px;
  }
  .log li {
    display: grid;
    grid-template-columns: 44px 8px minmax(0, 1fr) auto;
    align-items: baseline;
    gap: 12px;
    padding: 6px 16px;
  }
  .log li:hover {
    background: color-mix(in srgb, var(--surface-2) 55%, transparent);
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 2px;
    background: var(--line-2);
    align-self: center;
  }
  .dot.warn {
    background: var(--copper);
  }
  .dot.info {
    background: var(--info);
  }
  .what {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .t {
    font-size: 14px;
    font-weight: 560;
  }
  .d {
    overflow-wrap: anywhere;
  }
  .who {
    display: flex;
    align-items: baseline;
    gap: 8px;
    white-space: nowrap;
  }
  @media (max-width: 700px) {
    .log li {
      grid-template-columns: 40px 8px 1fr;
    }
    .who {
      grid-column: 3;
    }
  }
</style>
