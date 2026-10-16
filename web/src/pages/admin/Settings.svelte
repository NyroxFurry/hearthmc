<script lang="ts">
  import { onMount } from 'svelte';
  import { Save } from '@lucide/svelte';
  import Toggle from '../../components/Toggle.svelte';
  import { api } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';

  let values = $state<Record<string, string>>({});
  let original = $state<Record<string, string>>({});
  let loaded = $state(false);

  async function load() {
    try {
      const r = await api.get('/api/admin/settings');
      values = { ...r };
      original = { ...r };
    } catch (e) {
      toastError(e);
    } finally {
      loaded = true;
    }
  }
  onMount(load);

  let changed = $derived(Object.keys(values).filter((k) => String(values[k] ?? '') !== String(original[k] ?? '')));

  async function save() {
    const patch: Record<string, string> = {};
    for (const k of changed) patch[k] = String(values[k]);
    try {
      const r = await api.put('/api/admin/settings', patch);
      values = { ...r };
      original = { ...r };
      toastOk('Einstellungen gespeichert');
      session.load();
    } catch (e) {
      toastError(e);
    }
  }

  const zones = (() => {
    try {
      return (Intl as any).supportedValuesOf('timeZone') as string[];
    } catch {
      return ['Europe/Berlin', 'Europe/Vienna', 'Europe/Zurich', 'UTC'];
    }
  })();
</script>

{#if loaded}
  <div class="stack settings">
    <section class="panel">
      <div class="panel-head"><h3>Panel</h3></div>
      <div class="panel-body form-grid">
        <div class="field">
          <label for="s-name">Name</label>
          <input id="s-name" class="input" bind:value={values['panel.name']} maxlength="60" />
          <p class="hint">Erscheint im Browser-Tab, bei der Anmeldung und auf öffentlichen Seiten.</p>
        </div>
        <div class="field">
          <label for="s-url">Öffentliche Adresse</label>
          <input id="s-url" class="input mono" bind:value={values['panel.url']} placeholder={location.origin} />
          <p class="hint">Für Einladungslinks und öffentliche Seiten. Leer = aus der Anfrage ermittelt.</p>
        </div>
        <div class="field">
          <label for="s-contact">Kontakt</label>
          <input id="s-contact" class="input" bind:value={values['panel.contact']} placeholder="admin@example.com" />
          <p class="hint">Wird bei Anfragen an Modrinth und Mojang mitgeschickt, wie dort gewünscht.</p>
        </div>
        <div class="field">
          <label for="s-tz">Zeitzone für Zeitpläne</label>
          <select id="s-tz" class="select" bind:value={values['timezone']}>
            <option value="">Wie das System</option>
            {#each zones as z (z)}<option value={z}>{z}</option>{/each}
          </select>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Neue Server</h3></div>
      <div class="panel-body form-grid">
        <div class="field">
          <label for="s-mem">Standard-Arbeitsspeicher (MB)</label>
          <input id="s-mem" class="input num" type="number" min="256" step="512" bind:value={values['servers.defaultMemoryMb']} />
        </div>
        <div class="field">
          <label for="s-rt">Standard-Laufzeit</label>
          <select id="s-rt" class="select" bind:value={values['runtime.default']}>
            <option value="native">Direkt auf dem Host</option>
            <option value="docker">Docker-Container</option>
          </select>
        </div>
        <div class="field">
          <label for="s-java">Java</label>
          <select id="s-java" class="select" bind:value={values['java.mode']}>
            <option value="auto">Passende Version automatisch laden</option>
            <option value="system">Immer das System-Java verwenden</option>
          </select>
        </div>
        <div class="field">
          <label for="s-docker">Docker-Adresse</label>
          <input id="s-docker" class="input mono" bind:value={values['docker.host']} placeholder="unix:///var/run/docker.sock" />
          <p class="hint">Leer = Standard-Socket.</p>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Ports</h3></div>
      <div class="panel-body form-grid">
        <div class="field">
          <label for="s-ps">Spielports von</label>
          <input id="s-ps" class="input num" type="number" bind:value={values['ports.start']} />
        </div>
        <div class="field">
          <label for="s-pe">bis</label>
          <input id="s-pe" class="input num" type="number" bind:value={values['ports.end']} />
        </div>
        <div class="field">
          <label for="s-is">Interne Ports (RCON) von</label>
          <input id="s-is" class="input num" type="number" bind:value={values['ports.internalStart']} />
        </div>
        <div class="field">
          <label for="s-ie">bis</label>
          <input id="s-ie" class="input num" type="number" bind:value={values['ports.internalEnd']} />
          <p class="hint">Interne Ports lauschen nur lokal und müssen nicht freigegeben werden.</p>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Speicher & Aufbewahrung</h3></div>
      <div class="panel-body form-grid">
        <div class="field">
          <label for="s-bm">Backups pro Server</label>
          <input id="s-bm" class="input num" type="number" min="0" bind:value={values['backups.max']} />
          <p class="hint">Ältere werden automatisch gelöscht. 0 = unbegrenzt.</p>
        </div>
        <div class="field">
          <label for="s-ul">Upload-Limit (MB)</label>
          <input id="s-ul" class="input num" type="number" min="0" bind:value={values['files.uploadLimitMb']} />
          <p class="hint">0 = unbegrenzt</p>
        </div>
        <div class="field">
          <label for="s-mt">Kartenzeichner-Threads</label>
          <input id="s-mt" class="input num" type="number" min="1" max="64" bind:value={values['map.threads']} />
        </div>
        <div class="field">
          <label for="s-mu">Adresse des Kartenports <span class="faint">(optional)</span></label>
          <input id="s-mu" class="input mono" bind:value={values['map.publicUrl']} placeholder="automatisch" />
          <p class="hint">Karten-Plugins (BlueMap, squaremap, Dynmap) erscheinen über einen eigenen Port des Panels, standardmäßig Panel-Port + 1. Hinter einem Reverse-Proxy trägst du hier dessen Adresse ein, z. B. https://karte.example.de.</p>
        </div>
        <div class="field">
          <label for="s-sf">SFTP-Adresse <span class="faint">(optional)</span></label>
          <input id="s-sf" class="input mono" bind:value={values['sftp.host']} placeholder="automatisch" />
          <p class="hint">Wird Benutzern als SFTP-Server angezeigt. Leer lassen für den Host des Panels und Port 2022. Nötig, wenn das Panel hinter einem Proxy liegt, der kein SSH weiterleitet, z. B. sftp.example.de:2022.</p>
        </div>
        <div class="field">
          <label for="s-rm">Messwerte aufbewahren (Tage)</label>
          <input id="s-rm" class="input num" type="number" min="1" bind:value={values['retention.metricsDays']} />
        </div>
        <div class="field">
          <label for="s-ra">Protokoll aufbewahren (Tage)</label>
          <input id="s-ra" class="input num" type="number" min="1" bind:value={values['retention.auditDays']} />
        </div>
        <div class="field">
          <label for="s-rc">Chat aufbewahren (Tage)</label>
          <input id="s-rc" class="input num" type="number" min="1" bind:value={values['retention.chatDays']} />
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>Öffentliche Seiten</h3></div>
      <div class="panel-body">
        <Toggle
          checked={values['public.enabled'] === '1'}
          label="Öffentliche Serverseiten erlauben"
          hint="Ausgeschaltet sind alle /s/…-Seiten und Status-Banner nicht mehr erreichbar."
          onchange={(v) => (values['public.enabled'] = v ? '1' : '0')}
        />
      </div>
    </section>

    {#if changed.length}
      <div class="savebar">
        <span class="small"><strong>{changed.length}</strong> {changed.length === 1 ? 'Änderung' : 'Änderungen'}</span>
        <span class="spacer"></span>
        <button class="btn ghost" onclick={() => (values = { ...original })}>Verwerfen</button>
        <button class="btn primary" onclick={save}><Save /> Speichern</button>
      </div>
    {/if}
  </div>
{/if}

<style>
  .settings {
    padding-bottom: 50px;
  }
  .savebar {
    position: sticky;
    bottom: 16px;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: var(--surface-2);
    border: 1px solid var(--line-2);
    border-radius: var(--r-l);
    box-shadow: var(--pop-shadow);
    z-index: 10;
  }
</style>
