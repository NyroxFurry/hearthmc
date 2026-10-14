<script lang="ts">
  import PlayerHead from '../components/PlayerHead.svelte';
  import { api, qs } from '../lib/api';
  import { statByKey, statValue, type StatCol } from '../lib/mc';

  let {
    slug,
    profiles,
    onplayer,
  }: { slug: string; profiles: boolean; onplayer: (uuid: string) => void } = $props();

  const cats: StatCol[] = ['playtime', 'deaths', 'mobKills', 'blocksMined', 'distance', 'advancements', 'playerKills', 'fishCaught', 'jumps'].map(
    (k) => statByKey[k],
  );

  let cat = $state('playtime');
  let rows = $state<any[]>([]);
  let total = $state(0);
  let loading = $state(false);
  let error = $state('');
  let col = $derived(statByKey[cat]);

  async function load(more = false) {
    loading = true;
    try {
      const r = await api.get<{ players: any[]; total: number }>(
        `/api/public/${slug}/players${qs({ sort: cat, limit: more ? 25 : 10, offset: more ? rows.length : 0 })}`,
      );
      rows = more ? [...rows, ...r.players] : r.players;
      total = r.total;
      error = '';
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    cat;
    load();
  });

  const medal = (i: number) => (i === 0 ? 'gold' : i === 1 ? 'iron' : i === 2 ? 'copper' : '');
</script>

<div class="board">
  <div class="cats" role="tablist" aria-label="Kategorie">
    {#each cats as c (c.key)}
      <button role="tab" aria-selected={cat === c.key} class:on={cat === c.key} onclick={() => (cat = c.key)}>{c.short ?? c.label}</button>
    {/each}
  </div>

  {#if error}
    <p class="muted small pad">{error}</p>
  {:else if rows.length === 0 && !loading}
    <p class="muted pad">Noch keine Einträge. Die Liste füllt sich, sobald jemand eine Weile gespielt hat.</p>
  {:else}
    <ol class="rows">
      {#each rows as p, i (p.uuid)}
        {@const v = statValue(p, col)}
        <li class={medal(i)}>
          <span class="rank num">{i + 1}</span>
          <PlayerHead uuid={p.uuid} name={p.name} size={30} />
          {#if profiles}
            <button class="name" onclick={() => onplayer(p.uuid)}>{p.name}</button>
          {:else}
            <span class="name">{p.name}</span>
          {/if}
          {#if p.online}<span class="live" title="Gerade online"></span>{/if}
          <span class="spacer"></span>
          <span class="val num">{col.fmt(v)}</span>
        </li>
      {/each}
    </ol>
    {#if rows.length < total}
      <div class="more"><button class="btn sm" onclick={() => load(true)} disabled={loading}>Weitere anzeigen</button></div>
    {/if}
  {/if}
</div>

<style>
  .cats {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 16px 18px 8px;
  }
  .cats button {
    display: inline-flex;
    align-items: center;
    min-height: 34px;
    border: 1px solid var(--line);
    background: none;
    color: var(--text-2);
    font: inherit;
    font-size: 13.5px;
    font-weight: 700;
    padding: 0 13px;
    border-radius: 999px;
    cursor: pointer;
    transition: border-color 0.2s, color 0.2s, background-color 0.2s;
  }
  .cats button:hover {
    color: var(--text);
    border-color: var(--text);
  }
  .cats button.on {
    background: var(--band);
    border-color: var(--band);
    color: #fff;
  }
  .pad {
    padding: 18px 20px 22px;
  }
  .rows {
    list-style: none;
    margin: 0;
    padding: 6px 10px 12px;
  }
  .rows li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 10px;
    border-radius: 12px;
  }
  .rows li:hover {
    background: var(--surface-2);
  }
  .rank {
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    font-size: 13px;
    font-weight: 700;
    color: var(--text-3);
    flex: none;
  }
  /* das Podest in Minecrafts Metallen: Gold, Eisen und Kupfer */
  .gold .rank {
    background: linear-gradient(160deg, #f6d860, #c99a1e);
    color: #3b2a00;
  }
  .iron .rank {
    background: linear-gradient(160deg, #e9edf1, #a9b1ba);
    color: #2b3138;
  }
  .copper .rank {
    background: linear-gradient(160deg, #f0a875, #b8622f);
    color: #3a1a06;
  }
  .rows :global(img) {
    border-radius: 6px;
  }
  .name {
    border: none;
    background: none;
    padding: 0;
    color: var(--text);
    font: inherit;
    font-weight: 700;
    font-size: 15px;
    cursor: pointer;
    text-align: left;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  button.name:hover {
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  .live {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #2fbf8a;
    flex: none;
  }
  .val {
    font-family: var(--font-display);
    font-weight: 700;
    font-size: 15px;
  }
  .more {
    display: flex;
    justify-content: center;
    padding: 0 0 16px;
  }
</style>
