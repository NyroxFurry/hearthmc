// German formatting helpers.

const nf = new Intl.NumberFormat('de-DE');
const nf1 = new Intl.NumberFormat('de-DE', { maximumFractionDigits: 1 });

export const num = (n: number) => nf.format(Math.round(n || 0));
export const num1 = (n: number) => nf1.format(n || 0);

export function bytes(b: number, digits = 1): string {
  if (!b || b < 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (b >= 1024 && i < units.length - 1) {
    b /= 1024;
    i++;
  }
  return `${new Intl.NumberFormat('de-DE', { maximumFractionDigits: i === 0 ? 0 : digits }).format(b)} ${units[i]}`;
}

export function mb(m: number): string {
  if (m >= 1024) return `${num1(m / 1024)} GB`;
  return `${num(m)} MB`;
}

/** Duration in seconds → "3 Std. 12 Min." */
export function duration(sec: number, short = false): string {
  sec = Math.max(0, Math.floor(sec || 0));
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (short) {
    if (d > 0) return `${d} T ${h} Std`;
    if (h > 0) return `${h} Std ${m} Min`;
    if (m > 0) return `${m} Min`;
    return `${s} s`;
  }
  const parts: string[] = [];
  if (d) parts.push(`${d} ${d === 1 ? 'Tag' : 'Tage'}`);
  if (h) parts.push(`${h} Std.`);
  if (m && d === 0) parts.push(`${m} Min.`);
  if (!parts.length) parts.push(`${s} Sek.`);
  return parts.join(' ');
}

/** Hours with one decimal, e.g. playtime leaderboards. */
export function hours(sec: number): string {
  const h = sec / 3600;
  if (h < 1) return `${Math.round(sec / 60)} Min.`;
  return `${num1(h)} Std.`;
}

const rtf = new Intl.RelativeTimeFormat('de-DE', { numeric: 'auto' });

export function ago(unix: number): string {
  if (!unix) return 'nie';
  const diff = unix - Date.now() / 1000;
  const abs = Math.abs(diff);
  if (abs < 45) return 'gerade eben';
  if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour');
  if (abs < 86400 * 30) return rtf.format(Math.round(diff / 86400), 'day');
  return date(unix);
}

export function date(unix: number): string {
  if (!unix) return '–';
  return new Date(unix * 1000).toLocaleDateString('de-DE', { day: '2-digit', month: '2-digit', year: 'numeric' });
}

export function dateTime(unix: number): string {
  if (!unix) return '–';
  return new Date(unix * 1000).toLocaleString('de-DE', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function time(unix: number): string {
  if (!unix) return '–';
  return new Date(unix * 1000).toLocaleTimeString('de-DE', { hour: '2-digit', minute: '2-digit' });
}

export function distance(cm: number): string {
  const m = cm / 100;
  if (m >= 1000) return `${num1(m / 1000)} km`;
  return `${num(m)} m`;
}

export function pct(v: number): string {
  return `${num1(v)} %`;
}

/** "minecraft:diamond_ore" → "Diamond Ore" */
export function prettyId(id: string): string {
  return id
    .replace(/^minecraft:/, '')
    .split(/[_/]/)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ');
}
