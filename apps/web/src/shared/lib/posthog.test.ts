import { describe, expect, test } from 'bun:test';
import { posthogConfig } from './posthog';

describe('posthog config', () => {
  test('autocapture is disabled', () => {
    const cfg = posthogConfig();
    expect(cfg.autocapture).toBe(false);
  });

  test('before_send strips known PII event properties', () => {
    const cfg = posthogConfig();
    const event = {
      event: 'spending_report_drilldown',
      properties: {
        merchant: 'Starbucks',
        merchant_name: 'Starbucks',
        merchant_id: 'abc-123',
        user_id: 'u1',
        nickname: 'My Checking',
        institution: 'Chase',
        institution_name: 'Chase Bank',
        // Not denylisted (see piiScrubbing.ts): a bare `description` entry
        // would false-positive on `descriptionLength`-style metadata under
        // the shared tokenizing matcher. The `data-ph-mask` cell masking in
        // AutoCategorizationRuleTable is the actual defense against leaking
        // description content via replay.
        description: 'Coffee',
        account_name: 'Checking',
        balance: 1234.56,
        amount: 42.0,
        current_balance: 5000,
        target_amount: 10000,
        current_amount: 3000,
        total_saved: 500,
      },
    };
    const out = cfg.before_send?.(event as Parameters<NonNullable<typeof cfg.before_send>>[0]);
    // PII properties must be stripped
    expect(out?.properties?.merchant).toBeUndefined();
    expect(out?.properties?.merchant_name).toBeUndefined();
    expect(out?.properties?.nickname).toBeUndefined();
    expect(out?.properties?.institution).toBeUndefined();
    expect(out?.properties?.institution_name).toBeUndefined();
    expect(out?.properties?.account_name).toBeUndefined();
    expect(out?.properties?.balance).toBeUndefined();
    expect(out?.properties?.amount).toBeUndefined();
    expect(out?.properties?.current_balance).toBeUndefined();
    expect(out?.properties?.target_amount).toBeUndefined();
    expect(out?.properties?.current_amount).toBeUndefined();
    expect(out?.properties?.total_saved).toBeUndefined();
    // merchant_id is now redacted too: before_send used to delete by exact
    // literal key match, which let `merchant_id` (containing the `merchant`
    // token, but not itself a denylist entry, and not an exact match either)
    // survive. Now that before_send shares Sentry's tokenizing `isPiiKey`
    // matcher, `merchant_id` tokenizes to ['merchant', 'id'] and the
    // `merchant` token matches — the same as it always has for Sentry. One
    // matcher, one contract, beats two divergent ones; no current call site
    // passes `merchant_id` into analytics properties, so this closes a real
    // (if latent) gap rather than opening one.
    expect(out?.properties?.merchant_id).toBeUndefined();
    // Non-PII properties must be preserved
    expect(out?.properties?.user_id).toBe('u1');
    expect(out?.properties?.description).toBe('Coffee');
  });

  test('before_send redacts camelCase PII near-misses via the same tokenizing matcher Sentry uses', () => {
    // Regression test: before_send used to delete properties by exact,
    // case-sensitive literal key match against PII_PROPERTY_DENYLIST, so
    // camelCase fields sailed straight through even though Sentry's
    // `redactPii` caught the same fields via token matching. Now both paths
    // share `isPiiKey`.
    const cfg = posthogConfig();
    const event = {
      event: 'account_viewed',
      properties: {
        currentBalance: 100,
        institutionName: 'Chase',
        accountBalance: 500,
      },
    };
    const out = cfg.before_send?.(event as Parameters<NonNullable<typeof cfg.before_send>>[0]);
    expect(out?.properties?.currentBalance).toBeUndefined();
    expect(out?.properties?.institutionName).toBeUndefined();
    expect(out?.properties?.accountBalance).toBeUndefined();
  });

  test('before_send passes event through when no PII properties present', () => {
    const cfg = posthogConfig();
    const event = {
      event: 'page_viewed',
      properties: { page: '/dashboard', user_id: 'u1' },
    };
    const out = cfg.before_send?.(event as Parameters<NonNullable<typeof cfg.before_send>>[0]);
    expect(out?.properties?.page).toBe('/dashboard');
    expect(out?.properties?.user_id).toBe('u1');
  });
});
