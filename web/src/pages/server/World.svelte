<script lang="ts">
  import { onMount } from 'svelte';
  import { Globe, Download, RotateCcw, TriangleAlert, FolderOpen } from '@lucide/svelte';
  import { api } from '../../lib/api';
  import { can } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog } from '../../lib/dialogs.svelte';
  import { bytes, num } from '../../lib/format';
  import { dimLabels } from '../../lib/mc';
  import type { Server, Status } from '../../lib/types';

  let { server, status }: { server: Server; status: Status } = $props();

  const mcUrl = $derived(`/api/servers/${server.id}/mc`);
  const canEdit = $derived(can(server, 'settings.edit'));
  const offline = $derived(status.state === 'offline');

  interface Dim {
    id: string;
    key: string;
    label: string;
    regions: number;
    size: number;
  }
  let world = $state<{ name: string; layout: string; exists: boolean } | null>(null);
  let dims = $state<Dim[]>([]);
  let size = $state(0);
  let properties = $state<Record<string, string>>({});
  let loaded = $state(false);

  let seed = $state('');
  let confirmName = $state('');
  let resetting = $state(false);

  async function load() {
    try {
      const r = await api.get<{ world: { name: string; layout: string; exists: boolean }; dimensions: Dim[] | null; size: number }>(`${mcUrl}/worlds`);
      world = r.world;
      dims = r.dimensions ?? [];
      size = r.size;
      const p = await api.get<{ values: Record<string, string> }>(`${mcUrl}/properties`);
      properties = p.values;
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }

  onMount(load);

  async function resetDim(d: Dim) {
    const ok = await confirmDialog({
      title: `${dimLabels[d.key] ?? d.label} zurücksetzen?`,
      text: 'Alle Regionen dieser Dimension werden gelöscht und beim nächsten Betreten neu generiert. Bauten dort gehen verloren.',
      confirm: 'Zurücksetzen',
      danger: true,
    });
    if (!ok || !world) return;
    try {
      await api.post(`${mcUrl}/worlds/reset`, { dimension: d.key, confirm: world.name });
      toastOk(`${dimLabels[d.key] ?? d.label} wurde zurückgesetzt`);
      load();
    } catch (e) {
      toastError(e);
    }
  }

  async function resetAll(e: Event) {
    e.preventDefault();
    if (!world) return;
    resetting = true;
    try {
      await api.post(`${mcUrl}/worlds/reset`, { dimension: '', seed, confirm: confirmName });
      toastOk('Die Welt wurde gelöscht. Beim nächsten Start entsteht eine neue.');
      confirmName = '';
      seed = '';
      load();
    } catch (err) {
      toastError(err);
    } finally {
      resetting = false;
    }
  }

  let folders = $derived.by(() => {
    if (!world) return [];
    const f = [world.name];
    if (world.layout === 'bukkit') {
      if (dims.some((d) => d.key === 'the_nether')) f.push(world.name + '_nether');
      if (dims.some((d) => d.key === 'the_end')) f.push(world.name + '_the_end');
    }
    return f;
  });

  const layoutLabels: Record<string, string> = {
    modern: 'Minecraft 26.1+ (Dimensionen im Weltordner)',
    vanilla: 'Vanilla (Nether und End im Weltordner)',
    bukkit: 'Paper/Spigot (eigene Ordner für Nether und End)',
  };
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Welt</h2>
      {#if world}
        <p>
          Weltordner <code>{world.name}</code>{#if world.exists}, zusammen {bytes(size)}{/if}.
          {#if properties['level-seed']}Seed <code>{properties['level-seed']}</code>.{/if}
        </p>
      {/if}
    </div>
    {#if world?.exists && can(server, 'files.read')}
      <a class="btn" href="/api/servers/{server.id}/files/download?{folders.map((f) => 'paths=' + encodeURIComponent(f)).join('&')}" download data-external>
        <Download /> Welt herunterladen
      </a>
    {/if}
  </div>

  {#if loaded && world && !world.exists}
    <div class="panel">
      <div class="empty">
        <Globe />
        <h3>Noch keine Welt</h3>
        <p>Minecraft erzeugt die Welt beim ersten Start. Willst du eine vorhandene Welt verwenden, lade den Ordner im Dateimanager hoch und trage seinen Namen in server.properties bei „Weltordner“ ein.</p>
        <a class="btn" href="/servers/{server.id}/files"><FolderOpen /> Zum Dateimanager</a>
      </div>
    </div>
  {:else if world}
    <div class="panel">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Dimension</th><th class="r">Regionen</th><th class="r">Größe</th>{#if canEdit}<th></th>{/if}</tr>
          </thead>
          <tbody>
            {#each dims as d (d.key)}
              <tr>
                <td><strong>{dimLabels[d.key] ?? d.label}</strong> <span class="faint small mono">{d.id}</span></td>
                <td class="r num">{num(d.regions)}</td>
                <td class="r num">{bytes(d.size)}</td>
                {#if canEdit}
                  <td class="r">
                    {#if d.id !== 'minecraft:overworld'}
                      <button class="btn sm" disabled={!offline} onclick={() => resetDim(d)} title={offline ? '' : 'Nur bei gestopptem Server'}><RotateCcw /> Zurücksetzen</button>
                    {/if}
                  </td>
                {/if}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <p class="panel-body tiny faint layout">Ordnerstruktur: {layoutLabels[world.layout] ?? world.layout}</p>
    </div>

    {#if canEdit}
      <section class="panel danger-zone">
        <div class="panel-head"><TriangleAlert size={16} /><h3>Neue Welt erzeugen</h3></div>
        <form class="panel-body stack" onsubmit={resetAll}>
          <p class="small muted">
            Löscht die komplette Welt mit allen Dimensionen und die gerenderte Karte. Beim nächsten Start erzeugt Minecraft eine neue Welt.
            Spielerstatistiken im Panel bleiben erhalten. Lege vorher ein Backup an, wenn du die alte Welt behalten willst.
          </p>
          {#if !offline}
            <div class="notice warn"><TriangleAlert /><span>Stoppe den Server, um die Welt zurückzusetzen.</span></div>
          {/if}
          <div class="form-grid">
            <div class="field">
              <label for="seed">Seed für die neue Welt <span class="faint">(optional)</span></label>
              <input id="seed" class="input mono" bind:value={seed} placeholder="leer = zufällig" disabled={!offline} />
            </div>
            <div class="field">
              <label for="cn">Zur Bestätigung „{world.name}“ eingeben</label>
              <input id="cn" class="input mono" bind:value={confirmName} disabled={!offline} autocomplete="off" />
            </div>
          </div>
          <div>
            <button class="btn danger" disabled={!offline || confirmName !== world.name || resetting}><RotateCcw /> Welt löschen und neu erzeugen</button>
          </div>
        </form>
      </section>
    {/if}
  {/if}
</div>

<style>
  .layout {
    border-top: 1px solid var(--line);
    padding-top: 10px;
    padding-bottom: 12px;
  }
  .danger-zone {
    border-color: color-mix(in srgb, var(--danger) 35%, var(--line));
  }
  .danger-zone .panel-head :global(svg) {
    color: var(--danger);
  }
</style>
