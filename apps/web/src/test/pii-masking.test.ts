import { describe, expect, test } from 'bun:test';
import { scanForUnmaskedPII, type PIISentinel } from './pii-masking';

function el(
  tag: string,
  attrs: Record<string, string> = {},
  ...children: (Node | string)[]
): HTMLElement {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  for (const c of children) {
    node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  return node;
}

describe('scanForUnmaskedPII', () => {
  test('flags positive currency', () => {
    const root = el('div', {}, el('span', {}, '$1,234.56'));
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
  });

  test('flags negative currency variants', () => {
    for (const text of ['-$1,234', '−$500', '($1,234)']) {
      const root = el('div', {}, el('span', {}, text));
      expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
    }
  });

  test('flags postfix currency code', () => {
    const root = el('div', {}, el('span', {}, '1,234.56 USD'));
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
  });

  test('flags single-digit currency', () => {
    const root = el('div', {}, el('span', {}, '$5'));
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
  });

  test('passes when currency has masked ancestor', () => {
    const inner = el('i', {}, '$1,234.56');
    const middle = el('span', {}, inner);
    const root = el('div', { 'data-ph-mask': '' }, middle);
    expect(scanForUnmaskedPII(root, [])).toEqual([]);
  });

  test('flags account-mask bullet variant', () => {
    const root = el('div', {}, el('span', {}, '••••1234'));
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
  });

  test('flags account-mask asterisk variant', () => {
    const root = el('div', {}, el('span', {}, '****1234'));
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
  });

  test('flags account-mask xxxx variant', () => {
    const root = el('div', {}, el('span', {}, 'xxxx1234'));
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
  });

  test('flags caller sentinel substring', () => {
    const root = el('div', {}, 'Hello SENTINEL_X world');
    const sentinels: PIISentinel[] = [{ value: 'SENTINEL_X', fieldDescription: 'merchant_name' }];
    expect(scanForUnmaskedPII(root, sentinels).length).toBeGreaterThan(0);
  });

  test('flags PII in aria-label even when text content is masked', () => {
    const root = el(
      'div',
      { 'aria-label': '$1,234' },
      el('span', { 'data-ph-mask': '' }, '$1,234'),
    );
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
  });

  test('flags PII in title, placeholder, alt attrs', () => {
    const img = document.createElement('img');
    img.setAttribute('alt', '$500 balance graph');
    const input = document.createElement('input');
    input.setAttribute('placeholder', '$1,000');
    const root = el('div', { title: '$700' }, img, input);
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThanOrEqual(3);
  });

  test('does not flag aggregate counts or currency codes', () => {
    const root = el('div', {}, '3 accounts • USD • Food and Drink');
    expect(scanForUnmaskedPII(root, [])).toEqual([]);
  });

  test('does not flag standalone percentage', () => {
    const root = el('div', {}, el('span', {}, '+12.5%'));
    expect(scanForUnmaskedPII(root, [])).toEqual([]);
  });

  test('$0 matches currency regex (document expected behavior)', () => {
    const root = el('div', {}, el('span', {}, '$0'));
    expect(scanForUnmaskedPII(root, []).length).toBeGreaterThan(0);
  });

  test('flags PII in aria-label under a masked ancestor (attribute recorded verbatim)', () => {
    const button = el('button', { 'aria-label': 'Transfer $1,234 to savings' }, 'Confirm')
    const root = el('div', { 'data-ph-mask': '' }, button)
    const violations = scanForUnmaskedPII(root, [])
    expect(violations.length).toBeGreaterThan(0)
    expect(violations.some((v) => v.via === 'aria-label')).toBe(true)
  })

  test('flags sentinel in title attribute under a masked ancestor', () => {
    const inner = el('div', { title: 'SENTINEL_ATTR_ZX' }, 'safe text')
    const root = el('div', { 'data-ph-mask': '' }, inner)
    const sentinels = [{ value: 'SENTINEL_ATTR_ZX', fieldDescription: 'account_name' }]
    const violations = scanForUnmaskedPII(root, sentinels)
    expect(violations.length).toBeGreaterThan(0)
    expect(violations[0].via).toBe('title')
  })

  test('does not flag value attribute on form controls (covered by maskAllInputs:true)', () => {
    for (const tag of ['input', 'textarea', 'select']) {
      const formEl = document.createElement(tag)
      formEl.setAttribute('value', '$1,234')
      const root = el('div', {}, formEl)
      expect(scanForUnmaskedPII(root, [])).toEqual([])
    }
  })

  // Round 2 (#1514 fix round): SCANNED_ATTRS was a hand-curated allowlist
  // ('aria-label', 'aria-description', 'title', 'placeholder', 'alt', 'value') that
  // structurally could not see PII written into a `data-*` attribute — exactly where
  // DataTable's `data-row-id={rowKey(row)}` put a raw merchant name. A curated list only
  // ever knows about attribute names someone remembered to add; this closes that gap by
  // scanning every attribute except a documented safe list, rather than adding one more
  // name to the same kind of list that caused the miss.
  test('flags PII in a data-* attribute (issue #1514 round 2)', () => {
    const root = el('div', {}, el('tr', { 'data-row-id': 'SENTINEL_MERCHANT_XYZ' }, 'safe text'))
    const sentinels: PIISentinel[] = [
      { value: 'SENTINEL_MERCHANT_XYZ', fieldDescription: 'merchant_name in data-row-id' },
    ]
    const violations = scanForUnmaskedPII(root, sentinels)
    expect(violations.length).toBeGreaterThan(0)
    expect(violations.some((v) => v.via === 'data-row-id')).toBe(true)
  })

  test('flags currency in an arbitrary data-* attribute even with no named sentinel', () => {
    const root = el('div', {}, el('span', { 'data-tooltip': 'Balance: $1,234.56' }, 'safe text'))
    const violations = scanForUnmaskedPII(root, [])
    expect(violations.length).toBeGreaterThan(0)
    expect(violations.some((v) => v.via === 'data-tooltip')).toBe(true)
  })

  test('does not flag name-excluded structural attributes (class, id, role, type)', () => {
    const root = el('img', {
      class: 'avatar-image',
      id: 'avatar-1',
      role: 'img',
      type: 'button',
    })
    expect(scanForUnmaskedPII(root, [])).toEqual([])
  })

  // Round 3 (#1514 fix round 3): href/src used to be excluded BY NAME on the theory that
  // they only ever hold opaque ids/routes — but a name-based exclusion doesn't know a
  // future href/src carrying real PII from one that doesn't. They're now scanned like any
  // other attribute; this passes because the VALUE (an opaque Clerk-style resource URL,
  // matching MobileNav/UserDropdown/MembersList's real avatar `src`) doesn't match any of
  // matchPII's shapes — not because the attribute name is exempt.
  test('src/href with opaque, non-PII-shaped values pass (scanned, not excluded)', () => {
    const root = el('img', {
      src: 'https://img.clerk.com/eyJhbGciOiJub25lIn0.opaque-token',
      href: '#',
    })
    expect(scanForUnmaskedPII(root, [])).toEqual([])
  })

  test('flags a sentinel value carried in href', () => {
    const root = el('a', { href: '/search?q=SENTINEL_MERCHANT_HREF' }, 'Search')
    const sentinels: PIISentinel[] = [
      { value: 'SENTINEL_MERCHANT_HREF', fieldDescription: 'merchant_name in href' },
    ]
    const violations = scanForUnmaskedPII(root, sentinels)
    expect(violations.length).toBeGreaterThan(0)
    expect(violations.some((v) => v.via === 'href')).toBe(true)
  })

  test('flags a sentinel value carried in src', () => {
    const root = el('img', { src: 'https://example.com/avatars/SENTINEL_NAME_SRC.png' })
    const sentinels: PIISentinel[] = [
      { value: 'SENTINEL_NAME_SRC', fieldDescription: 'user.name in src' },
    ]
    const violations = scanForUnmaskedPII(root, sentinels)
    expect(violations.length).toBeGreaterThan(0)
    expect(violations.some((v) => v.via === 'src')).toBe(true)
  })
});
