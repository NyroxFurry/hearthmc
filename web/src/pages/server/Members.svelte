<script lang="ts">
  import { onMount } from 'svelte';
  import { UserPlus, Link2, Pencil, Trash2, Crown, KeyRound } from '@lucide/svelte';
  import Modal from '../../components/Modal.svelte';
  import PermissionMatrix from '../../components/PermissionMatrix.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import Menu from '../../components/Menu.svelte';
  import { api, qs } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog } from '../../lib/dialogs.svelte';
  import { date, dateTime } from '../../lib/format';
  import type { Member, PermInfo, Preset, Server } from '../../lib/types';

  let { server }: { server: Server } = $props();

  const url = $derived(`/api/servers/${server.id}/members`);
  let members = $state<Member[]>([]);
  let perms = $state<PermInfo[]>([]);
  let presets = $state<Preset[]>([]);
  let loaded = $state(false);

  async function load() {
    try {
      const r = await api.get<{ members: Member[]; permissions: PermInfo[]; presets: Preset[] }>(url);
      members = r.members;
      perms = r.permissions;
      presets = r.presets;
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }

  onMount(load);

  function summary(list: string[]): string {
    const set = new Set(list);
    const p = presets.find((x) => x.permissions.length === set.size && x.permissions.every((k) => set.has(k)));
    if (p) return p.label;
    if (list.length === perms.length) return 'Alle Rechte';
    if (list.length === 0) return 'Keine Rechte';
    return `${list.length} von ${perms.length} Rechten`;
  }

  // ---- edit ----
  let editing = $state<Member | null>(null);
  let editOpen = $state(false);
  let editPerms = $state<string[]>([]);
  let saving = $state(false);

  function edit(m: Member) {
    editing = m;
    editPerms = [...m.permissions];
    editOpen = true;
  }

  async function saveEdit() {
    if (!editing) return;
    saving = true;
    try {
      await api.put(`${url}/${editing.userId}`, { permissions: editPerms });
      toastOk(`Rechte von ${editing.username} gespeichert`);
      editOpen = false;
      load();
    } catch (e) {
      toastError(e);
    } finally {
      saving = false;
    }
  }

  async function remove(m: Member) {
    const self = m.userId === session.user?.id;
    const ok = await confirmDialog({
      title: self ? 'Server verlassen?' : `${m.username} entfernen?`,
      text: self ? 'Du verlierst den Zugriff auf diesen Server.' : `${m.username} hat danach keinen Zugriff mehr auf diesen Server.`,
      confirm: self ? 'Verlassen' : 'Entfernen',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.del(`${url}/${m.userId}`);
      toastOk(self ? 'Du hast den Server verlassen' : `${m.username} entfernt`);
      if (self) location.href = '/';
      else load();
    } catch (e) {
      toastError(e);
    }
  }

  // ---- add ----
  let addOpen = $state(false);
  let query = $state('');
  let suggestions = $state<{ id: number; username: string }[]>([]);
  let picked = $state<{ id: number; username: string } | null>(null);
  let addPerms = $state<string[]>([]);

  function openAdd() {
    query = '';
    picked = null;
    suggestions = [];
    addPerms = [...(presets.find((p) => p.id === 'viewer')?.permissions ?? [])].filter((k) => server.permissions.includes(k));
    addOpen = true;
  }

  let searchTimer: ReturnType<typeof setTimeout>;
  $effect(() => {
    const q = query.trim();
    if (!addOpen) return;
    clearTimeout(searchTimer);
    if (q.length < 2 || picked?.username === q) {
      suggestions = [];
      return;
    }
    searchTimer = setTimeout(async () => {
      try {
        const list = await api.get<{ id: number; username: string }[]>(`/api/users/search${qs({ q })}`);
        suggestions = list.filter((u) => u.id !== server.ownerId && !members.some((m) => m.userId === u.id));
      } catch {}
    }, 200);
  });

  async function add() {
    const username = picked?.username ?? query.trim();
    if (!username) return;
    saving = true;
    try {
      await api.post(url, picked ? { userId: picked.id, permissions: addPerms } : { username, permissions: addPerms });
      toastOk(`${username} hat jetzt Zugriff`);
      addOpen = false;
      load();
    } catch (e) {
      toastError(e);
    } finally {
      saving = false;
    }
  }

  // ---- invite link ----
  let inviteOpen = $state(false);
  let invPerms = $state<string[]>([]);
  let invNote = $state('');
  let invDays = $state(7);
  let invLink = $state('');
  let invExpires = $state(0);

  function openInvite() {
    invPerms = [...(presets.find((p) => p.id === 'viewer')?.permissions ?? [])].filter((k) => server.permissions.includes(k));
    invNote = '';
    invDays = 7;
    invLink = '';
    inviteOpen = true;
  }

  async function createInvite() {
    saving = true;
    try {
      const r = await api.post<{ link: string; expiresAt: number }>(`/api/servers/${server.id}/invites`, { permissions: invPerms, note: invNote, days: invDays });
      invLink = r.link;
      invExpires = r.expiresAt;
    } catch (e) {
      toastError(e);
    } finally {
      saving = false;
    }
  }
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Zugriff</h2>
      <p>Wer diesen Server sehen und bedienen darf. Rechte gelten nur für diesen Server; du kannst nur Rechte vergeben, die du selbst hast.</p>
    </div>
    <button class="btn" onclick={openInvite} disabled={!loaded}><Link2 /> Einladungslink</button>
    <button class="btn primary" onclick={openAdd} disabled={!loaded}><UserPlus /> Person hinzufügen</button>
  </div>

  <div class="panel">
    <ul class="people">
      <li>
        <span class="av owner">{(server.ownerName || '?').slice(0, 1).toUpperCase()}</span>
        <div class="pi">
          <strong>{server.ownerName || 'Unbekannt'}</strong>
          <span class="small muted">Besitzer, hat immer alle Rechte</span>
        </div>
        <Crown size={16} class="crown" />
      </li>
      {#each members as m (m.userId)}
        <li>
          <span class="av">{m.username.slice(0, 1).toUpperCase()}</span>
          <div class="pi">
            <strong>{m.username}{#if m.userId === session.user?.id}{' '}<span class="tag">Du</span>{/if}</strong>
            <span class="small muted" title={m.permissions.map((k) => perms.find((p) => p.key === k)?.label ?? k).join(', ')}>
              {summary(m.permissions)}, seit {date(m.createdAt)}
            </span>
          </div>
          <button class="btn sm" onclick={() => edit(m)}><KeyRound /> Rechte</button>
          <Menu
            items={[
              { label: 'Rechte bearbeiten', icon: Pencil, onclick: () => edit(m) },
              { divider: true, label: '' },
              { label: m.userId === session.user?.id ? 'Server verlassen' : 'Entfernen', icon: Trash2, danger: true, onclick: () => remove(m) },
            ]}
          />
        </li>
      {/each}
    </ul>
    {#if loaded && members.length === 0}
      <p class="panel-body small muted none">Außer dem Besitzer hat niemand Zugriff. Füge Personen hinzu, die schon ein Konto haben, oder verschicke einen Einladungslink.</p>
    {/if}
  </div>
</div>

<Modal bind:open={editOpen} title={editing ? `Rechte von ${editing.username}` : 'Rechte'} size="l">
  <PermissionMatrix {perms} {presets} bind:value={editPerms} allowed={server.permissions} />
  {#snippet footer()}
    <button class="btn" onclick={() => (editOpen = false)}>Abbrechen</button>
    <button class="btn primary" onclick={saveEdit} disabled={saving}>Speichern</button>
  {/snippet}
</Modal>

<Modal bind:open={addOpen} title="Person hinzufügen" size="l">
  <div class="stack">
    <div class="field user-field">
      <label for="uq">Benutzername</label>
      <input
        id="uq"
        class="input"
        bind:value={query}
        oninput={() => (picked = null)}
        placeholder="Mindestens zwei Buchstaben"
        autocomplete="off"
      />
      {#if suggestions.length && !picked}
        <ul class="suggest" role="listbox">
          {#each suggestions as u (u.id)}
            <li>
              <button
                type="button"
                role="option"
                aria-selected="false"
                onclick={() => {
                  picked = u;
                  query = u.username;
                  suggestions = [];
                }}><span class="av sm">{u.username.slice(0, 1).toUpperCase()}</span>{u.username}</button
              >
            </li>
          {/each}
        </ul>
      {/if}
      <p class="hint">Die Person braucht bereits ein Konto im Panel. Sonst verschicke einen Einladungslink.</p>
    </div>
    <div class="field">
      <span class="label">Rechte auf diesem Server</span>
      <PermissionMatrix {perms} {presets} bind:value={addPerms} allowed={server.permissions} />
    </div>
  </div>
  {#snippet footer()}
    <button class="btn" onclick={() => (addOpen = false)}>Abbrechen</button>
    <button class="btn primary" onclick={add} disabled={saving || !query.trim()}>Hinzufügen</button>
  {/snippet}
</Modal>

<Modal bind:open={inviteOpen} title="Einladungslink erstellen" size="l">
  {#if invLink}
    <div class="stack">
      <p>Schicke diesen Link an die Person. Sie legt damit ein Konto an und erhält sofort Zugriff auf <strong>{server.name}</strong>. Der Link funktioniert einmal und läuft am {dateTime(invExpires)} ab.</p>
      <div class="linkbox">
        <code class="ellipsis">{invLink}</code>
        <CopyButton text={invLink} label="Kopieren" cls="btn sm primary" />
      </div>
    </div>
  {:else}
    <div class="stack">
      <div class="form-grid">
        <div class="field">
          <label for="inote">Notiz <span class="faint">(optional)</span></label>
          <input id="inote" class="input" bind:value={invNote} placeholder="z. B. Für unser Mod-Team" maxlength="200" />
        </div>
        <div class="field">
          <label for="idays">Gültig für</label>
          <select id="idays" class="select" bind:value={invDays}>
            <option value={1}>1 Tag</option>
            <option value={7}>7 Tage</option>
            <option value={30}>30 Tage</option>
          </select>
        </div>
      </div>
      <div class="field">
        <span class="label">Rechte auf diesem Server</span>
        <PermissionMatrix {perms} {presets} bind:value={invPerms} allowed={server.permissions} />
      </div>
    </div>
  {/if}
  {#snippet footer()}
    {#if invLink}
      <button class="btn primary" onclick={() => (inviteOpen = false)}>Fertig</button>
    {:else}
      <button class="btn" onclick={() => (inviteOpen = false)}>Abbrechen</button>
      <button class="btn primary" onclick={createInvite} disabled={saving}>Link erstellen</button>
    {/if}
  {/snippet}
</Modal>

<style>
  .people {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .people li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--line);
  }
  .people li:last-child {
    border-bottom: none;
  }
  .av {
    width: 34px;
    height: 34px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: var(--surface-3);
    color: var(--text);
    font-weight: 700;
    flex: none;
  }
  .av.owner {
    background: var(--copper-soft);
    color: var(--copper);
  }
  .av.sm {
    width: 24px;
    height: 24px;
    border-radius: 6px;
    font-size: 12px;
  }
  .pi {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .people :global(.crown) {
    color: var(--copper);
    margin-right: 8px;
  }
  .none {
    border-top: 1px solid var(--line);
  }
  .user-field {
    position: relative;
  }
  .suggest {
    list-style: none;
    margin: 0;
    padding: 4px;
    position: absolute;
    top: 64px;
    left: 0;
    right: 0;
    z-index: 5;
    background: var(--surface);
    border: 1px solid var(--line-2);
    border-radius: var(--r-m);
    box-shadow: var(--pop-shadow);
  }
  .suggest button {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 7px 9px;
    border: none;
    background: none;
    border-radius: 6px;
    color: var(--text);
    font: inherit;
    font-size: 14px;
    text-align: left;
    cursor: pointer;
  }
  .suggest button:hover {
    background: var(--surface-2);
  }
  .linkbox {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 8px 8px 12px;
    border: 1px solid var(--line-2);
    border-radius: var(--r-m);
    background: var(--bg);
  }
  .linkbox code {
    flex: 1;
    background: none;
    padding: 0;
  }
</style>
