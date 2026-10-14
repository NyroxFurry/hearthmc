// Human readable German descriptions for common cron expressions.

const days = ['Sonntag', 'Montag', 'Dienstag', 'Mittwoch', 'Donnerstag', 'Freitag', 'Samstag'];

const pad = (n: string) => n.padStart(2, '0');
const isNum = (s: string) => /^\d+$/.test(s);

function dayList(f: string): string | null {
  if (f === '1-5') return 'werktags';
  if (f === '0,6' || f === '6,0') return 'am Wochenende';
  if (isNum(f)) return `jeden ${days[Number(f) % 7]}`;
  if (/^\d(,\d)+$/.test(f)) {
    const names = f.split(',').map((d) => days[Number(d) % 7]);
    return names.slice(0, -1).join(', ') + ' und ' + names[names.length - 1];
  }
  if (/^\d-\d$/.test(f)) {
    const [a, b] = f.split('-').map(Number);
    return `${days[a % 7]} bis ${days[b % 7]}`;
  }
  return null;
}

export function describeCron(expr: string): string {
  const e = expr.trim();
  const desc: Record<string, string> = {
    '@yearly': 'Einmal im Jahr (1. Januar, 0:00)',
    '@annually': 'Einmal im Jahr (1. Januar, 0:00)',
    '@monthly': 'Am 1. jedes Monats um 0:00',
    '@weekly': 'Sonntags um 0:00',
    '@daily': 'Täglich um 0:00',
    '@midnight': 'Täglich um 0:00',
    '@hourly': 'Stündlich',
  };
  if (desc[e]) return desc[e];
  if (e.startsWith('@every ')) return 'Alle ' + e.slice(7);
  const f = e.split(/\s+/);
  if (f.length !== 5) return e;
  const [min, hour, dom, mon, dow] = f;
  const allDays = dom === '*' && mon === '*';

  if (min.startsWith('*/') && hour === '*' && allDays && dow === '*') return `Alle ${min.slice(2)} Minuten`;
  if (min === '*' && hour === '*' && allDays && dow === '*') return 'Jede Minute';
  if (isNum(min) && hour === '*' && allDays && dow === '*') return min === '0' ? 'Stündlich zur vollen Stunde' : `Stündlich um :${pad(min)}`;
  if (isNum(min) && hour.startsWith('*/') && allDays && dow === '*') return `Alle ${hour.slice(2)} Stunden${min !== '0' ? ` (zur Minute ${min})` : ''}`;

  if (isNum(min) && /^\d+(,\d+)*$/.test(hour)) {
    const times = hour.split(',').map((h) => `${h}:${pad(min)}`);
    const at = times.length > 1 ? times.slice(0, -1).join(', ') + ' und ' + times[times.length - 1] : times[0];
    if (allDays && dow === '*') return `Täglich um ${at}`;
    if (allDays) {
      const d = dayList(dow);
      if (d) return `${d.charAt(0).toUpperCase() + d.slice(1)} um ${at}`;
    }
    if (isNum(dom) && mon === '*' && dow === '*') return `Am ${dom}. jedes Monats um ${at}`;
  }
  return e;
}

export const cronPresets: { label: string; expr: string }[] = [
  { label: 'Stündlich', expr: '0 * * * *' },
  { label: 'Alle 6 Stunden', expr: '0 */6 * * *' },
  { label: 'Täglich 4:00', expr: '0 4 * * *' },
  { label: 'Täglich 12:00', expr: '0 12 * * *' },
  { label: 'Werktags 6:00', expr: '0 6 * * 1-5' },
  { label: 'Sonntags 3:00', expr: '0 3 * * 0' },
];
