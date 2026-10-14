<script lang="ts">
  import { onMount } from 'svelte';
  import { Bell, Plus, Send, Pencil, Trash2, CircleCheck, CircleX } from '@lucide/svelte';
  import Modal from './Modal.svelte';
  import Toggle from './Toggle.svelte';
  import Menu from './Menu.svelte';
  import { api } from '../lib/api';
  import { toastError, toastOk } from '../lib/toast.svelte';
  import { confirmDialog } from '../lib/dialogs.svelte';
  import type { Webhook } from '../lib/types';

  let { base, intro }: { base: string; intro: string } = $props();

  let hooks = $state<Webhook[]>([]);
  let events = $state<{ key: string; label: string }[]>([]);
  let loaded = $state(false);

  async function load() {
    try {
      const r = await api.get<{ webhooks: Webhook[]; events: { key: string; label: string }[] }>(base);
      hooks = r.webhooks ?? [];
      events = r.events;
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }
  onMount(load);

  let open = $state(false);
  let draft = $state<{ id: number; name: string; kind: 'discord' | 'generic'; url: string; events: string[]; enabled: boolean }>({
    id: 0,
    name: '',
    kind: 'discord',
    url: '',
    events: [],
    enabled: true,
  });
  let saving = $state(false);

  function edit(h: Webhook | null) {
    draft = h
      ? { id: h.id, name: h.name, kind: h.kind, url: h.url, events: [...(h.events ?? [])], enabled: h.enabled }
      : { id: 0, name: 'Discord', kind: 'discord', url: '', events: ['server.started', 'server.stopped', 'server.crashed', 'backup.failed'], enabled: true };
    open = true;
  }

  $effect(() => {
    if (draft.url.includes('discord.com/api/webhooks') || draft.url.includes('discordapp.com/api/webhooks')) draft.kind = 'discord';
  });

  async function save(e: Event) {
    e.preventDefault();
    saving = true;
    try {
      if (draft.id) await api.put(`${base}/${draft.id}`, draft);
      else await api.post(base, draft);
      open = false;
      toastOk('Benachrichtigung gespeichert');
      load();
    } catch (err) {
      toastError(err);
    } finally {
      saving = false;
    }
  }

  async function test(h: Webhook) {
    try {
      await api.post(`${base}/${h.id}/test`);
      toastOk('Testnachricht gesendet');
      load();
    } catch (e) {
      toastError(e);
      load();
    }
  }

  async function remove(h: Webhook) {
    if (!(await confirmDialog({ title: `„${h.name}“ löschen?`, confirm: 'Löschen', danger: true }))) return;
    try {
      await api.del(`${base}/${h.id}`);
      load();
    } catch (e) {
      toastError(e);
    }
  }

  async function setEnabled(h: Webhook, enabled: boolean) {
    try {
      await api.put(`${base}/${h.id}`, { ...h, enabled });
      load();
    } catch (e) {
      toastError(e);
      load();
    }
  }

  function toggleEvent(k: string, on: boolean) {
    draft.events = on ? [...new Set([...draft.events, k])] : draft.events.filter((x) => x !== k);
  }

  function host(u: string) {
    try {
      return new URL(u).host;
    } catch {
      return u;
    }
  }
  const label = (k: string) => events.find((e) => e.key === k)?.label ?? k;
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Benachrichtigungen</h2>
      <p>{intro}</p>
    </div>
    <button class="btn primary" onclick={() => edit(null)}><Plus /> Webhook hinzufügen</button>
  </div>

  <div class="panel">
    {#if loaded && hooks.length === 0}
      <div class="empty">
        <Bell />
        <h3>Noch keine Benachrichtigungen</h3>
        <p>Füge einen Discord-Webhook hinzu, um Starts, Abstürze, Backups oder Spielerereignisse in einen Kanal zu posten.</p>
      </div>
    {:else}
      <ul class="hooks">
        {#each hooks as h (h.id)}
          <li class:off={!h.enabled}>
            <div class="hi">
              <div class="hn">
                <strong>{h.name}</strong>
                <span class="tag">{h.kind === 'discord' ? 'Discord' : 'JSON'}</span>
                {#if h.lastStatus}
                  {#if h.lastStatus.startsWith('ok')}
                    <span class="st ok"><CircleCheck size={13} /> zugestellt {h.lastStatus.slice(3)}</span>
                  {:else}
                    <span class="st bad" title={h.lastStatus}><CircleX size={13} /> {h.lastStatus}</span>
                  {/if}
                {/if}
              </div>
              <span class="small muted mono ellipsis">{host(h.url)}</span>
              <span class="tiny faint">{(h.events ?? []).map(label).join(', ') || 'Keine Ereignisse gewählt'}</span>
            </div>
            <Toggle checked={h.enabled} label="" ariaLabel="{h.name} aktiv" onchange={(v) => setEnabled(h, v)} />
            <button class="btn sm" onclick={() => test(h)}><Send /> Test</button>
            <Menu
              items={[
                { label: 'Bearbeiten', icon: Pencil, onclick: () => edit(h) },
                { divider: true, label: '' },
                { label: 'Löschen', icon: Trash2, danger: true, onclick: () => remove(h) },
              ]}
            />
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>

<Modal bind:open title={draft.id ? 'Webhook bearbeiten' : 'Webhook hinzufügen'} size="l">
  <form id="hook-form" class="stack" onsubmit={save}>
    <div class="form-grid">
      <div class="field">
        <label for="hn">Name</label>
        <input id="hn" class="input" bind:value={draft.name} maxlength="60" required />
      </div>
      <div class="field">
        <label for="hk">Format</label>
        <select id="hk" class="select" bind:value={draft.kind}>
          <option value="discord">Discord (Embeds)</option>
          <option value="generic">Allgemein (JSON)</option>
        </select>
      </div>
    </div>
    <div class="field">
      <label for="hu">Webhook-URL</label>
      <input id="hu" class="input mono" type="url" bind:value={draft.url} required placeholder="https://discord.com/api/webhooks/…" />
      <p class="hint">
        {#if draft.kind === 'discord'}In Discord: Kanal bearbeiten, Integrationen, Webhooks, „Neuer Webhook“, URL kopieren.{:else}Das Panel sendet einen POST mit JSON (event, server, title, text, player, time).{/if}
      </p>
    </div>
    <div class="field">
      <span class="label">Ereignisse</span>
      <div class="evs">
        {#each events as ev (ev.key)}
          <label class="check"><input type="checkbox" checked={draft.events.includes(ev.key)} onchange={(e) => toggleEvent(ev.key, e.currentTarget.checked)} /> {ev.label}</label>
        {/each}
      </div>
    </div>
    <Toggle bind:checked={draft.enabled} label="Aktiv" />
  </form>
  {#snippet footer()}
    <button class="btn" onclick={() => (open = false)}>Abbrechen</button>
    <button class="btn primary" type="submit" form="hook-form" disabled={saving}>Speichern</button>
  {/snippet}
</Modal>

<style>
  .hooks {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .hooks li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 13px 16px;
    border-bottom: 1px solid var(--line);
  }
  .hooks li:last-child {
    border-bottom: none;
  }
  .hooks li.off .hi {
    opacity: 0.6;
  }
  .hi {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .hn {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .st {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
  }
  .st.ok {
    color: var(--accent);
  }
  .st.bad {
    color: var(--danger);
  }
  .evs {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 8px 16px;
  }
</style>
