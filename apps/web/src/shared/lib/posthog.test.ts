import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { posthogConfig } from './posthog';

const minimumPostHogVersion = [1, 430, 2] as const;

function numericVersion(version: string): number[] {
  if (!/^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(version)) {
    throw new Error(`invalid stable SemVer: ${version}`);
  }
  return version.split('.').map(Number);
}

function isAtLeast(version: number[], minimum: readonly number[]): boolean {
  for (const [index, part] of version.entries()) {
    if (part !== minimum[index]) {
      return part > minimum[index];
    }
  }
  return true;
}

describe('posthog config', () => {
  test.each([
    ['1.430.1', false],
    ['1.430.2', true],
    ['1.430.3', true],
    ['1.431.0', true],
    ['2.0.0', true],
  ])('compares %s against the supported floor', (version, expected) => {
    expect(isAtLeast(numericVersion(version), minimumPostHogVersion)).toBe(expected);
  });

  test.each(['01.430.2', '1.0430.2', '1.430.02'])('rejects leading-zero SemVer %s', (version) => {
    expect(() => numericVersion(version)).toThrow(/invalid stable SemVer/);
  });

  test('uses an exact stable posthog-js version at or above the supported floor', () => {
    const packageJson = JSON.parse(
      readFileSync(resolve(import.meta.dir, '../../../package.json'), 'utf8'),
    ) as { dependencies: Record<string, string> };
    const declaredVersion = packageJson.dependencies['posthog-js'];

    expect(declaredVersion).toMatch(/^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/);
    expect(isAtLeast(numericVersion(declaredVersion), minimumPostHogVersion)).toBe(true);
  });

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
        institution_id: 'inst-456',
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
    // merchant_id must SURVIVE (issue #1514, defect 1): it's the sanctioned
    // trackEvent pattern this doc's "what NOT to do" section recommends —
    // `{ merchant_id: uuid }` instead of `{ merchant: name }` — and a prior
    // round of this unification wrongly pinned it as redacted, which would
    // have silently deleted every engineer's compliant analytics call.
    // `isPiiKey` allowlists exactly `merchant_id`/`institution_id` (and their
    // camelCase forms) by name — see piiScrubbing.ts's `ID_ALLOWLIST` and its
    // isPiiKey doc comment; a broader "any key ending in id" version of this
    // carve-out was tried and reverted for being a privacy regression (issue
    // #1514, defect 2).
    expect(out?.properties?.merchant_id).toBe('abc-123');
    // Same allowlist applies to institution_id.
    expect(out?.properties?.institution_id).toBe('inst-456');
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
