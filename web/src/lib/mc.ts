// Minecraft specific labels and formatters shared by the panel and the public page.
import { distance, hours, num, num1, ago, date } from './format';

export interface StatCol {
  /** sort key understood by the API */
  key: string;
  /** field in the player object */
  field: string;
  label: string;
  short?: string;
  fmt: (v: number) => string;
}

export const hearts = (v: number) => `${num1((v || 0) / 20)} ♥`;

export const statCols: StatCol[] = [
  { key: 'playtime', field: 'playSeconds', label: 'Spielzeit', fmt: hours },
  { key: 'deaths', field: 'deaths', label: 'Tode', fmt: num },
  { key: 'mobKills', field: 'mobKills', label: 'Besiegte Kreaturen', short: 'Mob-Kills', fmt: num },
  { key: 'playerKills', field: 'playerKills', label: 'Besiegte Spieler', short: 'PvP-Kills', fmt: num },
  { key: 'blocksMined', field: 'blocksMined', label: 'Abgebaute Blöcke', short: 'Abgebaut', fmt: num },
  { key: 'distance', field: 'distanceCm', label: 'Zurückgelegte Strecke', short: 'Strecke', fmt: distance },
  { key: 'advancements', field: 'advancements', label: 'Fortschritte', fmt: num },
  { key: 'jumps', field: 'jumps', label: 'Sprünge', fmt: num },
  { key: 'damageDealt', field: 'damageDealt', label: 'Ausgeteilter Schaden', short: 'Schaden', fmt: hearts },
  { key: 'damageTaken', field: 'damageTaken', label: 'Erlittener Schaden', short: 'Erlitten', fmt: hearts },
  { key: 'itemsCrafted', field: 'itemsCrafted', label: 'Hergestellte Gegenstände', short: 'Hergestellt', fmt: num },
  { key: 'fishCaught', field: 'fishCaught', label: 'Gefangene Fische', short: 'Fische', fmt: num },
];

export const statByKey = Object.fromEntries(statCols.map((c) => [c.key, c]));

export function statValue(p: Record<string, any>, c: StatCol): number {
  return Number(p[c.field] ?? 0);
}

/** German wording for advancement kinds from the server log. */
export function advancementText(kind: string): { before: string; after: string } {
  switch (kind) {
    case 'completed':
      return { before: 'hat die Herausforderung', after: 'abgeschlossen' };
    case 'reached':
      return { before: 'hat das Ziel', after: 'erreicht' };
    default:
      return { before: 'hat den Fortschritt', after: 'erzielt' };
  }
}

export const flavorNames: Record<string, string> = {
  paper: 'Paper',
  purpur: 'Purpur',
  vanilla: 'Vanilla',
  fabric: 'Fabric',
  forge: 'Forge',
  neoforge: 'NeoForge',
  custom: 'Eigene JAR',
};

export function flavorLabel(flavor: string, version = ''): string {
  const n = flavorNames[flavor] ?? flavor;
  return version ? `${n} ${version}` : n;
}

/** "zuletzt gestern" / "online" helpers for player lists. */
export function seen(p: { online?: boolean; lastSeen: number }): string {
  if (p.online) return 'online';
  return ago(p.lastSeen);
}

export function since(unix: number): string {
  return unix ? date(unix) : '–';
}

export const dimLabels: Record<string, string> = {
  overworld: 'Oberwelt',
  the_nether: 'Nether',
  the_end: 'End',
};

/** Pretty Minecraft ids from stats ("diamond_ore" → "Diamond Ore"). */
export function itemName(id: string): string {
  return id
    .replace(/^minecraft:/, '')
    .split('_')
    .map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w))
    .join(' ');
}

/** Labels for the most interesting entries of the "custom" stats category. */
export const customStatLabels: [string, string, 'time' | 'distance' | 'count'][] = [
  ['time_since_death', 'Seit dem letzten Tod', 'time'],
  ['sleep_in_bed', 'Nächte geschlafen', 'count'],
  ['walk_one_cm', 'Gelaufen', 'distance'],
  ['sprint_one_cm', 'Gesprintet', 'distance'],
  ['swim_one_cm', 'Geschwommen', 'distance'],
  ['fly_one_cm', 'Geflogen', 'distance'],
  ['aviate_one_cm', 'Mit Elytren geflogen', 'distance'],
  ['boat_one_cm', 'Mit dem Boot', 'distance'],
  ['horse_one_cm', 'Geritten', 'distance'],
  ['minecart_one_cm', 'Mit der Lore', 'distance'],
  ['animals_bred', 'Tiere gezüchtet', 'count'],
  ['traded_with_villager', 'Mit Dorfbewohnern gehandelt', 'count'],
  ['enchant_item', 'Gegenstände verzaubert', 'count'],
  ['open_chest', 'Truhen geöffnet', 'count'],
  ['eat_cake_slice', 'Kuchenstücke gegessen', 'count'],
  ['raid_win', 'Überfälle gewonnen', 'count'],
  ['bell_ring', 'Glocken geläutet', 'count'],
  ['play_noteblock', 'Notenblöcke gespielt', 'count'],
];
