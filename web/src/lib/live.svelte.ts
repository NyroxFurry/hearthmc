// Live updates over a single multiplexed WebSocket.
import type { Status } from './types';

export interface LiveEvent {
  t: string;
  s?: string;
  d?: any;
}

type Listener = { type: string; server: string | null; fn: (e: LiveEvent) => void };

class Live {
  connected = $state(false);
  status = $state<Record<string, Status>>({});
  /** last map render progress per server */
  mapProgress = $state<Record<string, any>>({});

  private ws: WebSocket | null = null;
  private listeners = new Set<Listener>();
  private consoleRefs = new Map<string, number>();
  private retry = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private wanted = false;

  connect() {
    this.wanted = true;
    if (this.ws) return;
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${proto}://${location.host}/api/ws`);
    this.ws = ws;
    ws.onopen = () => {
      this.connected = true;
      this.retry = 0;
      for (const id of this.consoleRefs.keys()) this.send({ op: 'sub', s: id });
      this.dispatch({ t: 'reconnected' });
    };
    ws.onmessage = (m) => {
      let e: LiveEvent;
      try {
        e = JSON.parse(m.data);
      } catch {
        return;
      }
      if ((e.t === 'status' || e.t === 'stats') && e.s) this.status[e.s] = e.d;
      if (e.t === 'map' && e.s) this.mapProgress[e.s] = e.d;
      this.dispatch(e);
    };
    ws.onclose = () => {
      this.ws = null;
      this.connected = false;
      if (!this.wanted) return;
      const delay = Math.min(15000, 500 * 2 ** this.retry++);
      this.timer = setTimeout(() => this.connect(), delay);
    };
    ws.onerror = () => ws.close();
  }

  disconnect() {
    this.wanted = false;
    if (this.timer) clearTimeout(this.timer);
    this.ws?.close();
    this.ws = null;
  }

  private dispatch(e: LiveEvent) {
    for (const l of this.listeners) {
      if ((l.type === '*' || l.type === e.t) && (l.server === null || l.server === e.s)) {
        try {
          l.fn(e);
        } catch (err) {
          console.error(err);
        }
      }
    }
  }

  send(obj: object) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(obj));
  }

  /** Listen for events of a type (optionally for one server). Returns an unsubscribe function. */
  on(type: string, server: string | null, fn: (e: LiveEvent) => void): () => void {
    const l = { type, server, fn };
    this.listeners.add(l);
    return () => this.listeners.delete(l);
  }

  /** Subscribe to console output of a server (ref counted). */
  subscribeConsole(id: string): () => void {
    const n = this.consoleRefs.get(id) || 0;
    this.consoleRefs.set(id, n + 1);
    if (n === 0) this.send({ op: 'sub', s: id });
    return () => {
      const c = (this.consoleRefs.get(id) || 1) - 1;
      if (c <= 0) {
        this.consoleRefs.delete(id);
        this.send({ op: 'unsub', s: id });
      } else this.consoleRefs.set(id, c);
    };
  }

  command(id: string, cmd: string) {
    this.send({ op: 'cmd', s: id, cmd });
  }
}

export const live = new Live();
