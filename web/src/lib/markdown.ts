import { escapeHtml } from './mctext';

/** Tiny, safe markdown subset: paragraphs, line breaks, **bold**, *italic*, `code`, [links](https://…). */
export function markdown(src: string): string {
  if (!src) return '';
  const paras = escapeHtml(src.trim()).split(/\n{2,}/);
  return paras
    .map((p) => {
      let h = p
        .replace(/`([^`]+)`/g, '<code>$1</code>')
        .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
        .replace(/\*([^*]+)\*/g, '<em>$1</em>')
        .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>')
        .replace(/\n/g, '<br>');
      return `<p>${h}</p>`;
    })
    .join('');
}
