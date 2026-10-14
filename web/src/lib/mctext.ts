// Renders Minecraft § formatting codes as safe HTML.

const colors: Record<string, string> = {
  '0': '#000000', '1': '#0000aa', '2': '#00aa00', '3': '#00aaaa', '4': '#aa0000', '5': '#aa00aa',
  '6': '#ffaa00', '7': '#aaaaaa', '8': '#555555', '9': '#5555ff', a: '#55ff55', b: '#55ffff',
  c: '#ff5555', d: '#ff55ff', e: '#ffff55', f: '#ffffff',
};

export function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
}

export function mcToHtml(text: string): string {
  if (!text) return '';
  let out = '';
  let color = '';
  let bold = false,
    italic = false,
    underline = false,
    strike = false;
  let buf = '';
  const flush = () => {
    if (!buf) return;
    const style: string[] = [];
    if (color) style.push(`color:${color}`);
    if (bold) style.push('font-weight:700');
    if (italic) style.push('font-style:italic');
    const deco = [underline && 'underline', strike && 'line-through'].filter(Boolean).join(' ');
    if (deco) style.push(`text-decoration:${deco}`);
    const html = escapeHtml(buf).replace(/\n/g, '<br>');
    out += style.length ? `<span style="${style.join(';')}">${html}</span>` : html;
    buf = '';
  };
  const chars = [...text];
  for (let i = 0; i < chars.length; i++) {
    const c = chars[i];
    if (c === '§' && i + 1 < chars.length) {
      const code = chars[i + 1].toLowerCase();
      // hex color: §x§r§r§g§g§b§b
      if (code === 'x' && i + 13 < chars.length) {
        flush();
        let hex = '#';
        for (let k = 0; k < 6; k++) hex += chars[i + 3 + k * 2];
        if (/^#[0-9a-fA-F]{6}$/.test(hex)) color = hex;
        i += 13;
        continue;
      }
      flush();
      if (colors[code]) {
        color = colors[code];
        bold = italic = underline = strike = false;
      } else if (code === 'l') bold = true;
      else if (code === 'o') italic = true;
      else if (code === 'n') underline = true;
      else if (code === 'm') strike = true;
      else if (code === 'r') {
        color = '';
        bold = italic = underline = strike = false;
      }
      i++;
      continue;
    }
    buf += c;
  }
  flush();
  return out;
}

export function stripCodes(text: string): string {
  return (text || '').replace(/§./g, '');
}
