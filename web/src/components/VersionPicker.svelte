<script lang="ts">
  import { RefreshCw, TriangleAlert } from '@lucide/svelte';
  import Toggle from './Toggle.svelte';
  import { api, qs } from '../lib/api';
  import { session } from '../lib/session.svelte';

  let {
    flavor = $bindable('paper'),
    version = $bindable(''),
    build = $bindable(''),
    compact = false,
  }: { flavor: string; version: string; build: string; compact?: boolean } = $props();

  let snapshots = $state(false);
  let versions = $state<{ id: string; type: string; stable: boolean; java?: number }[]>([]);
  let builds = $state<{ id: string; stable: boolean; channel?: string }[]>([]);
  let loading = $state(false);
  let error = $state('');
  let manual = $state(false);

  const flavors = $derived(session.meta?.flavors ?? []);
  const hasBuilds = $derived(['paper', 'purpur', 'fabric', 'forge', 'neoforge'].includes(flavor));

  async function loadVersions() {
    loading = true;
    error = '';
    try {
      versions = await api.get(`/api/meta/mc/versions${qs({ flavor: flavor === 'custom' ? 'vanilla' : flavor, snapshots: snapshots ? 1 : '' })}`);
      if (!versions.some((v) => v.id === version)) version = (versions.find((v) => v.stable) ?? versions[0])?.id ?? '';
      manual = false;
    } catch (e: any) {
      versions = [];
      error = e.message;
      manual = true;
    } finally {
      loading = false;
    }
  }

  async function loadBuilds() {
    builds = [];
    if (!version || !hasBuilds) return;
    try {
      builds = await api.get(`/api/meta/mc/builds${qs({ flavor, version })}`);
      if (build && !builds.some((b) => b.id === build)) build = '';
    } catch {}
  }

  $effect(() => {
    flavor;
    snapshots;
    loadVersions();
  });

  $effect(() => {
    version;
    flavor;
    loadBuilds();
  });

  const kindLabel: Record<string, string> = { plugins: 'Plugins', mods: 'Mods', vanilla: 'Ohne Erweiterungen', custom: 'Eigene Datei' };
</script>

<div class="picker stack">
  <div class="flavors" class:compact role="radiogroup" aria-label="Server-Art">
    {#each flavors as f (f.id)}
      <label class="fl" class:on={flavor === f.id}>
        <input type="radio" name="vp-flavor" value={f.id} bind:group={flavor} />
        <span class="fn">{f.name}</span>
        <span class="fk tiny faint">{kindLabel[f.kind]}</span>
      </label>
    {/each}
  </div>

  {#if error}
    <div class="notice warn">
      <TriangleAlert />
      <span style="flex:1">Versionsliste nicht erreichbar ({error}). Du kannst die Version von Hand eintragen.</span>
      <button type="button" class="btn sm" onclick={loadVersions}><RefreshCw /> Erneut</button>
    </div>
  {/if}

  <div class="form-grid">
    <div class="field">
      <label for="vp-ver">Minecraft-Version</label>
      {#if manual}
        <input id="vp-ver" class="input" bind:value={version} placeholder="z. B. 1.21.10 oder 26.3" />
      {:else}
        <select id="vp-ver" class="select" bind:value={version} disabled={loading}>
          {#each versions as v (v.id)}
            <option value={v.id}>{v.id}{v.type === 'snapshot' ? ' (Vorschau)' : !v.stable ? ' (experimentell)' : ''}</option>
          {/each}
        </select>
      {/if}
    </div>
    {#if hasBuilds && builds.length}
      <div class="field">
        <label for="vp-build">{flavor === 'paper' || flavor === 'purpur' ? 'Build' : 'Loader-Version'}</label>
        <select id="vp-build" class="select" bind:value={build}>
          <option value="">Neueste stabile</option>
          {#each builds as b (b.id)}
            <option value={b.id}>{b.id}{b.stable ? '' : ' (experimentell)'}</option>
          {/each}
        </select>
      </div>
    {/if}
  </div>
  <Toggle bind:checked={snapshots} label="Vorabversionen anzeigen" />
</div>

<style>
  .flavors {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
    gap: 8px;
  }
  .fl {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 9px 12px;
    border-radius: var(--r-m);
    border: 1px solid var(--line-2);
    cursor: pointer;
  }
  .fl:hover {
    background: var(--surface-2);
  }
  .fl.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .fl input {
    position: absolute;
    opacity: 0;
  }
  .fl:has(input:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .fn {
    font-weight: 650;
    font-size: 14px;
  }
</style>
