<script lang="ts">
  import FlavorPicker from '../../components/FlavorPicker.svelte';
  import { onMount } from 'svelte';
  import { ExternalLink, Plus, X, TriangleAlert, EyeOff, Megaphone } from '@lucide/svelte';
  import Toggle from '../../components/Toggle.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import PlayerHead from '../../components/PlayerHead.svelte';
  import { api, qs } from '../../lib/api';
  import { can } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import type { Server } from '../../lib/types';

  let { server, onsaved }: { server: Server; onsaved?: () => void } = $props();

  interface PublicConfig {
    title: string;
    description: string;
    accent: string;
    address: string;
    showAddress: boolean;
    showOnline: boolean;
    showLeaderboard: boolean;
    showDeaths: boolean;
    showAdvancements: boolean;
    showChat: boolean;
    showMap: boolean;
    showMapPlayers: boolean;
    showActivity: boolean;
    showProfiles: boolean;
    showVersion: boolean;
    hiddenPlayers: string[];
    links: { label: string; url: string }[];
  }

  const url = $derived(`/api/servers/${server.id}/public`);
  let enabled = $state(false);
  let slug = $state('');
  let cfg = $state<PublicConfig | null>(null);
  let saved = $state('');
  let publicUrl = $state('');
  let globalEnabled = $state(true);
  let saving = $state(false);

  let names = $state<Record<string, string>>({});
  let playerQuery = $state('');
  let playerHits = $state<{ uuid: string; name: string }[]>([]);

  function snapshot() {
    return JSON.stringify({ enabled, slug, cfg });
  }

  function slugify(s: string) {
    return s
      .toLowerCase()
      .replace(/ä/g, 'ae')
      .replace(/ö/g, 'oe')
      .replace(/ü/g, 'ue')
      .replace(/ß/g, 'ss')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 42);
  }

  async function load() {
    try {
      const r = await api.get<{ enabled: boolean; slug: string; config: PublicConfig; url: string; globalEnabled: boolean }>(url);
      enabled = r.enabled;
      slug = r.slug || slugify(server.name);
      cfg = r.config;
      publicUrl = r.url;
      globalEnabled = r.globalEnabled;
      saved = snapshot();
      if (cfg.hiddenPlayers.length && can(server, 'players.read')) {
        for (const u of cfg.hiddenPlayers) {
          api
            .get<{ player: { name: string } }>(`/api/servers/${server.id}/mc/players/${u}`)
            .then((d) => (names[u] = d.player.name))
            .catch(() => {});
        }
      }
    } catch (e) {
      toastError(e);
    }
  }

  onMount(load);

  let dirty = $derived(cfg !== null && snapshot() !== saved);
  let base = $derived(publicUrl ? publicUrl.replace(/\/s\/.*$/, '') : location.origin);
  let pageUrl = $derived(`${base}/s/${slug}`);
  let slugOk = $derived(/^[a-z0-9][a-z0-9-]{1,40}[a-z0-9]$/.test(slug));

  async function save() {
    if (!cfg) return;
    saving = true;
    try {
      const r = await api.put<{ enabled: boolean; slug: string; config: PublicConfig; url: string }>(url, { enabled, slug, config: cfg });
      enabled = r.enabled;
      slug = r.slug;
      cfg = r.config;
      publicUrl = r.url;
      saved = snapshot();
      toastOk(enabled ? 'Öffentliche Seite gespeichert' : 'Gespeichert, die Seite ist nicht öffentlich');
      onsaved?.();
    } catch (e) {
      toastError(e);
    } finally {
      saving = false;
    }
  }

  let searchTimer: ReturnType<typeof setTimeout>;
  $effect(() => {
    const q = playerQuery.trim();
    clearTimeout(searchTimer);
    if (q.length < 2 || !can(server, 'players.read')) {
      playerHits = [];
      return;
    }
    searchTimer = setTimeout(async () => {
      try {
        const r = await api.get<{ players: { uuid: string; name: string }[] }>(`/api/servers/${server.id}/mc/players${qs({ q, limit: 8, sort: 'name', dir: 'asc' })}`);
        playerHits = r.players.filter((p) => !cfg?.hiddenPlayers.includes(p.uuid));
      } catch {}
    }, 200);
  });

  function hide(p: { uuid: string; name: string }) {
    if (!cfg) return;
    names[p.uuid] = p.name;
    cfg.hiddenPlayers = [...cfg.hiddenPlayers, p.uuid];
    playerQuery = '';
    playerHits = [];
  }


  let badgeUrl = $derived(`${base}/api/public/${slug}/badge.svg`);
  let markdownSnippet = $derived(`[![${cfg?.title || server.name}](${badgeUrl})](${pageUrl})`);
  let htmlSnippet = $derived(`<a href="${pageUrl}"><img src="${badgeUrl}" alt="${cfg?.title || server.name}"></a>`);

  const sections: { key: keyof PublicConfig; label: string; hint: string }[] = [
    { key: 'showOnline', label: 'Spieler online', hint: 'Köpfe und Namen der Spieler, die gerade spielen.' },
    { key: 'showLeaderboard', label: 'Bestenlisten', hint: 'Spielzeit, Tode, abgebaute Blöcke und mehr, sortierbar.' },
    { key: 'showProfiles', label: 'Spielerprofile', hint: 'Eigene Seite pro Spieler mit Statistiken und Verlauf.' },
    { key: 'showDeaths', label: 'Todesfeed', hint: 'Die letzten Tode mit Ursache.' },
    { key: 'showAdvancements', label: 'Fortschritte', hint: 'Erzielte Fortschritte und Herausforderungen.' },
    { key: 'showMap', label: 'Karte', hint: 'Die gerenderte Weltkarte zum Erkunden.' },
    { key: 'showActivity', label: 'Aktivität', hint: 'Diagramm der Spielerzahlen der letzten Tage.' },
  ];
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Öffentliche Seite</h2>
      <p>Eine Statusseite für deine Community mit Karte, Bestenlisten und Todesfeed. Sie ist ohne Anmeldung erreichbar und zeigt nur, was du hier freigibst.</p>
    </div>
    {#if enabled && saved && !dirty && server.publicEnabled}
      <a class="btn" href={pageUrl} target="_blank" rel="noopener"><ExternalLink /> Seite öffnen</a>
    {/if}
  </div>

  {#if !globalEnabled}
    <div class="notice warn"><TriangleAlert /><span>Öffentliche Seiten sind in den Panel-Einstellungen ausgeschaltet. Ein Administrator kann sie dort aktivieren.</span></div>
  {/if}

  {#if cfg}
    <section class="panel">
      <div class="panel-body stack">
        <Toggle bind:checked={enabled} label="Seite veröffentlichen" hint="Ausgeschaltet ist die Adresse nicht erreichbar." />
        <div class="field">
          <label for="slug">Adresse</label>
          <div class="slug" class:bad={!slugOk}>
            <span class="pre mono small">{base.replace(/^https?:\/\//, '')}/s/</span>
            <input id="slug" class="mono" bind:value={slug} oninput={() => (slug = slug.toLowerCase())} spellcheck="false" autocomplete="off" />
            <CopyButton text={pageUrl} cls="btn ghost sm icon" />
          </div>
          {#if !slugOk}<p class="error-text">3 bis 42 Zeichen: Kleinbuchstaben, Zahlen und Bindestriche, nicht am Anfang oder Ende.</p>{/if}
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Darstellung</h3></div>
      <div class="panel-body stack">
        <div class="form-grid">
          <div class="field">
            <label for="pt">Titel</label>
            <input id="pt" class="input" bind:value={cfg.title} placeholder={server.name} maxlength="80" />
          </div>
          <div class="field">
            <label for="pa">Adresse zum Verbinden</label>
            <input id="pa" class="input mono" bind:value={cfg.address} placeholder={server.displayAddress || `${location.hostname}:${server.port}`} maxlength="120" />
          </div>
        </div>
        <div class="field">
          <label for="pd">Beschreibung</label>
          <textarea id="pd" class="textarea" rows="4" bind:value={cfg.description} maxlength="2000" placeholder="Worum geht es auf eurem Server? Regeln, Discord, Besonderheiten …"></textarea>
          <p class="hint">Absätze, **fett**, *kursiv* und [Links](https://…) werden unterstützt.</p>
        </div>
        <div class="field">
          <span class="label">Farbe der Seite</span>
          <FlavorPicker bind:value={cfg.accent} autoLabel="Wie Server" autoColor={server.color} seed={server.id} custom />
          <p class="hint">Bestimmt das farbige Band oben und den Lichthof. „Wie Server“ übernimmt die Farbe aus den allgemeinen Einstellungen.</p>
        </div>
        <div class="toggles">
          <Toggle bind:checked={cfg.showAddress} label="Adresse anzeigen" hint="Mit Knopf zum Kopieren." />
          <Toggle bind:checked={cfg.showVersion} label="Version anzeigen" />
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Inhalte</h3></div>
      <div class="panel-body grid2">
        {#each sections as s (s.key)}
          <Toggle checked={!!cfg[s.key]} label={s.label} hint={s.hint} onchange={(v) => cfg && ((cfg as any)[s.key] = v)} />
        {/each}
        {#if cfg.showMap}
          <Toggle bind:checked={cfg.showMapPlayers} label="Spieler auf der Karte" hint="Live-Positionen der Spieler. Überlege, ob das auf deinem Server erwünscht ist." />
        {/if}
        <Toggle
          bind:checked={cfg.showChat}
          label="Chatnachrichten"
          hint={server.settings?.trackChat ? 'Zeigt den Chatverlauf öffentlich.' : 'Chat-Aufzeichnung ist unter „Start & Version“ ausgeschaltet.'}
          disabled={!server.settings?.trackChat}
        />
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Links</h3><span class="spacer"></span><span class="small muted">{cfg.links.length} / 8</span></div>
      <div class="panel-body stack">
        {#each cfg.links as l, i (i)}
          <div class="linkrow">
            <input class="input" bind:value={l.label} placeholder="Discord" maxlength="40" aria-label="Linktext" />
            <input class="input" bind:value={l.url} placeholder="https://discord.gg/…" aria-label="Adresse" />
            <button class="btn ghost icon" onclick={() => cfg && (cfg.links = cfg.links.filter((_, j) => j !== i))} aria-label="Link entfernen"><X /></button>
          </div>
        {/each}
        {#if cfg.links.length < 8}
          <div><button class="btn sm" onclick={() => cfg && (cfg.links = [...cfg.links, { label: '', url: '' }])}><Plus /> Link</button></div>
        {/if}
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><EyeOff size={16} /><h3>Ausgeblendete Spieler</h3></div>
      <div class="panel-body stack">
        <p class="small muted">Diese Spieler erscheinen nirgends auf der öffentlichen Seite, weder online noch in Bestenlisten oder Feeds.</p>
        {#if cfg.hiddenPlayers.length}
          <ul class="hidden">
            {#each cfg.hiddenPlayers as u (u)}
              <li>
                <PlayerHead uuid={u} name={names[u]} size={22} />
                <span class={names[u] ? '' : 'mono small'}>{names[u] ?? u}</span>
                <button class="btn ghost sm icon" onclick={() => cfg && (cfg.hiddenPlayers = cfg.hiddenPlayers.filter((x) => x !== u))} aria-label="Wieder anzeigen"><X /></button>
              </li>
            {/each}
          </ul>
        {/if}
        {#if can(server, 'players.read')}
          <div class="psearch">
            <input class="input" bind:value={playerQuery} placeholder="Spieler suchen und ausblenden" aria-label="Spieler suchen" />
            {#if playerHits.length}
              <ul class="hits">
                {#each playerHits as p (p.uuid)}
                  <li><button onclick={() => hide(p)}><PlayerHead uuid={p.uuid} name={p.name} size={20} /> {p.name}</button></li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
      </div>
    </section>

    {#if slugOk}
      <section class="panel">
        <div class="panel-head"><Megaphone size={16} /><h3>Status-Badge</h3></div>
        <div class="panel-body stack">
          <p class="small muted">Zeigt live, ob der Server online ist und wie viele spielen, etwa auf eurer Webseite oder im Discord.</p>
          {#if server.publicEnabled}<img class="badge" src={badgeUrl} alt="Status-Badge" />{/if}
          <div class="snip">
            <span class="tiny faint">Markdown</span>
            <code>{markdownSnippet}</code>
            <CopyButton text={markdownSnippet} cls="btn ghost sm icon" />
          </div>
          <div class="snip">
            <span class="tiny faint">HTML</span>
            <code>{htmlSnippet}</code>
            <CopyButton text={htmlSnippet} cls="btn ghost sm icon" />
          </div>
        </div>
      </section>
    {/if}

    {#if dirty}
      <div class="savebar">
        <span class="small">Ungespeicherte Änderungen</span>
        <span class="spacer"></span>
        <button class="btn" onclick={load}>Verwerfen</button>
        <button class="btn primary" onclick={save} disabled={saving || !slugOk}>Speichern</button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .slug {
    display: flex;
    align-items: center;
    height: 38px;
    border: 1px solid var(--line-2);
    border-radius: var(--r-s);
    background: var(--bg);
    padding: 0 4px 0 11px;
    max-width: 640px;
  }
  .slug:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .slug.bad {
    border-color: var(--danger);
  }
  .pre {
    color: var(--text-3);
    white-space: nowrap;
  }
  .slug input {
    flex: 1;
    min-width: 60px;
    border: none;
    background: none;
    color: var(--text);
    font: inherit;
    font-family: var(--font-mono);
    font-size: 14px;
    outline: none;
    height: 100%;
  }
  .toggles,
  .grid2 {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 8px 28px;
  }
  .linkrow {
    display: grid;
    grid-template-columns: 180px 1fr auto;
    gap: 8px;
  }
  .hidden {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .hidden li {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 3px 3px 3px 6px;
    border-radius: 7px;
    background: var(--surface-2);
    font-size: 14px;
  }
  .psearch {
    position: relative;
    max-width: 360px;
  }
  .hits {
    list-style: none;
    margin: 4px 0 0;
    padding: 4px;
    border: 1px solid var(--line-2);
    border-radius: var(--r-m);
    background: var(--surface);
    box-shadow: var(--pop-shadow);
    position: absolute;
    left: 0;
    right: 0;
    z-index: 5;
  }
  .hits button {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 8px;
    border: none;
    background: none;
    border-radius: 6px;
    color: var(--text);
    font: inherit;
    font-size: 14px;
    cursor: pointer;
  }
  .hits button:hover {
    background: var(--surface-2);
  }
  .badge {
    align-self: flex-start;
    height: 20px;
  }
  .snip {
    display: grid;
    grid-template-columns: 70px 1fr auto;
    align-items: center;
    gap: 10px;
  }
  .snip code {
    overflow-x: auto;
    white-space: nowrap;
    font-size: 12px;
    padding: 6px 8px;
  }
  .panel-head :global(svg) {
    color: var(--text-3);
  }
  @media (max-width: 640px) {
    .linkrow {
      grid-template-columns: 1fr auto;
    }
    .linkrow .input:first-child {
      grid-column: 1 / -1;
    }
    .snip {
      grid-template-columns: 1fr auto;
    }
    .snip span {
      grid-column: 1 / -1;
    }
  }
</style>
