export interface PIISentinel {
  value: string;
  fieldDescription: string;
}

export interface PIIViolation {
  text: string;
  path: string;
  matched: string;
  via: 'text' | 'aria-label' | 'aria-description' | 'title' | 'placeholder' | 'alt' | 'value';
}

// Covers: prefix symbols ($1,234 / -$45.99 / +$500 / €500), postfix codes (1,234 USD),
// prefix codes (USD 8.99), unicode minus (−$500), parenthetical negatives (($1,234)).
const CURRENCY_RE =
  /[-+]?\s?[$€£¥]\s?-?[\d,]+(?:\.\d+)?|[\d,]+(?:\.\d+)?\s?(?:USD|EUR|GBP|JPY)|(?:USD|EUR|GBP|JPY)\s?[\d,]+(?:\.\d+)?|−\s?[$€£¥]?[\d,]+(?:\.\d+)?|\([$€£¥]?[\d,]+(?:\.\d+)?\)/;

// Covers: •••• / **** / xxxx / ****-1234 / x-1234 followed by 3+ digits,
// "last4: 1234", and "ending in 1234" (card number display) patterns.
const ACCOUNT_MASK_RE =
  /(?:[•●⚫⬤*xX]){2,}[-\s]?\d{3,}|last[_-]?4\s*:?\s*\d{4}|ending\s+in\s+\d{4}/i;

const SCANNED_ATTRS = [
  'aria-label',
  'aria-description',
  'title',
  'placeholder',
  'alt',
  'value',
] as const;

function hasMaskedAncestor(node: Node): boolean {
  let cur: Node | null = node;
  while (cur) {
    if (cur instanceof Element && cur.hasAttribute('data-ph-mask')) return true;
    cur = cur.parentNode;
  }
  return false;
}

function pathTo(node: Node): string {
  const segs: string[] = [];
  let cur: Node | null = node;
  while (cur instanceof Element) {
    let seg = cur.tagName.toLowerCase();
    if (cur.id) seg += `#${cur.id}`;
    segs.unshift(seg);
    cur = cur.parentElement;
  }
  return segs.join(' > ');
}

function matchPII(text: string, sentinels: PIISentinel[]): string | undefined {
  if (CURRENCY_RE.test(text)) return 'currency';
  if (ACCOUNT_MASK_RE.test(text)) return 'account-mask';
  for (const s of sentinels) {
    if (text.includes(s.value)) return s.value;
  }
  return undefined;
}

export function scanForUnmaskedPII(
  container: HTMLElement,
  sentinels: PIISentinel[],
): PIIViolation[] {
  const out: PIIViolation[] = [];

  // Walk text nodes (TreeWalker does not cross Shadow DOM — acceptable; codebase uses light DOM).
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let node: Node | null;
  while ((node = walker.nextNode())) {
    const text = (node.textContent ?? '').trim();
    if (!text) continue;
    const matched = matchPII(text, sentinels);
    if (!matched) continue;
    if (hasMaskedAncestor(node)) continue;
    out.push({ text, path: pathTo(node.parentNode as Element), matched, via: 'text' });
  }

  // Scan attributes on every element (including container itself).
  const all = [container, ...Array.from(container.querySelectorAll('*'))];
  for (const elNode of all) {
    for (const attr of SCANNED_ATTRS) {
      const val = elNode.getAttribute(attr);
      if (!val) continue;
      const matched = matchPII(val, sentinels);
      if (!matched) continue;
      if (hasMaskedAncestor(elNode)) continue;
      out.push({ text: val, path: pathTo(elNode), matched, via: attr });
    }
  }

  return out;
}
