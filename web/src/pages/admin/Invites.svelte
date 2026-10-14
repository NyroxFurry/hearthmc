<script lang="ts">
  import { onMount } from 'svelte';
  import { Plus, Ticket, Trash2 } from '@lucide/svelte';
  import Modal from '../../components/Modal.svelte';
  import Toggle from '../../components/Toggle.svelte';
  import PermissionMatrix from '../../components/PermissionMatrix.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import { api } from '../../lib/api';
  import { session, servers } from '../../lib/session.svelte';
  import { toastError } from '../../lib/toast.svelte';
  import { ago, dateTime } from '../../lib/format';

  interface Invite {
    id: number;
    note: string;
    isAdmin: boolean;
    canCreateServers: boolean;
    serverId: string;
    serverPerms: string[];
    createdAt: number;
    expiresAt: number;
    usedBy: number;
    usedAt: number;
  }

  let invites = $state<Invite[]>([]);
  let users = $state<Record<number, string>>({});

  async function load() {
    try {
      invites = await api.get('/api/admin/invites');
      const list = await api.get<{ id: number; username: string }[]>('/api/admin/users');
      users = Object.fromEntries(list.map((u) => [u.id, u.username]));
    } catch (e) {
      toastError(e);
    }
  }
  onMount(load);

  const now = () => Date.now() / 1000;

  async function remove(i: Invite) {
    try {
      await api.del(`/api/admin/invites/${i.id}`);
      load();
    } catch (e) {
      toastError(e);
    }
  }

  // ---- create ----
  let open = $state(false);
  let note = $state('');
  let days = $state(7);
  let isAdmin = $state(false);
  let canCreate = $state(false);
  let serverId = $state('');
  let perms = $state<string[]>([]);
  let link = $state('');
  let expires = $state(0);

  function start() {
    note = '';
    days = 7;
    isAdmin = false;
    canCreate = false;
    serverId = '';
    perms = session.meta?.presets.find((p) => p.id === 'viewer')?.permissions ?? [];
    link = '';
    open = true;
  }

  async function create(e: Event) {
    e.preventDefault();
    try {
      const r = await api.post('/api/admin/invites', {
        note,
        days: Number(days),
        isAdmin,
        canCreateServers: canCreate,
        serverId,
        serverPerms: serverId ? perms : [],
      });
      link = r.link;
      expires = r.invite.expiresAt;
      load();
    } catch (err) {
      toastError(err);
    }
  }

  function describe(i: Invite): string {
    const parts: string[] = [];
    if (i.isAdmin) parts.push('Administrator');
    else if (i.canCreateServers) parts.push('darf Server anlegen');
    if (i.serverId) parts.push(`Zugriff auf ${servers.get(i.serverId)?.name ?? 'gelöschten Server'}`);
    return parts.join(', ') || 'normales Konto';
  }
</script>

<div class="stack">
  <div class="bar">
    <p class="small muted">Mit einem Einladungslink legt sich jemand selbst ein Konto an. Jeder Link funktioniert einmal.</p>
    <span class="spacer"></span>
    <button class="btn primary" onclick={start}><Plus /> Einladung erstellen</button>
  </div>

  {#if invites.length === 0}
    <div class="panel empty"><Ticket /><h3>Keine Einladungen</h3><p>Erstelle einen Link, um Freunde oder Teammitglieder ins Panel zu holen.</p></div>
  {:else}
    <div class="panel list">
      {#each invites as i (i.id)}
        {@const used = i.usedAt > 0}
        {@const expired = !used && i.expiresAt < now()}
        <div class="inv" class:done={used || expired}>
          <div class="ii">
            <strong>{i.note || 'Einladung'}</strong>
            <span class="small muted">{describe(i)}</span>
          </div>
          <span class="small">
            {#if used}<span class="tag accent">eingelöst von {users[i.usedBy] ?? 'unbekannt'}</span>
            {:else if expired}<span class="tag">abgelaufen</span>
            {:else}<span class="faint" title={dateTime(i.expiresAt)}>läuft ab {ago(i.expiresAt)}</span>{/if}
          </span>
          <button class="btn ghost sm icon" onclick={() => remove(i)} aria-label="Einladung löschen" title="Löschen"><Trash2 /></button>
        </div>
      {/each}
    </div>
  {/if}
</div>

<Modal bind:open title="Einladung erstellen" size="l">
  {#if link}
    <div class="stack">
      <p>Schick diesen Link an die Person:</p>
      <div class="row">
        <input class="input mono" readonly value={link} onfocus={(e) => e.currentTarget.select()} />
        <CopyButton text={link} label="Kopieren" cls="btn primary" />
      </div>
      <p class="small muted">Gültig bis {dateTime(expires)}.</p>
    </div>
  {:else}
    <form id="inv-form" class="stack" onsubmit={create}>
      <div class="form-grid">
        <div class="field">
          <label for="i-n">Notiz</label>
          <input id="i-n" class="input" bind:value={note} placeholder="z. B. Für das Mod-Team" maxlength="200" />
        </div>
        <div class="field">
          <label for="i-d">Gültig für</label>
          <select id="i-d" class="select" bind:value={days}>
            <option value={1}>1 Tag</option>
            <option value={7}>7 Tage</option>
            <option value={30}>30 Tage</option>
            <option value={90}>90 Tage</option>
          </select>
        </div>
      </div>
      <Toggle bind:checked={isAdmin} label="Als Administrator" />
      {#if !isAdmin}
        <Toggle bind:checked={canCreate} label="Darf eigene Server anlegen" hint="Mit den Standard-Kontingenten, anpassbar unter Benutzer." />
        <div class="field">
          <label for="i-s">Zugriff auf einen Server <span class="faint">(optional)</span></label>
          <select id="i-s" class="select" bind:value={serverId}>
            <option value="">Keiner</option>
            {#each servers.list as s (s.id)}<option value={s.id}>{s.name}</option>{/each}
          </select>
        </div>
        {#if serverId && session.meta}
          <PermissionMatrix perms={session.meta.permissions} presets={session.meta.presets} bind:value={perms} />
        {/if}
      {/if}
    </form>
  {/if}
  {#snippet footer()}
    {#if link}
      <button class="btn primary" onclick={() => (open = false)}>Fertig</button>
    {:else}
      <button class="btn" type="button" onclick={() => (open = false)}>Abbrechen</button>
      <button class="btn primary" type="submit" form="inv-form">Link erstellen</button>
    {/if}
  {/snippet}
</Modal>

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .inv {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 12px 18px;
  }
  .inv.done .ii {
    opacity: 0.55;
  }
  .ii {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
</style>
