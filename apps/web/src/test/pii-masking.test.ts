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
});
