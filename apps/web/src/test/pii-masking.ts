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

/**
 * Derive PII sentinels from a fixture by walking every string leaf.
 *
 * WHY THIS EXISTS. The gate's denominator used to be a hand-written list of sentinel VALUES
 * per component: adding a field to a fixture did nothing unless someone also remembered to add
 * it to the sentinel array, and which fields had been skipped was invisible. That is exactly
 * how WebhookDetailModal sat inside the gate while rendering `processing_error` unmasked.
 *
 * This inverts the default. Every string leaf in the fixture is a sentinel unless its key is
 * explicitly opted OUT — because it is structural (ids, enums, timestamps) or because it must
 * stay legible to an operator (an attester id, an event type). Opting a field out is a visible,
 * reviewable line of code; forgetting to opt a field IN is no longer possible.
 *
 * `skipKeys` entries match either a leaf key name (`event_type`) or a full dotted path
 * (`data.customer.email`). Matching is exact, not by prefix.
 *
 * ⚠ What this still cannot catch is recorded at the call sites and in the batch report: a field
 * absent from the fixture, a component nobody imported, a value the component reformats before
 * rendering (so string inclusion misses it), and attribute text the component constructs rather
 * than copying from the fixture.
 */
export function sentinelsFromFixture(
  fixture: unknown,
  options: { skipKeys?: readonly string[]; minLength?: number; label?: string } = {},
): PIISentinel[] {
  const skip = new Set(options.skipKeys ?? []);
  const minLength = options.minLength ?? 3;
  const label = options.label ? `${options.label}.` : '';
  const seen = new Set<string>();
  const out: PIISentinel[] = [];

  const walk = (value: unknown, path: string, key: string) => {
    if (skip.has(key) || (path && skip.has(path))) return;
    if (typeof value === 'string') {
      if (value.trim().length < minLength) return;
      if (seen.has(value)) return;
      seen.add(value);
      out.push({ value, fieldDescription: `${label}${path || key}` });
      return;
    }
    if (Array.isArray(value)) {
      value.forEach((item, i) => walk(item, `${path}[${i}]`, key));
      return;
    }
    if (value && typeof value === 'object') {
      for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
        walk(v, path ? `${path}.${k}` : k, k);
      }
    }
  };

  walk(fixture, '', '');
  return out;
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
  // NOTE: data-ph-mask masks DOM text content only; DOM attributes are recorded verbatim
  // by PostHog session replay (posthog-pii-masking.md:8,18), so we don't skip masked ancestors.
  // Exception: `value` on form controls is masked via maskAllInputs:true in posthog.ts.
  const formControlTags = new Set(['INPUT', 'TEXTAREA', 'SELECT']);
  for (const elNode of [container, ...Array.from(container.querySelectorAll('*'))]) {
    for (const attr of SCANNED_ATTRS) {
      if (attr === 'value' && formControlTags.has(elNode.tagName)) continue;
      const val = elNode.getAttribute(attr);
      if (!val) continue;
      const matched = matchPII(val, sentinels);
      if (!matched) continue;
      out.push({ text: val, path: pathTo(elNode), matched, via: attr });
    }
  }

  return out;
}
