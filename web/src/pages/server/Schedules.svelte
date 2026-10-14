<script lang="ts">
  import { onMount } from 'svelte';
  import {
    CalendarClock, Plus, Play, Pencil, Trash2, ArrowUp, ArrowDown, X, TerminalSquare, MessageSquare, Power, Archive, Hourglass, CircleCheck, CircleX, LoaderCircle,
  } from '@lucide/svelte';
  import Modal from '../../components/Modal.svelte';
  import Toggle from '../../components/Toggle.svelte';
  import Menu, { type MenuItem } from '../../components/Menu.svelte';
  import { api, qs } from '../../lib/api';
  import { can } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog } from '../../lib/dialogs.svelte';
  import { ago, dateTime } from '../../lib/format';
  import { describeCron, cronPresets } from '../../lib/cron';
  import type { Schedule, ScheduleStep, Server } from '../../lib/types';

  let { server }: { server: Server } = $props();

  const url = $derived(`/api/servers/${server.id}/schedules`);
  const canEdit = $derived(can(server, 'schedules.edit'));
  let list = $state<Schedule[]>([]);
  let tz = $state('');
  let loaded = $state(false);

  async function load() {
    try {
      const r = await api.get<{ schedules: Schedule[]; timezone: string }>(url);
      list = r.schedules;
      tz = r.timezone;
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }

  onMount(() => {
    load();
    const t = setInterval(load, 15000);
    return () => clearInterval(t);
  });

  // ---- editor ----
  type Draft = { id: number; name: string; cron: string; enabled: boolean; onlyWhenOnline: boolean; steps: ScheduleStep[] };
  let open = $state(false);
  let draft = $state<Draft>({ id: 0, name: '', cron: '0 4 * * *', enabled: true, onlyWhenOnline: false, steps: [] });
  let preview = $state<number[]>([]);
  let cronError = $state('');
  let saving = $state(false);

  const templates: { label: string; make: () => Omit<Draft, 'id'> }[] = [
    {
      label: 'Tägliches Backup',
      make: () => ({ name: 'Tägliches Backup', cron: '0 4 * * *', enabled: true, onlyWhenOnline: false, steps: [{ type: 'backup', value: 'Automatisches Backup' }] }),
    },
    {
      label: 'Neustart mit Vorwarnung',
      make: () => ({
        name: 'Nächtlicher Neustart',
        cron: '0 5 * * *',
        enabled: true,
        onlyWhenOnline: true,
        steps: [
          { type: 'say', value: 'Der Server startet in 5 Minuten neu.' },
          { type: 'wait', value: '', seconds: 240 },
          { type: 'say', value: 'Neustart in 1 Minute!' },
          { type: 'wait', value: '', seconds: 50 },
          { type: 'say', value: 'Neustart in 10 Sekunden …' },
          { type: 'wait', value: '', seconds: 10 },
          { type: 'power', value: 'restart' },
        ],
      }),
    },
    {
      label: 'Regelmäßige Nachricht',
      make: () => ({ name: 'Hinweis im Chat', cron: '*/30 * * * *', enabled: true, onlyWhenOnline: true, steps: [{ type: 'say', value: '' }] }),
    },
    {
      label: 'Welt speichern',
      make: () => ({ name: 'Welt speichern', cron: '*/15 * * * *', enabled: true, onlyWhenOnline: true, steps: [{ type: 'command', value: 'save-all' }] }),
    },
  ];

  function edit(s: Schedule | null, tpl?: Omit<Draft, 'id'>) {
    if (s) draft = { id: s.id, name: s.name, cron: s.cron, enabled: s.enabled, onlyWhenOnline: s.onlyWhenOnline, steps: s.steps.map((x) => ({ ...x })) };
    else draft = { id: 0, ...(tpl ?? { name: '', cron: '0 4 * * *', enabled: true, onlyWhenOnline: false, steps: [{ type: 'command', value: '' }] }) };
    open = true;
  }

  let previewTimer: ReturnType<typeof setTimeout>;
  $effect(() => {
    const expr = draft.cron;
    if (!open) return;
    clearTimeout(previewTimer);
    previewTimer = setTimeout(async () => {
      try {
        const r = await api.get<{ next: number[] }>(`/api/cron/preview${qs({ expr })}`);
        preview = r.next;
        cronError = '';
      } catch (e: any) {
        preview = [];
        cronError = e.message;
      }
    }, 250);
  });

  function addStep(type: ScheduleStep['type']) {
    const step: ScheduleStep = { type, value: type === 'power' ? 'restart' : '' };
    if (type === 'wait') step.seconds = 60;
    draft.steps = [...draft.steps, step];
  }

  function moveStep(i: number, d: number) {
    const s = [...draft.steps];
    const [x] = s.splice(i, 1);
    s.splice(i + d, 0, x);
    draft.steps = s;
  }

  async function save(e: Event) {
    e.preventDefault();
    saving = true;
    try {
      const body = { ...draft, steps: draft.steps.map((s) => (s.type === 'wait' ? { type: 'wait', value: '', seconds: Number(s.seconds) || 1 } : s)) };
      if (draft.id) await api.put(`${url}/${draft.id}`, body);
      else await api.post(url, body);
      open = false;
      toastOk('Zeitplan gespeichert');
      load();
    } catch (err) {
      toastError(err);
    } finally {
      saving = false;
    }
  }

  async function toggleEnabled(s: Schedule, enabled: boolean) {
    try {
      await api.put(`${url}/${s.id}`, { ...s, enabled });
      load();
    } catch (e) {
      toastError(e);
      load();
    }
  }

  async function run(s: Schedule) {
    try {
      await api.post(`${url}/${s.id}/run`);
      toastOk(`„${s.name}“ wird ausgeführt`);
      setTimeout(load, 800);
    } catch (e) {
      toastError(e);
    }
  }

  async function remove(s: Schedule) {
    if (!(await confirmDialog({ title: `„${s.name}“ löschen?`, confirm: 'Löschen', danger: true }))) return;
    try {
      await api.del(`${url}/${s.id}`);
      load();
    } catch (e) {
      toastError(e);
    }
  }

  const stepMeta: Record<string, { label: string; icon: any }> = {
    command: { label: 'Befehl', icon: TerminalSquare },
    say: { label: 'Nachricht', icon: MessageSquare },
    power: { label: 'Aktion', icon: Power },
    backup: { label: 'Backup', icon: Archive },
    wait: { label: 'Warten', icon: Hourglass },
  };
  const powerLabels: Record<string, string> = { start: 'Starten', stop: 'Stoppen', restart: 'Neu starten', kill: 'Hart beenden' };

  function stepText(s: ScheduleStep): string {
    switch (s.type) {
      case 'command':
        return s.value;
      case 'say':
        return `„${s.value}“`;
      case 'power':
        return powerLabels[s.value] ?? s.value;
      case 'backup':
        return s.value || 'Backup erstellen';
      case 'wait':
        return (s.seconds ?? 0) >= 60 && (s.seconds ?? 0) % 60 === 0 ? `${(s.seconds ?? 0) / 60} Min.` : `${s.seconds} Sek.`;
    }
    return '';
  }

  function menu(s: Schedule): MenuItem[] {
    return [
      { label: 'Jetzt ausführen', icon: Play, onclick: () => run(s), disabled: s.running },
      { label: 'Bearbeiten', icon: Pencil, onclick: () => edit(s) },
      { divider: true, label: '' },
      { label: 'Löschen', icon: Trash2, danger: true, onclick: () => remove(s) },
    ];
  }
</script>

<div class="sec">
  <div class="sec-head">
    <div class="titles">
      <h2>Zeitpläne</h2>
      <p>Wiederkehrende Abläufe wie nächtliche Backups, Neustarts mit Vorwarnung oder Hinweise im Chat. {#if tz}Zeitzone: {tz}.{/if}</p>
    </div>
    {#if canEdit}
      <Menu text="Vorlage" items={templates.map((t) => ({ label: t.label, onclick: () => edit(null, t.make()) }))} />
      <button class="btn primary" onclick={() => edit(null)}><Plus /> Neuer Zeitplan</button>
    {/if}
  </div>

  {#if loaded && list.length === 0}
    <div class="panel">
      <div class="empty">
        <CalendarClock />
        <h3>Noch keine Zeitpläne</h3>
        <p>Starte mit einer Vorlage, zum Beispiel einem täglichen Backup um 4 Uhr nachts.</p>
      </div>
    </div>
  {:else}
    <div class="cards">
      {#each list as s (s.id)}
        <article class="panel sched" class:off={!s.enabled}>
          <div class="top">
            <div class="ttl">
              <h3>{s.name}</h3>
              <span class="small muted">{describeCron(s.cron)}{s.onlyWhenOnline ? ', nur wenn der Server läuft' : ''}</span>
            </div>
            {#if canEdit}
              <div class="tg"><Toggle checked={s.enabled} label="Aktiv" onchange={(v) => toggleEnabled(s, v)} /></div>
              <Menu items={menu(s)} />
            {/if}
          </div>
          <ol class="steps">
            {#each s.steps as st, i (i)}
              {@const M = stepMeta[st.type]}
              <li class="step {st.type}" title={M?.label}>
                <M.icon size={13} />
                <span class="ellipsis">{stepText(st)}</span>
              </li>
            {/each}
          </ol>
          <div class="foot small">
            {#if s.running}
              <span class="run"><LoaderCircle size={14} class="spin" /> Läuft gerade</span>
            {:else if s.enabled && s.next?.length}
              <span class="muted" title={dateTime(s.next[0])}>Nächster Lauf {ago(s.next[0])}</span>
            {:else if !s.enabled}
              <span class="faint">Pausiert</span>
            {/if}
            <span class="spacer"></span>
            {#if s.lastRunAt}
              {#if s.lastStatus === 'ok'}
                <span class="faint" title={dateTime(s.lastRunAt)}><CircleCheck size={13} class="ok" /> {ago(s.lastRunAt)}</span>
              {:else if s.lastStatus === 'skipped'}
                <span class="faint" title={s.lastError || 'Übersprungen'}>Übersprungen {ago(s.lastRunAt)}</span>
              {:else}
                <span class="bad" title={s.lastError}><CircleX size={13} /> Fehler {ago(s.lastRunAt)}</span>
              {/if}
            {/if}
          </div>
          {#if s.lastStatus === 'failed' && s.lastError}<p class="tiny err">{s.lastError}</p>{/if}
        </article>
      {/each}
    </div>
  {/if}
</div>

<Modal bind:open title={draft.id ? 'Zeitplan bearbeiten' : 'Neuer Zeitplan'} size="l">
  <form id="sched-form" class="stack" onsubmit={save}>
    <div class="field">
      <label for="sn">Name</label>
      <input id="sn" class="input" bind:value={draft.name} maxlength="80" required placeholder="z. B. Nächtlicher Neustart" />
    </div>

    <div class="field">
      <label for="sc">Wann</label>
      <div class="presets">
        {#each cronPresets as p (p.expr)}
          <button type="button" class="btn sm" class:primary={draft.cron === p.expr} onclick={() => (draft.cron = p.expr)}>{p.label}</button>
        {/each}
      </div>
      <input id="sc" class="input mono" bind:value={draft.cron} required spellcheck="false" />
      {#if cronError}
        <p class="error-text">{cronError}</p>
      {:else}
        <p class="hint">
          <strong>{describeCron(draft.cron)}</strong>{#if preview.length}. Nächste Läufe: {preview.slice(0, 3).map((t) => dateTime(t)).join(', ')}{/if}
        </p>
        <p class="hint">Cron-Format: Minute, Stunde, Tag, Monat, Wochentag. Beispiel <code>30 3 * * 1-5</code> heißt werktags um 3:30.</p>
      {/if}
    </div>

    <div class="toggles">
      <Toggle bind:checked={draft.enabled} label="Aktiv" />
      <Toggle bind:checked={draft.onlyWhenOnline} label="Nur ausführen, wenn der Server läuft" hint="Sonst wird der Lauf übersprungen." />
    </div>

    <div class="field">
      <span class="label">Schritte</span>
      <p class="hint">Werden nacheinander ausgeführt. Mit „Warten“ baust du Pausen ein, etwa für Vorwarnungen.</p>
      <ol class="editsteps">
        {#each draft.steps as st, i (i)}
          {@const M = stepMeta[st.type]}
          <li>
            <span class="n num">{i + 1}</span>
            <span class="kind"><M.icon size={14} /> {M.label}</span>
            <div class="val">
              {#if st.type === 'power'}
                <select class="select" bind:value={st.value}>
                  {#each Object.entries(powerLabels) as [k, l] (k)}<option value={k}>{l}</option>{/each}
                </select>
              {:else if st.type === 'wait'}
                <div class="row">
                  <input class="input num" type="number" min="1" max="3600" bind:value={st.seconds} style="max-width:120px" />
                  <span class="small muted">Sekunden</span>
                </div>
              {:else if st.type === 'backup'}
                <input class="input" bind:value={st.value} placeholder="Name des Backups (optional)" />
              {:else if st.type === 'say'}
                <input class="input" bind:value={st.value} placeholder="Nachricht an alle Spieler" required />
              {:else}
                <input class="input mono" bind:value={st.value} placeholder="z. B. save-all" required spellcheck="false" />
              {/if}
            </div>
            <div class="ops">
              <button type="button" class="btn ghost sm icon" disabled={i === 0} onclick={() => moveStep(i, -1)} aria-label="Nach oben"><ArrowUp /></button>
              <button type="button" class="btn ghost sm icon" disabled={i === draft.steps.length - 1} onclick={() => moveStep(i, 1)} aria-label="Nach unten"><ArrowDown /></button>
              <button type="button" class="btn ghost sm icon" onclick={() => (draft.steps = draft.steps.filter((_, j) => j !== i))} aria-label="Schritt entfernen"><X /></button>
            </div>
          </li>
        {/each}
      </ol>
      <div class="row wrap add">
        {#each Object.entries(stepMeta) as [k, m] (k)}
          <button type="button" class="btn sm" onclick={() => addStep(k as ScheduleStep['type'])}><Plus /> {m.label}</button>
        {/each}
      </div>
    </div>
  </form>
  {#snippet footer()}
    <button class="btn" onclick={() => (open = false)}>Abbrechen</button>
    <button class="btn primary" type="submit" form="sched-form" disabled={saving || !draft.steps.length || !!cronError}>Speichern</button>
  {/snippet}
</Modal>

<style>
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(330px, 1fr));
    gap: 14px;
  }
  .sched {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px 14px 12px 16px;
  }
  .sched.off {
    opacity: 0.72;
  }
  .top {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }
  .ttl {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .tg {
    margin-top: -3px;
  }
  .tg :global(.label) {
    font-size: 12.5px;
    color: var(--text-3);
    font-weight: 500;
  }
  .steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .step {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    height: 26px;
    padding: 0 9px;
    border-radius: 6px;
    background: var(--surface-2);
    font-size: 12.5px;
    color: var(--text-2);
  }
  .step :global(svg) {
    flex: none;
    color: var(--text-3);
  }
  .step.power :global(svg) {
    color: var(--copper);
  }
  .step.say :global(svg) {
    color: var(--info);
  }
  .step.backup :global(svg) {
    color: var(--accent);
  }
  .foot {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px 8px;
    padding-top: 10px;
    border-top: 1px solid var(--line);
  }
  .foot span {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    white-space: nowrap;
  }
  .foot :global(.ok) {
    color: var(--accent);
  }
  .bad {
    color: var(--danger);
  }
  .run {
    color: var(--warn);
  }
  :global(.spin) {
    animation: spin 1s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .err {
    color: var(--danger);
    margin-top: -6px;
  }
  .presets {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .toggles {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .editsteps {
    list-style: none;
    margin: 4px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .editsteps li {
    display: grid;
    grid-template-columns: 22px 108px 1fr auto;
    align-items: center;
    gap: 8px;
    padding: 6px 6px 6px 10px;
    border: 1px solid var(--line);
    border-radius: var(--r-m);
    background: var(--surface-2);
  }
  .n {
    color: var(--text-3);
    font-size: 13px;
  }
  .kind {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 13.5px;
    font-weight: 600;
  }
  .ops {
    display: flex;
    gap: 2px;
  }
  .add {
    margin-top: 6px;
    gap: 6px;
  }
  @media (max-width: 640px) {
    .editsteps li {
      grid-template-columns: 22px 1fr auto;
    }
    .val {
      grid-column: 1 / -1;
    }
  }
</style>
