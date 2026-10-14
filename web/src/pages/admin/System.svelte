<script lang="ts">
  import { onMount } from 'svelte';
  import { Cpu, MemoryStick, HardDrive, Container, Coffee, Download, Trash2, RefreshCw } from '@lucide/svelte';
  import { api } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { toastError, toastOk } from '../../lib/toast.svelte';
  import { confirmDialog } from '../../lib/dialogs.svelte';
  import { bytes, duration, num1 } from '../../lib/format';

  let sys = $state<any>(null);
  let java = $state<{ runtimes: { major: number; path: string; version: string; size: number; source: string }[]; system: { path: string; major: number } } | null>(null);
  let installMajor = $state(21);
  let javaLog = $state<string[]>([]);

  async function load() {
    try {
      sys = await api.get('/api/admin/system');
    } catch (e) {
      toastError(e);
    }
  }

  async function loadJava() {
    try {
      java = await api.get('/api/admin/java');
    } catch {}
  }

  onMount(() => {
    load();
    loadJava();
    const t = setInterval(load, 15000);
    const off = live.on('task', null, (e) => {
      if (e.d?.kind !== 'java') return;
      javaLog = [...javaLog.slice(-5), e.d.message];
      if (/bereit|Fehler/.test(e.d.message)) loadJava();
    });
    return () => {
      clearInterval(t);
      off();
    };
  });

  async function install() {
    try {
      javaLog = [`Java ${installMajor} wird heruntergeladen …`];
      await api.post(`/api/admin/java/${installMajor}`);
    } catch (e) {
      toastError(e);
    }
  }

  async function removeJava(major: number) {
    const ok = await confirmDialog({ title: `Java ${major} entfernen?`, text: 'Server, die diese Version brauchen, laden sie beim nächsten Start erneut herunter.', confirm: 'Entfernen', danger: true });
    if (!ok) return;
    try {
      await api.del(`/api/admin/java/${major}`);
      toastOk(`Java ${major} entfernt`);
      loadJava();
    } catch (e) {
      toastError(e);
    }
  }

  const pct = (a: number, b: number) => (b ? (a / b) * 100 : 0);
</script>

{#if sys}
  <div class="stack">
    <div class="cards">
      <section class="panel card">
        <div class="ch"><Cpu size={17} /><h3>Prozessor</h3></div>
        <span class="big num">{num1(sys.cpuPercent ?? 0)} %</span>
        <div class="meter" class:warn={sys.cpuPercent > 80}><span style="width:{Math.min(100, sys.cpuPercent ?? 0)}%"></span></div>
        <span class="small muted ellipsis" title={sys.cpuModel}>{sys.cpus} Threads{#if sys.cpuModel}, {sys.cpuModel}{/if}</span>
        {#if sys.load}<span class="tiny faint num">Last {sys.load.map((l: number) => num1(l)).join(' / ')}</span>{/if}
      </section>
      <section class="panel card">
        <div class="ch"><MemoryStick size={17} /><h3>Arbeitsspeicher</h3></div>
        <span class="big num">{bytes(sys.memUsed)} <span class="faint small">von {bytes(sys.memTotal, 0)}</span></span>
        <div class="meter" class:warn={pct(sys.memUsed, sys.memTotal) > 85}><span style="width:{pct(sys.memUsed, sys.memTotal)}%"></span></div>
        <span class="small muted">{bytes(sys.memTotal - sys.memUsed)} frei</span>
      </section>
      <section class="panel card">
        <div class="ch"><HardDrive size={17} /><h3>Speicherplatz</h3></div>
        <span class="big num">{bytes(sys.diskUsed)} <span class="faint small">von {bytes(sys.diskTotal, 0)}</span></span>
        <div class="meter" class:warn={pct(sys.diskUsed, sys.diskTotal) > 85} class:danger={pct(sys.diskUsed, sys.diskTotal) > 95}>
          <span style="width:{pct(sys.diskUsed, sys.diskTotal)}%"></span>
        </div>
        <span class="small muted ellipsis" title={sys.dataDir}>{bytes(sys.diskFree)} frei in {sys.dataDir}</span>
      </section>
      <section class="panel card">
        <div class="ch"><Container size={17} /><h3>Docker</h3></div>
        {#if sys.docker}
          <span class="big">verfügbar</span>
          <span class="small muted">Version {sys.dockerVersion ?? '?'}</span>
        {:else}
          <span class="big faint">nicht verfügbar</span>
          <span class="small muted ellipsis" title={sys.dockerError}>{sys.dockerError}</span>
        {/if}
      </section>
    </div>

    <section class="panel">
      <div class="panel-head"><h3>Panel & Host</h3><span class="spacer"></span><button class="btn ghost sm icon" onclick={load} aria-label="Aktualisieren"><RefreshCw /></button></div>
      <dl class="kv">
        <dt>Version</dt>
        <dd>Hearth {sys.version}{#if sys.commit && sys.commit !== 'unknown'}{' '}<span class="faint mono small">({sys.commit.slice(0, 8)})</span>{/if}</dd>
        <dt>Läuft seit</dt>
        <dd>{duration(sys.uptime)}</dd>
        <dt>Server</dt>
        <dd>{sys.servers} angelegt, {sys.serversRunning} aktiv</dd>
        <dt>Rechner</dt>
        <dd>{sys.hostname ?? '?'}{#if sys.platform}, {sys.platform}{/if}</dd>
        <dt>System</dt>
        <dd>{sys.os}/{sys.arch}{#if sys.kernel}, Kernel {sys.kernel}{/if}</dd>
        <dt>Datenordner</dt>
        <dd class="mono small">{sys.dataDir}</dd>
        <dt>Go</dt>
        <dd>{sys.goVersion}</dd>
        <dt>Lizenzen</dt>
        <dd><a href="/licenses/THIRD_PARTY_LICENSES.txt" target="_blank" rel="noopener">Eingebaute Bibliotheken und Schriften</a></dd>
      </dl>
    </section>

    <section class="panel">
      <div class="panel-head"><Coffee size={16} /><h3>Java-Laufzeiten</h3></div>
      <div class="list">
        {#if java?.system.path}
          <div class="jrow">
            <strong class="jm">Java {java.system.major || '?'}</strong>
            <span class="small muted ellipsis mono">{java.system.path}</span>
            <span class="tag">System</span>
            <span></span>
          </div>
        {/if}
        {#each java?.runtimes.filter((r) => r.source !== 'system') ?? [] as r (r.major)}
          <div class="jrow">
            <strong class="jm">Java {r.major}</strong>
            <span class="small muted ellipsis">{r.version}{#if r.size}, {bytes(r.size)}{/if}</span>
            <span class="tag accent">vom Panel verwaltet</span>
            <button class="btn ghost sm icon" onclick={() => removeJava(r.major)} aria-label="Java {r.major} entfernen"><Trash2 /></button>
          </div>
        {/each}
        {#if !java?.system.path && !java?.runtimes.length}
          <p class="none small muted">Noch kein Java vorhanden. Das Panel lädt die passende Version beim ersten Start eines Servers automatisch.</p>
        {/if}
      </div>
      <div class="panel-body row wrap jinst">
        <span class="small muted">Vorab herunterladen:</span>
        <select class="select jsel" bind:value={installMajor} aria-label="Java-Version">
          {#each [8, 11, 17, 21, 25] as v (v)}<option value={v}>Java {v}</option>{/each}
        </select>
        <button class="btn" onclick={install}><Download /> Herunterladen</button>
        {#if javaLog.length}<span class="small faint ellipsis log">{javaLog[javaLog.length - 1]}</span>{/if}
      </div>
    </section>
  </div>
{/if}

<style>
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
    gap: 16px;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 16px 18px;
    min-width: 0;
  }
  .ch {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-2);
  }
  .ch h3 {
    font-size: 14px;
    color: var(--text-2);
  }
  .big {
    font-size: 21px;
    font-weight: 680;
  }
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 8px 20px;
    margin: 0;
    padding: 14px 18px;
    font-size: 14px;
  }
  .kv dt {
    color: var(--text-3);
  }
  .kv dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .jrow {
    display: grid;
    grid-template-columns: 90px 1fr auto 34px;
    gap: 12px;
    align-items: center;
    padding: 10px 18px;
  }
  .jm {
    font-size: 14.5px;
  }
  .none {
    padding: 14px 18px;
  }
  .jinst {
    border-top: 1px solid var(--line);
  }
  .jsel {
    width: 130px;
  }
  .log {
    flex: 1;
    min-width: 0;
  }
</style>
