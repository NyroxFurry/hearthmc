<script lang="ts">
  // Zugangsdaten für den eingebauten SFTP-Server, im Dateien-Bereich eines Servers.
  import { TriangleAlert, ExternalLink, KeySquare } from '@lucide/svelte';
  import Modal from './Modal.svelte';
  import CopyButton from './CopyButton.svelte';
  import { api } from '../lib/api';

  let { open = $bindable(), serverId }: { open: boolean; serverId: string } = $props();

  interface Info {
    enabled: boolean;
    host: string;
    port: number;
    username: string;
    fingerprint: string;
    passwordLogin: boolean;
    keys: number;
    write: boolean;
    delete: boolean;
    error?: string;
  }
  let info = $state<Info | null>(null);
  let error = $state('');

  $effect(() => {
    if (!open) return;
    error = '';
    api
      .get<Info>(`/api/servers/${serverId}/sftp`)
      .then((r) => (info = r))
      .catch((e) => (error = e.message));
  });

  let hostPort = $derived(info ? (info.host.includes(':') ? `[${info.host}]` : info.host) : '');
  let url = $derived(info ? `sftp://${encodeURIComponent(info.username)}@${hostPort}:${info.port}` : '');
  let cli = $derived(info ? `sftp -P ${info.port} ${info.username}@${hostPort}` : '');
  let rights = $derived.by(() => {
    if (!info) return '';
    const r = ['ansehen'];
    if (info.write) r.push('hochladen', 'ändern');
    if (info.delete) r.push('löschen');
    if (r.length === 1) r.push('herunterladen');
    return r.slice(0, -1).join(', ') + ' und ' + r[r.length - 1];
  });
</script>

<Modal bind:open title="SFTP-Zugang" size="m">
  {#if error}
    <div class="notice danger"><TriangleAlert /><span>{error}</span></div>
  {:else if info && !info.enabled}
    <div class="notice warn">
      <TriangleAlert />
      <span>
        {#if info.error}Der SFTP-Server konnte nicht starten: {info.error}. Ist Port {info.port} schon belegt?{:else}SFTP ist auf diesem Panel ausgeschaltet.{/if}
        Die Verwaltung stellt das in der Panel-Konfiguration ein (<code>sftpListen</code>).
      </span>
    </div>
  {:else if info}
    <div class="stack">
      <p class="small muted">Mit einem SFTP-Programm wie FileZilla, WinSCP oder Cyberduck arbeitest du direkt mit den Dateien dieses Servers. Als Protokoll SFTP wählen und diese Daten eintragen:</p>
      <dl class="creds">
        <dt>Server</dt>
        <dd><code>{info.host}</code><CopyButton text={info.host} label="" cls="btn ghost sm icon" /></dd>
        <dt>Port</dt>
        <dd><code>{info.port}</code><CopyButton text={String(info.port)} label="" cls="btn ghost sm icon" /></dd>
        <dt>Benutzer</dt>
        <dd><code>{info.username}</code><CopyButton text={info.username} label="" cls="btn ghost sm icon" /></dd>
        <dt>Passwort</dt>
        <dd>
          {#if info.passwordLogin}
            <span class="small">Dein Panel-Passwort{info.keys ? ' oder einer deiner SSH-Schlüssel' : ''}</span>
          {:else}
            <span class="small">Nur mit SSH-Schlüssel, weil deine Zwei-Faktor-Anmeldung aktiv ist. {info.keys ? `${info.keys} hinterlegt.` : ''}</span>
          {/if}
        </dd>
      </dl>
      {#if !info.passwordLogin && !info.keys}
        <div class="notice info">
          <KeySquare />
          <span>Hinterlege zuerst unter <a href="/account">Konto &amp; Sicherheit</a> einen SSH-Schlüssel.</span>
        </div>
      {/if}
      <div class="field">
        <span class="label">Fingerabdruck des Servers</span>
        <code class="fp">{info.fingerprint}</code>
        <p class="hint">Beim ersten Verbinden fragt dein Programm, ob es diesem Server vertrauen soll. Der angezeigte Fingerabdruck muss genau so aussehen.</p>
      </div>
      <div class="field">
        <span class="label">Terminal</span>
        <div class="cli"><code>{cli}</code><CopyButton text={cli} label="" cls="btn ghost sm icon" /></div>
      </div>
      <p class="small muted">Du darfst hier Dateien {rights}. Jede Anmeldung steht im Aktivitätsprotokoll.</p>
    </div>
  {:else}
    <p class="muted">Wird geladen …</p>
  {/if}
  {#snippet footer()}
    {#if info?.enabled}<a class="btn" href={url}><ExternalLink /> Im SFTP-Programm öffnen</a>{/if}
    <button class="btn primary" onclick={() => (open = false)}>Fertig</button>
  {/snippet}
</Modal>

<style>
  .creds {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 6px 16px;
    margin: 0;
    align-items: center;
  }
  .creds dt {
    font-size: 13.5px;
    font-weight: 700;
    color: var(--text-2);
  }
  .creds dd {
    margin: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .creds code,
  .cli code {
    overflow-wrap: anywhere;
  }
  .fp {
    font-size: 12.5px;
    overflow-wrap: anywhere;
    padding: 8px 10px;
    border-radius: var(--r-s);
    background: var(--surface-2);
  }
  .cli {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 4px 4px 10px;
    border-radius: var(--r-s);
    background: var(--surface-2);
  }
  .cli code {
    flex: 1;
    font-size: 12.5px;
  }
</style>
