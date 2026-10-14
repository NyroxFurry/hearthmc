<script lang="ts">
  import type { Snippet } from 'svelte';
  import PlayerHead from './PlayerHead.svelte';
  import BarChart from './BarChart.svelte';
  import EventFeed from './EventFeed.svelte';
  import { statCols, statValue, itemName, customStatLabels } from '../lib/mc';
  import { ago, date, dateTime, duration, hours, num, distance, time } from '../lib/format';
  import type { PlayerDetail } from '../lib/types';

  let { detail, actions }: { detail: PlayerDetail; actions?: Snippet } = $props();

  let p = $derived(detail.player);
  let values = $derived({ ...p, playSeconds: detail.playSeconds } as Record<string, any>);

  let bars = $derived(
    detail.daily.map((d) => {
      const dt = new Date(d.day + 'T12:00:00');
      return {
        label: dt.toLocaleDateString('de-DE', { day: 'numeric', month: 'numeric' }),
        title: dt.toLocaleDateString('de-DE', { weekday: 'long', day: 'numeric', month: 'long' }),
        value: d.seconds,
      };
    }),
  );
  let last30 = $derived(detail.daily.reduce((a, d) => a + d.seconds, 0));

  let causes = $derived(
    Object.entries(detail.deathCauses ?? {})
      .map(([k, v]) => ({ key: k, label: detail.causeLabels?.[k] ?? k, n: v }))
      .sort((a, b) => b.n - a.n),
  );
  let causeMax = $derived(Math.max(1, ...causes.map((c) => c.n)));
  let causeTotal = $derived(causes.reduce((a, c) => a + c.n, 0));

  let tops = $derived.by(() => {
    const s = detail.stats;
    if (!s) return [];
    return [
      { title: 'Am meisten abgebaut', list: s.mined },
      { title: 'Besiegte Kreaturen', list: s.killed },
      { title: 'Gestorben durch', list: s.killedBy },
      { title: 'Hergestellt', list: s.crafted },
      { title: 'Benutzt', list: s.used },
      { title: 'Aufgehoben', list: s.pickedUp },
    ].filter((t) => t.list?.length);
  });

  let extras = $derived.by(() => {
    const c = detail.stats?.custom;
    if (!c) return [];
    return customStatLabels
      .filter(([k]) => (c[k] ?? 0) > 0)
      .map(([k, label, kind]) => ({
        label,
        value: kind === 'time' ? duration(c[k] / 20, true) : kind === 'distance' ? distance(c[k]) : num(c[k]),
      }));
  });
</script>

<div class="profile">
  <header class="hero">
    <div class="head-wrap" class:online={detail.online}>
      <PlayerHead uuid={p.uuid} name={p.name} size={88} />
    </div>
    <div class="ident">
      <h2>{p.name}</h2>
      <p class="status">
        {#if detail.online}
          <span class="state running">Online seit {time(detail.onlineSince ?? 0)}</span>
        {:else}
          <span class="muted">Zuletzt online {ago(p.lastSeen)}</span>
        {/if}
      </p>
      <p class="small faint">Erstmals gesehen am {date(p.firstSeen)}</p>
    </div>
    <div class="playtime">
      <span class="pt-value num">{hours(detail.playSeconds)}</span>
      <span class="small muted">Spielzeit insgesamt</span>
    </div>
    {#if actions}<div class="actions">{@render actions()}</div>{/if}
  </header>

  <div class="slab">
    {#each statCols.slice(1) as c (c.key)}
      <div class="cell">
        <span class="v num">{c.fmt(statValue(values, c))}</span>
        <span class="l">{c.label}</span>
      </div>
    {/each}
  </div>

  <div class="grid">
    <section class="panel">
      <div class="panel-head">
        <h3>Letzte 30 Tage</h3>
        <span class="spacer"></span>
        <span class="small muted num">{hours(last30)}</span>
      </div>
      <div class="panel-body">
        <BarChart {bars} format={(v) => (v > 0 ? hours(v) : 'nicht online')} />
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Todesursachen</h3><span class="spacer"></span>{#if causeTotal}<span class="small muted num">{num(causeTotal)} aufgezeichnet</span>{/if}</div>
      <div class="panel-body">
        {#if causes.length}
          <ul class="hbars">
            {#each causes as c (c.key)}
              <li>
                <span class="hl">{c.label}</span>
                <span class="track"><span style="width:{(c.n / causeMax) * 100}%"></span></span>
                <span class="hn num">{num(c.n)}</span>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="muted small">Noch keine Tode aufgezeichnet.</p>
        {/if}
      </div>
    </section>
  </div>

  {#if tops.length}
    <div class="tops">
      {#each tops as t (t.title)}
        <section class="panel">
          <div class="panel-head"><h3>{t.title}</h3></div>
          <ol class="toplist">
            {#each t.list as it (it.id)}
              <li><span class="ellipsis">{itemName(it.id)}</span><span class="num">{num(it.value)}</span></li>
            {/each}
          </ol>
        </section>
      {/each}
    </div>
  {/if}

  <div class="grid">
    <section class="panel">
      <div class="panel-head"><h3>Ereignisse</h3></div>
      {#if detail.events.length}
        <EventFeed events={detail.events} causeLabels={detail.causeLabels} showHead={false} dense />
      {:else}
        <p class="panel-body muted small">Noch keine Tode, Fortschritte oder Nachrichten.</p>
      {/if}
    </section>
    <div class="stack">
      {#if extras.length}
        <section class="panel">
          <div class="panel-head"><h3>Weitere Werte</h3></div>
          <dl class="extras">
            {#each extras as x (x.label)}
              <dt>{x.label}</dt>
              <dd class="num">{x.value}</dd>
            {/each}
          </dl>
        </section>
      {/if}
      {#if detail.sessions?.length}
        <section class="panel">
          <div class="panel-head"><h3>Letzte Sitzungen</h3></div>
          <table class="table">
            <tbody>
              {#each detail.sessions as s (s.joinedAt)}
                <tr>
                  <td>{dateTime(s.joinedAt)}</td>
                  <td class="r num">{s.leftAt ? duration(s.leftAt - s.joinedAt, true) : 'läuft'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </section>
      {/if}
    </div>
  </div>
</div>

<style>
  .profile {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .hero {
    display: flex;
    align-items: center;
    gap: 20px;
    flex-wrap: wrap;
  }
  .head-wrap {
    padding: 5px;
    border-radius: 10px;
    background: var(--surface-2);
    border: 1px solid var(--line);
    line-height: 0;
  }
  .head-wrap.online {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .head-wrap :global(img) {
    border-radius: 5px;
  }
  .ident {
    flex: 1;
    min-width: 180px;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .ident h2 {
    font-size: 28px;
  }
  .playtime {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
  }
  .pt-value {
    font-family: var(--font-display);
    font-size: 28px;
    font-weight: 800;
    color: var(--text);
    line-height: 1.1;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .slab {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--r-l);
    overflow: hidden;
  }
  .cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 13px 16px;
    box-shadow: 1px 0 0 var(--line), 0 1px 0 var(--line);
  }
  .cell .v {
    font-size: 19px;
    font-weight: 680;
  }
  .cell .l {
    font-size: 12.5px;
    color: var(--text-3);
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 18px;
    align-items: start;
  }
  .hbars {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .hbars li {
    display: grid;
    grid-template-columns: 130px 1fr 44px;
    align-items: center;
    gap: 10px;
    font-size: 13.5px;
  }
  .track {
    height: 8px;
    background: var(--surface-3);
    border-radius: 3px;
    overflow: hidden;
  }
  .track span {
    display: block;
    height: 100%;
    background: var(--danger);
    border-radius: 3px;
  }
  .hn {
    text-align: right;
    color: var(--text-2);
  }
  .tops {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
    gap: 18px;
  }
  .toplist {
    list-style: none;
    margin: 0;
    padding: 6px 0;
    counter-reset: n;
  }
  .toplist li {
    display: grid;
    grid-template-columns: 22px 1fr auto;
    gap: 6px;
    padding: 5px 16px;
    font-size: 13.5px;
    counter-increment: n;
  }
  .toplist li::before {
    content: counter(n);
    color: var(--text-3);
    font-variant-numeric: tabular-nums;
  }
  .toplist .num {
    color: var(--text-2);
  }
  .extras {
    display: grid;
    grid-template-columns: 1fr auto;
    margin: 0;
    padding: 8px 16px 12px;
    gap: 7px 12px;
    font-size: 13.5px;
  }
  .extras dt {
    color: var(--text-2);
  }
  .extras dd {
    margin: 0;
    font-weight: 600;
  }
  @media (max-width: 900px) {
    .grid {
      grid-template-columns: 1fr;
    }
    .playtime {
      align-items: flex-start;
    }
  }
</style>
