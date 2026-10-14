import { api } from './api';
import type { Meta, PanelInfo, Server, User } from './types';

class Session {
  user = $state<User | null>(null);
  panel = $state<PanelInfo>({ name: 'Hearth', version: '', publicUrl: '' });
  needsSetup = $state(false);
  twoFactor = $state(false);
  meta = $state<Meta | null>(null);
  ready = $state(false);

  async load() {
    try {
      const setup = await api.get<{ needsSetup: boolean; panel: PanelInfo }>('/api/setup');
      this.needsSetup = setup.needsSetup;
      this.panel = setup.panel;
      if (!setup.needsSetup) {
        const me = await api.get<{ user: User | null; twoFactor?: boolean; panel: PanelInfo }>('/api/auth/me');
        this.user = me.user;
        this.twoFactor = !!me.twoFactor;
        this.panel = me.panel;
        if (me.user) await this.loadMeta();
      }
    } finally {
      this.ready = true;
    }
  }

  async loadMeta() {
    this.meta = await api.get<Meta>('/api/meta');
  }

  setUser(u: User | null) {
    this.user = u;
    this.twoFactor = false;
  }

  async logout() {
    await api.post('/api/auth/logout').catch(() => {});
    this.user = null;
    servers.list = [];
  }
}

export const session = new Session();

class ServerStore {
  list = $state<Server[]>([]);
  loaded = $state(false);
  private pending: Promise<void> | null = null;

  load(): Promise<void> {
    if (this.pending) return this.pending;
    this.pending = api
      .get<Server[]>('/api/servers')
      .then((l) => {
        this.list = l;
        this.loaded = true;
      })
      .finally(() => (this.pending = null));
    return this.pending;
  }

  get(id: string): Server | undefined {
    return this.list.find((s) => s.id === id);
  }

  put(s: Server) {
    const i = this.list.findIndex((x) => x.id === s.id);
    if (i >= 0) this.list[i] = s;
    else this.list = [...this.list, s];
  }

  remove(id: string) {
    this.list = this.list.filter((s) => s.id !== id);
  }
}

export const servers = new ServerStore();

export function can(server: Server | undefined, perm: string): boolean {
  return !!server && server.permissions.includes(perm);
}

// ---- theme ----

export type ThemePref = 'system' | 'dark' | 'light';

export function themePref(): ThemePref {
  try {
    return (localStorage.getItem('hearth-theme') as ThemePref) || 'system';
  } catch {
    return 'system';
  }
}

export function applyTheme(pref: ThemePref = themePref()) {
  try {
    localStorage.setItem('hearth-theme', pref);
  } catch {}
  const dark = pref === 'dark' || (pref === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
}
