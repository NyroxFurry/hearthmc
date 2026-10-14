<script lang="ts">
  import { onMount } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { WebLinksAddon } from '@xterm/addon-web-links';
  import '@xterm/xterm/css/xterm.css';
  import { ChevronRight, Eraser, ArrowDownToLine } from '@lucide/svelte';
  import { live } from '../lib/live.svelte';
  import { api } from '../lib/api';
  import { toastError } from '../lib/toast.svelte';

  let { serverId, canSend = false, running = false, fill = false }: { serverId: string; canSend?: boolean; running?: boolean; fill?: boolean } = $props();

  let host: HTMLDivElement;
  let term: Terminal;
  let fit: FitAddon;
  let input = $state('');
  let atBottom = $state(true);
  let history: string[] = [];
  let histIdx = -1;

  const histKey = () => `hearth-cmd-${serverId}`;

  function themeColors() {
    const cs = getComputedStyle(document.documentElement);
    const v = (n: string) => cs.getPropertyValue(n).trim();
    return {
      background: v('--console'),
      foreground: v('--console-text'),
      cursor: v('--console'),
      selectionBackground: 'rgba(79,174,148,0.35)',
      black: '#1b1e24', red: '#e5584f', green: '#5fc08a', yellow: '#e9b949', blue: '#6aa5e8', magenta: '#c08ae0',
      cyan: '#4fbfae', white: '#d7dbe2', brightBlack: '#6b7380', brightRed: '#ff7a70', brightGreen: '#7fe0a8',
      brightYellow: '#ffd36b', brightBlue: '#8cc0ff', brightMagenta: '#dcaaff', brightCyan: '#79e3d2', brightWhite: '#ffffff',
    };
  }

  onMount(() => {
    try {
      history = JSON.parse(sessionStorage.getItem(histKey()) || '[]');
    } catch {}
    term = new Terminal({
      // xterm measures glyphs on a canvas and cannot use variable font axes,
      // so the console uses a real monospace font
      fontFamily: "ui-monospace, 'Cascadia Mono', 'JetBrains Mono', 'SF Mono', Menlo, Consolas, 'DejaVu Sans Mono', 'Liberation Mono', monospace",
      fontSize: 13,
      lineHeight: 1.28,
      scrollback: 6000,
      disableStdin: true,
      convertEol: true,
      cursorInactiveStyle: 'none',
      cursorStyle: 'bar',
      theme: themeColors(),
      allowProposedApi: false,
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(host);
    requestAnimationFrame(() => fit.fit());
    const ro = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {}
    });
    ro.observe(host);
    term.onScroll(() => {
      const b = term.buffer.active;
      atBottom = b.viewportY >= b.baseY;
    });

    const offHistory = live.on('history', serverId, (e) => {
      term.reset();
      const lines: string[] = e.d || [];
      if (lines.length) term.write(lines.join('\r\n') + '\r\n');
      term.scrollToBottom();
    });
    const offConsole = live.on('console', serverId, (e) => {
      const follow = atBottom;
      term.write((e.d as string[]).join('\r\n') + '\r\n');
      if (follow) term.scrollToBottom();
    });
    const unsub = live.subscribeConsole(serverId);
    // initial history via REST in case the socket is not connected yet
    if (!live.connected) {
      api
        .get<{ lines: string[] }>(`/api/servers/${serverId}/console?lines=1500`)
        .then((r) => {
          if (r.lines?.length) term.write(r.lines.join('\r\n') + '\r\n');
        })
        .catch(() => {});
    }
    const mo = new MutationObserver(() => (term.options.theme = themeColors()));
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    return () => {
      offHistory();
      offConsole();
      unsub();
      ro.disconnect();
      mo.disconnect();
      term.dispose();
    };
  });

  async function send(e: Event) {
    e.preventDefault();
    const cmd = input.trim();
    if (!cmd) return;
    history = [cmd, ...history.filter((h) => h !== cmd)].slice(0, 50);
    try {
      sessionStorage.setItem(histKey(), JSON.stringify(history));
    } catch {}
    histIdx = -1;
    input = '';
    if (live.connected) live.command(serverId, cmd);
    else {
      try {
        await api.post(`/api/servers/${serverId}/command`, { command: cmd });
      } catch (err) {
        toastError(err);
      }
    }
    term.scrollToBottom();
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'ArrowUp') {
      if (histIdx < history.length - 1) {
        histIdx++;
        input = history[histIdx];
      }
      e.preventDefault();
    } else if (e.key === 'ArrowDown') {
      if (histIdx > 0) {
        histIdx--;
        input = history[histIdx];
      } else {
        histIdx = -1;
        input = '';
      }
      e.preventDefault();
    }
  }
</script>

<div class="console" class:fill>
  <div class="term" bind:this={host}></div>
  <div class="tools">
    {#if !atBottom}
      <button class="btn sm" onclick={() => term.scrollToBottom()}><ArrowDownToLine /> Neueste</button>
    {/if}
    <button class="btn ghost sm icon" title="Ansicht leeren" aria-label="Ansicht leeren" onclick={() => term.clear()}><Eraser /></button>
  </div>
  {#if canSend}
    <form class="prompt" onsubmit={send}>
      <ChevronRight />
      <input
        bind:value={input}
        onkeydown={keydown}
        placeholder={running ? 'Befehl eingeben, z. B. say Hallo oder list' : 'Server ist offline'}
        disabled={!running}
        autocomplete="off"
        spellcheck="false"
        aria-label="Konsolenbefehl"
      />
    </form>
  {/if}
</div>

<style>
  .console {
    position: relative;
    display: flex;
    flex-direction: column;
    background: var(--console);
    border-radius: var(--r-l);
    border: 1px solid var(--line);
    overflow: hidden;
    min-height: 320px;
    height: 100%;
  }
  .console.fill {
    height: 100%;
  }
  .term {
    flex: 1;
    min-height: 0;
    padding: 10px 4px 6px 12px;
  }
  .term :global(.xterm-viewport) {
    background: transparent !important;
  }
  .term :global(.xterm-viewport::-webkit-scrollbar) {
    width: 8px;
  }
  .term :global(.xterm-viewport::-webkit-scrollbar-thumb) {
    background: rgba(255, 255, 255, 0.12);
    border-radius: 4px;
  }
  .tools {
    position: absolute;
    top: 8px;
    right: 14px;
    display: flex;
    gap: 6px;
    z-index: 5;
  }
  .tools :global(.btn) {
    --b-bg: rgba(40, 44, 52, 0.85);
    --b-fg: #c9ced6;
    --b-line: rgba(255, 255, 255, 0.08);
  }
  .prompt {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 12px;
    border-top: 1px solid rgba(255, 255, 255, 0.07);
    background: rgba(255, 255, 255, 0.025);
    color: var(--accent);
  }
  .prompt :global(svg) {
    width: 16px;
    height: 16px;
    flex: none;
  }
  .prompt input {
    flex: 1;
    height: 42px;
    background: transparent;
    border: none;
    outline: none;
    color: var(--console-text);
    font: inherit;
    font-family: var(--font-mono);
    font-size: 13.5px;
  }
  .prompt input::placeholder {
    color: #6b7380;
  }
</style>
