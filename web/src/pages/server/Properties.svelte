<script lang="ts">
  import { onMount } from 'svelte';
  import { Search, Lock, TriangleAlert, RotateCw, Plus } from '@lucide/svelte';
  import Toggle from '../../components/Toggle.svelte';
  import { api } from '../../lib/api';
  import { can } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { promptDialog } from '../../lib/dialogs.svelte';
  import { mcToHtml } from '../../lib/mctext';
  import type { Server, Status } from '../../lib/types';

  let { server, status }: { server: Server; status: Status } = $props();

  interface PropInfo {
    key: string;
    type: 'bool' | 'int' | 'string' | 'enum';
    label: string;
    description?: string;
    group: string;
    options?: string[];
    default: string;
    managed?: boolean;
    min?: number;
    max?: number;
  }

  const url = $derived(`/api/servers/${server.id}/mc/properties`);
  const canEdit = $derived(can(server, 'settings.edit'));
  let known = $state<PropInfo[]>([]);
  let saved = $state<Record<string, string>>({});
  let values = $state<Record<string, string>>({});
  let filter = $state('');
  let loaded = $state(false);
  let saving = $state(false);
  let restartHint = $state(false);

  async function load() {
    try {
      const r = await api.get<{ values: Record<string, string>; known: PropInfo[] }>(url);
      known = r.known;
      saved = { ...r.values };
      values = { ...r.values };
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }

  onMount(load);

  const optionLabels: Record<string, Record<string, string>> = {
    difficulty: { peaceful: 'Friedlich', easy: 'Einfach', normal: 'Normal', hard: 'Schwer' },
    gamemode: { survival: 'Überleben', creative: 'Kreativ', adventure: 'Abenteuer', spectator: 'Zuschauer' },
    'level-type': {
      'minecraft:normal': 'Standard',
      'minecraft:flat': 'Flachland',
      'minecraft:large_biomes': 'Große Biome',
      'minecraft:amplified': 'Zerklüftet',
      'minecraft:single_biome_surface': 'Ein einziges Biom',
    },
    'region-file-compression': { deflate: 'Deflate (Standard)', lz4: 'LZ4 (schneller, größer)', none: 'Keine' },
  };

  let changed = $derived(Object.keys(values).filter((k) => values[k] !== saved[k]));
  let dirty = $derived(changed.length > 0);

  function val(p: PropInfo): string {
    return values[p.key] ?? p.default;
  }
  function setVal(k: string, v: string) {
    values[k] = v;
  }

  let groups = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    const match = (p: PropInfo) => !f || p.key.includes(f) || p.label.toLowerCase().includes(f) || (p.description ?? '').toLowerCase().includes(f);
    const m = new Map<string, PropInfo[]>();
    for (const p of known) {
      if (!match(p)) continue;
      if (!m.has(p.group)) m.set(p.group, []);
      m.get(p.group)!.push(p);
    }
    return [...m.entries()];
  });

  let others = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    const knownKeys = new Set(known.map((p) => p.key));
    return Object.keys(values)
      .filter((k) => !knownKeys.has(k) && (!f || k.includes(f)))
      .sort();
  });

  async function save() {
    saving = true;
    try {
      const diff: Record<string, string> = {};
      for (const k of changed) diff[k] = values[k];
      const r = await api.put<{ values: Record<string, string>; restartRequired: boolean }>(url, { values: diff });
      saved = { ...r.values };
      values = { ...r.values };
      restartHint = r.restartRequired;
      toastOk('server.properties gespeichert');
    } catch (e) {
      toastError(e);
    } finally {
      saving = false;
    }
  }

  async function addKey() {
    const k = await promptDialog({ title: 'Eintrag hinzufügen', label: 'Schlüssel', placeholder: 'z. B. spawn-monsters', confirm: 'Hinzufügen' });
    const key = k?.trim();
    if (!key) return;
    if (/[=:\s]/.test(key)) return toastError('Der Schlüssel darf keine Leerzeichen, = oder : enthalten');
    if (!(key in values)) values[key] = '';
  }

  async function restart() {
    try {
      await api.post(`/api/servers/${server.id}/power`, { action: 'restart' });
      restartHint = false;
    } catch (e) {
      toastError(e);
    }
  }
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>server.properties</h2>
      <p>Die Grundeinstellungen von Minecraft. Änderungen gelten nach dem nächsten Start des Servers.</p>
    </div>
    <div class="search">
      <Search size={15} />
      <input class="input" placeholder="Einstellung suchen" bind:value={filter} aria-label="Einstellungen filtern" />
    </div>
  </div>

  {#if restartHint && status.state !== 'offline'}
    <div class="notice warn">
      <TriangleAlert />
      <span style="flex:1">Gespeichert. Der laufende Server übernimmt die Änderungen erst nach einem Neustart.</span>
      {#if can(server, 'power.stop')}<button class="btn sm" onclick={restart}><RotateCw /> Neu starten</button>{/if}
    </div>
  {/if}

  {#if loaded}
    {#each groups as [group, items] (group)}
      <section class="panel">
        <div class="panel-head"><h3>{group}</h3></div>
        <div class="props">
          {#each items as p (p.key)}
            <div class="prop" class:changed={values[p.key] !== undefined && values[p.key] !== saved[p.key]}>
              {#if p.type === 'bool' && !p.managed}
                <Toggle
                  checked={val(p) === 'true'}
                  label={p.label}
                  hint={p.description ?? ''}
                  disabled={!canEdit}
                  onchange={(v) => setVal(p.key, v ? 'true' : 'false')}
                />
                <code class="key">{p.key}</code>
              {:else}
                <div class="lbl">
                  <label for="p-{p.key}">{p.label}</label>
                  {#if p.description}<span class="hint">{p.description}</span>{/if}
                  <code class="key">{p.key}</code>
                </div>
                <div class="ctl">
                  {#if p.managed}
                    <span class="managed small muted" title="Wird vor jedem Start vom Panel gesetzt"><Lock size={13} /> {p.key === 'rcon.password' ? '••••••' : val(p)} <span class="faint">vom Panel verwaltet</span></span>
                  {:else if p.type === 'enum'}
                    <select id="p-{p.key}" class="select" value={val(p)} disabled={!canEdit} onchange={(e) => setVal(p.key, e.currentTarget.value)}>
                      {#each p.options ?? [] as o (o)}<option value={o}>{optionLabels[p.key]?.[o] ?? o}</option>{/each}
                      {#if !(p.options ?? []).includes(val(p))}<option value={val(p)}>{val(p)}</option>{/if}
                    </select>
                  {:else if p.type === 'int'}
                    <input id="p-{p.key}" class="input num" type="number" min={p.min} max={p.max || undefined} value={val(p)} disabled={!canEdit} oninput={(e) => setVal(p.key, e.currentTarget.value)} />
                  {:else}
                    <input id="p-{p.key}" class="input" class:mono={p.key !== 'motd'} value={val(p)} disabled={!canEdit} oninput={(e) => setVal(p.key, e.currentTarget.value)} spellcheck="false" />
                    {#if p.key === 'motd' && val(p)}
                      <div class="motd" aria-label="Vorschau">{@html mcToHtml(val(p).replace(/\\n/g, '\n').replace(/\\u00a7/gi, '§'))}</div>
                    {/if}
                  {/if}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      </section>
    {/each}

    {#if others.length || (!filter && canEdit)}
      <section class="panel">
        <div class="panel-head">
          <h3>Weitere Einträge</h3>
          <span class="spacer"></span>
          {#if canEdit}<button class="btn sm" onclick={addKey}><Plus /> Eintrag</button>{/if}
        </div>
        {#if others.length}
          <div class="props">
            {#each others as k (k)}
              <div class="prop raw" class:changed={values[k] !== saved[k]}>
                <label class="mono small" for="o-{k}">{k}</label>
                <input id="o-{k}" class="input mono" value={values[k]} disabled={!canEdit} oninput={(e) => setVal(k, e.currentTarget.value)} spellcheck="false" />
              </div>
            {/each}
          </div>
        {:else}
          <p class="panel-body small faint">Alle Einträge sind oben aufgeführt.</p>
        {/if}
      </section>
    {/if}

    {#if dirty}
      <div class="savebar">
        <span class="small"><strong>{changed.length}</strong> {changed.length === 1 ? 'Änderung' : 'Änderungen'} nicht gespeichert</span>
        <span class="spacer"></span>
        <button class="btn" onclick={() => (values = { ...saved })}>Verwerfen</button>
        <button class="btn primary" onclick={save} disabled={saving}>Speichern</button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .search {
    position: relative;
    width: 240px;
  }
  .search :global(svg) {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-3);
  }
  .search .input {
    padding-left: 32px;
  }
  .props {
    display: flex;
    flex-direction: column;
  }
  .prop {
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(200px, 320px);
    gap: 8px 24px;
    align-items: center;
    padding: 12px 18px;
    border-bottom: 1px solid var(--line);
  }
  .prop:last-child {
    border-bottom: none;
  }
  .prop:has(> :global(.toggle)) {
    grid-template-columns: 1fr;
    gap: 2px;
  }
  .prop.changed::before {
    content: '';
    position: absolute;
    left: 0;
    top: 10px;
    bottom: 10px;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--warn);
  }
  .lbl {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .lbl label {
    font-size: 14px;
    font-weight: 560;
  }
  .key {
    background: none;
    padding: 0;
    font-size: 11.5px;
    color: var(--text-3);
  }
  .ctl {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .managed {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .motd {
    padding: 8px 10px;
    border-radius: var(--r-s);
    background: #1d1f23;
    color: #aaaaaa;
    font-family: var(--font-mono);
    font-size: 13px;
    line-height: 1.4;
    white-space: pre-wrap;
  }
  .raw {
    grid-template-columns: minmax(0, 1fr) minmax(200px, 320px);
  }
  @media (max-width: 760px) {
    .prop,
    .raw {
      grid-template-columns: 1fr;
    }
    .search {
      width: 100%;
    }
  }
</style>
