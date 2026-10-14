// Minimal history based router.

export interface Route {
  path: string;
  name: string;
  params: Record<string, string>;
  query: URLSearchParams;
}

type Pattern = { name: string; parts: string[] };

const patterns: Pattern[] = [];

export function defineRoutes(defs: Record<string, string>) {
  patterns.length = 0;
  for (const [name, path] of Object.entries(defs)) {
    patterns.push({ name, parts: path.split('/').filter(Boolean) });
  }
}

function match(pathname: string): { name: string; params: Record<string, string> } {
  const segs = pathname.split('/').filter(Boolean).map(decodeURIComponent);
  for (const p of patterns) {
    if (p.parts.length !== segs.length) continue;
    const params: Record<string, string> = {};
    let ok = true;
    for (let i = 0; i < p.parts.length; i++) {
      const part = p.parts[i];
      if (part.startsWith(':')) params[part.slice(1)] = segs[i];
      else if (part !== segs[i]) {
        ok = false;
        break;
      }
    }
    if (ok) return { name: p.name, params };
  }
  return { name: 'notfound', params: {} };
}

function current(): Route {
  const m = match(location.pathname);
  return { path: location.pathname, name: m.name, params: m.params, query: new URLSearchParams(location.search) };
}

class Router {
  route = $state<Route>({ path: '/', name: 'notfound', params: {}, query: new URLSearchParams() });

  init() {
    this.route = current();
    addEventListener('popstate', () => (this.route = current()));
    // intercept internal link clicks
    document.addEventListener('click', (e) => {
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      const a = (e.target as HTMLElement).closest('a');
      if (!a || a.target || a.hasAttribute('download') || a.dataset.external !== undefined) return;
      const href = a.getAttribute('href');
      if (!href || !href.startsWith('/') || href.startsWith('/api/') || href.startsWith('/s/')) return;
      e.preventDefault();
      this.go(href);
    });
  }

  go(path: string, replace = false) {
    if (path === location.pathname + location.search) return;
    if (replace) history.replaceState(null, '', path);
    else history.pushState(null, '', path);
    this.route = current();
    if (!replace) window.scrollTo(0, 0);
  }

  setQuery(params: Record<string, string | null>) {
    const q = new URLSearchParams(location.search);
    for (const [k, v] of Object.entries(params)) {
      if (v === null || v === '') q.delete(k);
      else q.set(k, v);
    }
    const s = q.toString();
    history.replaceState(null, '', location.pathname + (s ? '?' + s : ''));
    this.route = current();
  }
}

export const router = new Router();
export const go = (p: string, replace = false) => router.go(p, replace);
