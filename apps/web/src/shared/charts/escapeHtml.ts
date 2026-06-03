const ESCAPE_MAP: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

export const escapeHtml = (s: string): string =>
  s.replace(/[&<>"']/g, (c) => ESCAPE_MAP[c] ?? c)
