// Farbsorten: eine kräftige Farbe für Punkte,
// Kacheln und Buttons und ein tiefer Grund für das farbige Band. Jeder Server
// (und seine öffentliche Seite) trägt eine Sorte; das Panel selbst Minze.

export interface Flavor {
  name: string;
  /** kräftige Farbe */
  color: string;
  /** tiefer Grund für das Band */
  bg: string;
  /** Schrift auf der kräftigen Farbe */
  ink: string;
}

const dark = '#101214';

export const flavors: Flavor[] = [
  { name: 'Minze', color: '#3fd6a4', bg: '#0e3b30', ink: dark },
  { name: 'Beere', color: '#e0457b', bg: '#3e1024', ink: dark },
  { name: 'Ozean', color: '#3aa7f0', bg: '#0d2a47', ink: dark },
  { name: 'Zitrus', color: '#f7b32b', bg: '#3d2a06', ink: dark },
  { name: 'Lavendel', color: '#a98bf0', bg: '#251a45', ink: dark },
  { name: 'Orange', color: '#ff8a4c', bg: '#40190a', ink: dark },
  { name: 'Limette', color: '#a6d83a', bg: '#26330b', ink: dark },
  { name: 'Neutral', color: '#dce3e8', bg: '#1e2428', ink: dark },
];

export const brandFlavor = flavors[0];

// Farben der früheren Palette auf die passende Sorte abbilden.
const legacy: Record<string, number> = {
  '#4fae94': 0,
  '#d16a8f': 1,
  '#e5584f': 1,
  '#5a8fd6': 2,
  '#b7a03c': 3,
  '#9d88e6': 4,
  '#e08a4c': 5,
  '#c4734f': 5,
  '#5fa95a': 6,
};

function parseHex(hex: string): [number, number, number] | null {
  const m = /^#?([0-9a-f]{6}|[0-9a-f]{3})$/i.exec(hex.trim());
  if (!m) return null;
  let h = m[1];
  if (h.length === 3) h = [...h].map((c) => c + c).join('');
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

function toHex(rgb: number[]) {
  return '#' + rgb.map((v) => Math.max(0, Math.min(255, Math.round(v))).toString(16).padStart(2, '0')).join('');
}

/** relative Helligkeit nach WCAG */
export function luminance(hex: string) {
  const rgb = parseHex(hex);
  if (!rgb) return 0;
  const [r, g, b] = rgb.map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function hash(s: string) {
  let h = 0;
  for (const c of s) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return h;
}

/** Sorte zu einer gespeicherten Farbe. Ohne Farbe wählt `seed` (z. B. die Server-ID) eine feste Sorte. */
export function flavorFor(color?: string | null, seed = ''): Flavor {
  const c = (color || '').trim().toLowerCase();
  if (!c) return seed ? flavors[hash(seed) % (flavors.length - 1)] : brandFlavor;
  const known = flavors.find((f) => f.color === c);
  if (known) return known;
  if (c in legacy) return flavors[legacy[c]];
  const rgb = parseHex(c);
  if (!rgb) return brandFlavor;
  // eigene Farbe: Grund wie bei den Sorten aus der Farbe abdunkeln
  return { name: 'Eigene Farbe', color: c, bg: toHex(rgb.map((v) => v * 0.26)), ink: luminance(c) > 0.2 ? dark : '#ffffff' };
}

/** CSS-Variablen für ein Element, das die Sorte trägt. */
export function flavorVars(f: Flavor) {
  return `--flavor:${f.color};--band:${f.bg};--on-flavor:${f.ink}`;
}
