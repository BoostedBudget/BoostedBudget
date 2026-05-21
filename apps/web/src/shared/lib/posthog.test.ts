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
    expect(out?.properties?.description).toBeUndefined();
    expect(out?.properties?.account_name).toBeUndefined();
    expect(out?.properties?.balance).toBeUndefined();
    expect(out?.properties?.amount).toBeUndefined();
    expect(out?.properties?.current_balance).toBeUndefined();
    expect(out?.properties?.target_amount).toBeUndefined();
    expect(out?.properties?.current_amount).toBeUndefined();
    expect(out?.properties?.total_saved).toBeUndefined();
    // Non-PII properties must be preserved
    expect(out?.properties?.merchant_id).toBe('abc-123');
    expect(out?.properties?.user_id).toBe('u1');
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
