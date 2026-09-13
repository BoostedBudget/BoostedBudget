import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { CaptureResult, PostHogConfig } from 'posthog-js';
import {
  capturePageView,
  clearPostHogQueuedEvents,
  guardPostHogRetryQueue,
  isAnonymousAnalyticsPath,
  isSensitiveAnalyticsPath,
  listenForAnalyticsWithdrawal,
  posthogConfig,
  resetAnalyticsLifecycleForTests,
  sanitizeAnalyticsUrl,
  sanitizeCapturedNetworkRequest,
} from './posthog';
import {
  createAnalyticsCaptureGate,
  createProductAnalyticsLifecycle,
  type AnalyticsClient,
} from './analyticsLifecycle';
import * as posthogModule from './posthog';
import { mockPosthogCapture } from '@/test/mocks/posthog';

const minimumPostHogVersion = [1, 430, 2] as const;

function applyBeforeSend(config: Partial<PostHogConfig>, event: CaptureResult): CaptureResult | null {
  const beforeSend = config.before_send;
  if (typeof beforeSend !== 'function') throw new Error('before_send must be configured');
  return beforeSend(event);
}

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
  test('withdrawal prevents stale retries while a later authority generation can retry', () => {
    let generation = 1;
    let granted = true;
    const retryQueue = {
      _isPolling: true,
      _queue: [] as unknown[],
      _enqueue(request: unknown) { this._queue.push(request) },
      retriableRequest(request: unknown) { this._enqueue(request) },
    };

    guardPostHogRetryQueue({ _retryQueue: retryQueue }, () => generation, () => granted);
    const staleRequest = { event: 'obsolete-authority-event' };
    retryQueue.retriableRequest(staleRequest);
    clearPostHogQueuedEvents({ _retryQueue: retryQueue });
    generation += 1;
    granted = false;
    retryQueue._enqueue(staleRequest);

    expect(retryQueue._queue).toEqual([]);
    expect(retryQueue._isPolling).toBe(false);

    granted = true;
    retryQueue.retriableRequest({ event: 'new-authority-event' });
    expect(retryQueue._queue).toHaveLength(1);
  });

  test('fails loudly when the initialized SDK retry queue is incompatible', () => {
    expect(() => guardPostHogRetryQueue({}, () => 1, () => true))
      .toThrow(/initialized PostHog retry queue/i);
    expect(() => guardPostHogRetryQueue({ _retryQueue: { _enqueue: () => {} } }, () => 1, () => true))
      .toThrow(/retriableRequest/i);
  });

  test.each(['/pricing/', '/Pricing', '/legal/privacy-policy/'])(
    'recognizes router-equivalent anonymous public route %s',
    route => {
      expect(isAnonymousAnalyticsPath(route)).toBe(true);
    },
  );

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

  test('has no eager initialization or reload-derived auth/privacy captures in runtime code', () => {
    const srcRoot = resolve(import.meta.dir, '../..');
    const main = readFileSync(resolve(srcRoot, 'main.tsx'), 'utf8');
    const runtime = [
      readFileSync(resolve(srcRoot, 'App.tsx'), 'utf8'),
      readFileSync(resolve(srcRoot, 'shared/hooks/usePostHogIdentify.ts'), 'utf8'),
      readFileSync(resolve(srcRoot, 'features/profile/PrivacySettings.tsx'), 'utf8'),
    ].join('\n');

    expect(main).not.toMatch(/\binitPostHog\s*\(/);
    expect(runtime).not.toMatch(/SIGN_(?:IN|OUT|UP)_COMPLETED/);
    expect(runtime).not.toContain('PRIVACY_SETTINGS_CHANGED');
  });

  test('broadcasts a signed-out denial so every open tab stops analytics', () => {
    const app = readFileSync(resolve(import.meta.dir, '../../App.tsx'), 'utf8');

    expect(app).toMatch(/if \(consents\.analytics\) grantProductAnalytics\('anonymous'\)\s+else withdrawProductAnalytics\('anonymous'\)/);
  });

  test('autocapture is disabled', () => {
    const cfg = posthogConfig();
    expect(cfg.autocapture).toBe(false);
  });

  test('uses the exact fail-closed product configuration', () => {
    const cfg = posthogConfig();

    expect({
      opt_out_capturing_by_default: cfg.opt_out_capturing_by_default,
      opt_out_persistence_by_default: cfg.opt_out_persistence_by_default,
      respect_dnt: cfg.respect_dnt,
      capture_pageview: cfg.capture_pageview,
      capture_pageleave: cfg.capture_pageleave,
      autocapture: cfg.autocapture,
      capture_exceptions: cfg.capture_exceptions,
      disable_session_recording: cfg.disable_session_recording,
      disable_surveys: cfg.disable_surveys,
      disable_surveys_automatic_display: cfg.disable_surveys_automatic_display,
      disable_conversations: cfg.disable_conversations,
      disable_web_experiments: cfg.disable_web_experiments,
      disable_product_tours: cfg.disable_product_tours,
      disable_external_dependency_loading: cfg.disable_external_dependency_loading,
      capture_heatmaps: cfg.capture_heatmaps,
      capture_dead_clicks: cfg.capture_dead_clicks,
      disable_scroll_properties: cfg.disable_scroll_properties,
      save_referrer: cfg.save_referrer,
      save_campaign_params: cfg.save_campaign_params,
      custom_campaign_params: cfg.custom_campaign_params,
      advanced_disable_flags: cfg.advanced_disable_flags,
      persistence: cfg.persistence,
      reuseAnonymousId: cfg.reuseAnonymousId,
      request_batching: cfg.request_batching,
      disable_persistence: cfg.disable_persistence,
    }).toEqual({
      opt_out_capturing_by_default: false,
      opt_out_persistence_by_default: false,
      respect_dnt: true,
      capture_pageview: false,
      capture_pageleave: false,
      autocapture: false,
      capture_exceptions: false,
      disable_session_recording: true,
      disable_surveys: true,
      disable_surveys_automatic_display: true,
      disable_conversations: true,
      disable_web_experiments: true,
      disable_product_tours: true,
      disable_external_dependency_loading: true,
      capture_heatmaps: false,
      capture_dead_clicks: false,
      disable_scroll_properties: true,
      save_referrer: false,
      save_campaign_params: false,
      custom_campaign_params: [],
      advanced_disable_flags: true,
      persistence: 'sessionStorage',
      reuseAnonymousId: true,
      request_batching: false,
      disable_persistence: true,
    });
  });

  test('before_send rejects every event while the injected local capture gate is denied', () => {
    const cfg = posthogConfig(() => false);
    expect(applyBeforeSend(cfg, { event: 'dashboard_viewed', properties: {} } as CaptureResult)).toBeNull();
  });

  test('before_send strips known PII event properties', () => {
    const cfg = posthogConfig(() => true);
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
    const out = applyBeforeSend(cfg, event as unknown as CaptureResult);
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
    const cfg = posthogConfig(() => true);
    const event = {
      event: 'account_viewed',
      properties: {
        currentBalance: 100,
        institutionName: 'Chase',
        accountBalance: 500,
      },
    };
    const out = applyBeforeSend(cfg, event as unknown as CaptureResult);
    expect(out?.properties?.currentBalance).toBeUndefined();
    expect(out?.properties?.institutionName).toBeUndefined();
    expect(out?.properties?.accountBalance).toBeUndefined();
  });

  test('before_send passes event through when no PII properties present', () => {
    const cfg = posthogConfig(() => true);
    const event = {
      event: 'page_viewed',
      properties: { page: '/dashboard', user_id: 'u1' },
    };
    const out = applyBeforeSend(cfg, event as unknown as CaptureResult);
    expect(out?.properties?.page).toBe('/dashboard');
    expect(out?.properties?.user_id).toBe('u1');
  });

  test('before_send recursively removes PII and URL/referrer sentinels without mutating input', () => {
    const cfg = posthogConfig(() => true);
    const event = {
      event: 'page_viewed',
      properties: {
        $current_url: 'https://app.example/accounts/acc-secret?email=sentinel#token',
        $pathname: '/budgets/budget-secret?sentinel=yes#hidden',
        $referrer: 'https://search.example/?q=sentinel',
        $initial_referrer: 'https://search.example/?q=initial-sentinel',
        $session_entry_url: 'https://app.example/accounts/session-account-sentinel?query=session-query-sentinel#session-hash-sentinel',
        $session_entry_pathname: '/accounts/session-account-sentinel',
        $session_entry_referrer: 'https://search.example/?q=session-referrer-sentinel',
        utm_source: 'https://campaign.example/?secret=sentinel',
        nested: {
          userEmail: 'nested@example.com',
          safe: 'kept',
        },
        $set: {
          accountName: 'Secret checking',
          safe_tier: 'premium',
          $current_url: '/boosts/boost-secret?sentinel=yes',
        },
      },
    };
    const original = structuredClone(event);

    const out = applyBeforeSend(cfg, event as unknown as CaptureResult);

    expect(event).toEqual(original);
    expect(out?.properties?.$current_url).toBe('https://app.example/accounts/:accountId');
    expect(out?.properties?.$pathname).toBe('/budgets/:budgetId');
    expect(out?.properties?.$referrer).toBeUndefined();
    expect(out?.properties?.$initial_referrer).toBeUndefined();
    expect(out?.properties?.$session_entry_url).toBe('https://app.example/accounts/:accountId');
    expect(out?.properties?.$session_entry_pathname).toBe('/accounts/:accountId');
    expect(out?.properties?.$session_entry_referrer).toBeUndefined();
    expect(out?.properties?.utm_source).toBeUndefined();
    expect((out?.properties?.nested as Record<string, unknown>).userEmail).toBeUndefined();
    expect((out?.properties?.nested as Record<string, unknown>).safe).toBe('kept');
    expect((out?.properties?.$set as Record<string, unknown>).accountName).toBeUndefined();
    expect((out?.properties?.$set as Record<string, unknown>).safe_tier).toBe('premium');
    expect((out?.properties?.$set as Record<string, unknown>).$current_url).toBe('/boosts/:id');
    expect(JSON.stringify(out)).not.toContain('sentinel');
  });

  test('before_send sanitizes cyclic objects without recursion failure or input mutation', () => {
    const cfg = posthogConfig(() => true);
    const nested: Record<string, unknown> = { safe: 'kept', email: 'secret@example.com' };
    nested.self = nested;
    const event = { event: 'cyclic_event', properties: nested };

    const out = applyBeforeSend(cfg, event as unknown as CaptureResult);

    expect(out?.properties?.safe).toBe('kept');
    expect(out?.properties?.email).toBeUndefined();
    expect(out?.properties?.self).toBeNull();
    expect(() => JSON.stringify(out)).not.toThrow();
    expect(nested.email).toBe('secret@example.com');
    expect(nested.self).toBe(nested);
  });

  test('before_send preserves SDK Date metadata while cloning the event', () => {
    const cfg = posthogConfig(() => true);
    const timestamp = new Date('2026-09-13T12:00:00.000Z');
    const event = { event: 'sdk_event', properties: {}, timestamp };

    const out = applyBeforeSend(cfg, event as unknown as CaptureResult);

    expect(out?.timestamp).toEqual(timestamp);
    expect(out?.timestamp).not.toBe(timestamp);
  });

  test.each([
    '/callback?code=secret',
    '/accounts/oauth-callback#state=secret',
    '/billing/manage?customer=secret',
    '/billing/success?checkout=secret',
    '/cancel-deletion/token-secret',
    '/households/invites/token-secret',
    '/household/invite/token-secret',
    '/user/delete/cancel/token-secret',
  ])('drops pageviews for sensitive route %s', (url) => {
    expect(isSensitiveAnalyticsPath(url)).toBe(true);
    mockPosthogCapture.mockClear();
    capturePageView(url);
    expect(mockPosthogCapture).not.toHaveBeenCalled();
  });

  test.each([
    ['/accounts/acc-secret?sentinel=yes#hash', '/accounts/:accountId'],
    ['/budgets/budget-secret?sentinel=yes', '/budgets/:budgetId'],
    ['/boosts/boost-secret/edit#sentinel', '/boosts/:id/edit'],
    ['https://app.example/transactions?merchant=sentinel#hash', 'https://app.example/transactions'],
  ])('sanitizes %s as %s', (url, expected) => {
    expect(sanitizeAnalyticsUrl(url)).toBe(expected);
  });

  test('sanitizes replay request/page URLs and removes all headers and bodies', () => {
    const request = {
      name: 'https://api.example/api/v1/accounts/acc-secret?sentinel=yes#hash',
      entryType: 'resource',
      startTime: 1,
      duration: 2,
      requestHeaders: { authorization: 'Bearer sentinel' },
      responseHeaders: { 'set-cookie': 'sentinel' },
      requestBody: 'sentinel-request',
      responseBody: 'sentinel-response',
    };

    const out = sanitizeCapturedNetworkRequest(request);

    expect(out).not.toBe(request);
    expect(out.name).toBe('https://api.example/api/v1/accounts/:accountId');
    expect(out.requestHeaders).toBeUndefined();
    expect(out.responseHeaders).toBeUndefined();
    expect(out.requestBody).toBeUndefined();
    expect(out.responseBody).toBeUndefined();
    expect(request.requestHeaders.authorization).toBe('Bearer sentinel');
  });

  test.each([
    [
      'https://api.example/api/v1/households/invites/invite-token-sentinel/accept?code=query-sentinel',
      'https://api.example/api/v1/households/invites/[redacted]/accept',
    ],
    [
      'https://api.example/api/v1/user/delete/cancel/deletion-token-sentinel#hash-sentinel',
      'https://api.example/api/v1/user/delete/cancel/[redacted]',
    ],
  ])('redacts capability tokens from replay network URL %s', (name, expected) => {
    const out = sanitizeCapturedNetworkRequest({
      name,
      entryType: 'resource',
      startTime: 1,
      duration: 2,
      requestHeaders: { authorization: 'Bearer sentinel' },
      requestBody: 'body-sentinel',
    });

    expect(out.name).toBe(expected);
    expect(out.requestHeaders).toBeUndefined();
    expect(out.requestBody).toBeUndefined();
  });
});

describe('posthog lifecycle', () => {
  let key: string | undefined;
  let dnt: boolean;
  let client: AnalyticsClient;
  let captureGate: ReturnType<typeof createAnalyticsCaptureGate>;
  let lifecycle: ReturnType<typeof createProductAnalyticsLifecycle>;

  beforeEach(() => {
    resetAnalyticsLifecycleForTests();
    window.ENV = { ...window.ENV, POSTHOG_KEY: 'phc_test' };
    key = 'phc_test';
    dnt = false;
    client = {
      capture: mock(() => {}),
      clearLegacyConsent: mock(() => {}),
      clearPersistence: mock(() => {}),
      clearQueuedEvents: mock(() => {}),
      identify: mock(() => {}),
      init: mock(() => {}),
      reset: mock(() => {}),
      setPersistenceEnabled: mock(() => {}),
      setPersonProperties: mock(() => {}),
      stopSessionRecording: mock(() => {}),
    };
    captureGate = createAnalyticsCaptureGate();
    lifecycle = createProductAnalyticsLifecycle({
      captureGate,
      client,
      config: () => posthogConfig(captureGate.isGranted),
      getHost: () => 'https://example.test',
      getKey: () => key,
      isDoNotTrackEnabled: () => dnt,
    });
  });

  test('does nothing while pending and initializes idempotently only after grant', () => {
    lifecycle.capture('dashboard_viewed');
    expect(client.init).not.toHaveBeenCalled();
    expect(client.capture).not.toHaveBeenCalled();
    expect(client.identify).not.toHaveBeenCalled();
    expect(client.clearLegacyConsent).not.toHaveBeenCalled();

    lifecycle.grant('user-123', { subscription_tier: 'premium' });
    lifecycle.grant('user-123', { subscription_tier: 'premium' });

    expect(client.init).toHaveBeenCalledTimes(1);
    expect(client.clearLegacyConsent).toHaveBeenCalledTimes(1);
    expect(captureGate.isGranted()).toBe(true);
    expect(lifecycle.getState()).toBe('granted');
  });

  test('preserves the unconfigured no-op', () => {
    key = undefined;
    expect(lifecycle.init()).toBe(false);
    lifecycle.grant('user-123');
    expect(client.init).not.toHaveBeenCalled();
    expect(client.identify).not.toHaveBeenCalled();
    expect(lifecycle.getState()).toBe('denied');
  });

  test('DNT is a browser-only hard stop', () => {
    dnt = true;
    lifecycle.grant('user-123');
    expect(client.init).not.toHaveBeenCalled();
    expect(lifecycle.getState()).toBe('denied');
  });

  test('withdrawal closes the local gate before stopping recording and resetting tab state', () => {
    const order: string[] = [];
    const setGranted = captureGate.setGranted;
    captureGate.setGranted = mock((granted: boolean) => {
      order.push(`gate:${granted}`);
      setGranted(granted);
    });
    client.clearQueuedEvents = mock(() => { order.push('clear'); });
    client.clearPersistence = mock(() => { order.push('clear-persistence'); });
    client.stopSessionRecording = mock(() => { order.push('stop'); });
    client.reset = mock(() => { order.push('reset'); });
    client.setPersistenceEnabled = mock((enabled: boolean) => { order.push(`persistence:${enabled}`); });
    lifecycle.grant('user-123');
    order.length = 0;

    lifecycle.deny();

    expect(order).toEqual(['gate:false', 'persistence:false', 'clear-persistence', 'clear', 'stop', 'reset']);
    expect(client.reset).toHaveBeenCalledWith(true);
    expect(lifecycle.getState()).toBe('denied');
  });

  test('pending and denied clear stale SDK storage before first initialization', () => {
    lifecycle.setScope('user-a');
    lifecycle.setPending();
    lifecycle.deny();

    expect(client.init).not.toHaveBeenCalled();
    expect(client.clearPersistence).toHaveBeenCalledTimes(2);
  });

  test('pending disables persistence before any reset and grant restores it before identity', () => {
    const order: string[] = [];
    client.clearLegacyConsent = mock(() => { order.push('clear-legacy-consent'); });
    client.setPersistenceEnabled = mock((enabled: boolean) => { order.push(`persistence:${enabled}`); });
    client.clearPersistence = mock(() => { order.push('clear-persistence'); });
    client.identify = mock(() => { order.push('identify'); });
    lifecycle.setScope('user-a');
    lifecycle.grantFromAuthority('user-a', 'user-a');
    order.length = 0;

    lifecycle.setPending();
    expect(order).toEqual(['persistence:false', 'clear-persistence']);

    lifecycle.grantFromAuthority('user-a', 'user-a');
    expect(order).toEqual([
      'persistence:false',
      'clear-persistence',
      'clear-legacy-consent',
      'persistence:true',
      'identify',
    ]);
  });

  test('separate tab lifecycles do not share native consent or identity state', () => {
    const tabBClient: AnalyticsClient = {
      capture: mock(() => {}),
      clearLegacyConsent: mock(() => {}),
      clearPersistence: mock(() => {}),
      identify: mock(() => {}),
      init: mock(() => {}),
      reset: mock(() => {}),
      setPersistenceEnabled: mock(() => {}),
      setPersonProperties: mock(() => {}),
      stopSessionRecording: mock(() => {}),
    };
    const tabBGate = createAnalyticsCaptureGate();
    const tabB = createProductAnalyticsLifecycle({
      captureGate: tabBGate,
      client: tabBClient,
      config: () => posthogConfig(tabBGate.isGranted),
      getHost: () => 'https://example.test',
      getKey: () => key,
      isDoNotTrackEnabled: () => false,
    });
    lifecycle.setScope('user-a');
    lifecycle.grantFromAuthority('user-a', 'user-a');
    tabB.setScope('user-b');
    tabB.grantFromAuthority('user-b', 'user-b');

    lifecycle.withdraw('user-a');
    tabB.capture('still_enabled');

    expect(tabBGate.isGranted()).toBe(true);
    expect(tabBClient.reset).not.toHaveBeenCalled();
    expect(tabBClient.capture).toHaveBeenCalledWith('still_enabled', undefined);
    expect(client).not.toHaveProperty('opt_in_capturing');
    expect(client).not.toHaveProperty('opt_out_capturing');
  });

  test('test authority reset can preserve one physical SDK initialization', () => {
    const order: string[] = [];
    client.setPersistenceEnabled = mock(() => { order.push('disable-persistence'); });
    client.clearPersistence = mock(() => { order.push('clear-persistence'); });
    client.clearQueuedEvents = mock(() => { order.push('clear-queues'); });
    client.stopSessionRecording = mock(() => { order.push('stop-recording'); });
    client.clearLegacyConsent = mock(() => { order.push('clear-legacy-consent'); });
    client.reset = mock(() => { order.push('reset'); });
    lifecycle.setScope('user-a');
    lifecycle.grantFromAuthority('user-a', 'user-a');
    (client.init as ReturnType<typeof mock>).mockClear();
    order.length = 0;

    lifecycle.resetForTests(true);
    lifecycle.setScope('user-b');
    lifecycle.grantFromAuthority('user-b', 'user-b');

    expect(client.init).not.toHaveBeenCalled();
    expect(client.reset).toHaveBeenCalledWith(true);
    expect(order).toEqual([
      'disable-persistence',
      'clear-persistence',
      'clear-queues',
      'stop-recording',
      'clear-legacy-consent',
      'reset',
      'clear-legacy-consent',
      'disable-persistence',
    ]);
  });

  test('a cross-tab withdrawal latches denial against stale authority and cleans up', () => {
    const withdraw = mock((_scope: string) => {});
    const cleanup = listenForAnalyticsWithdrawal(withdraw);

    window.dispatchEvent(new StorageEvent('storage', {
      key: 'boostedbudget:analytics-withdrawal',
      newValue: JSON.stringify({ scope: 'user-123', nonce: 1 }),
    }));
    expect(withdraw).toHaveBeenCalledWith('user-123');

    cleanup();
    window.dispatchEvent(new StorageEvent('storage', {
      key: 'boostedbudget:analytics-withdrawal',
      newValue: JSON.stringify({ scope: 'user-123', nonce: 2 }),
    }));
    expect(withdraw).toHaveBeenCalledTimes(1);
  });

  test('exposes identity-scoped authority and withdrawal transitions', () => {
    const scoped = lifecycle as unknown as Record<string, unknown>;

    expect(scoped.setScope).toBeFunction();
    expect(scoped.grantFromAuthority).toBeFunction();
    expect(scoped.grantExplicitly).toBeFunction();
    expect(scoped.withdraw).toBeFunction();
    expect(scoped.subscribe).toBeFunction();
  });

  test('exposes scoped browser lifecycle integration points', () => {
    expect(posthogModule.setProductAnalyticsScope).toBeFunction();
    expect(posthogModule.applyProductAnalyticsAuthority).toBeFunction();
    expect(posthogModule.subscribeToAnalyticsLifecycle).toBeFunction();
  });

  test('failed backend withdrawal stays denied when stale server authority refetches true', () => {
    lifecycle.setScope('user-a');
    lifecycle.grantFromAuthority('user-a', 'user-a');
    lifecycle.withdraw('user-a');
    expect(lifecycle.getSnapshot().generation).toBe(2);

    lifecycle.setPending();
    lifecycle.grantFromAuthority('user-a', 'user-a');
    expect(lifecycle.getState()).toBe('denied');

    lifecycle.grantExplicitly('user-a', 'user-a');
    expect(lifecycle.getState()).toBe('granted');
  });

  test('ignores late authority responses from a torn-down identity generation', () => {
    lifecycle.setScope('user-a');
    lifecycle.grantFromAuthority('user-a', 'user-a');
    lifecycle.setScope('user-b');

    lifecycle.grantFromAuthority('user-a', 'user-a');
    expect(lifecycle.getState()).toBe('pending');
    expect(client.identify).toHaveBeenCalledTimes(1);

    lifecycle.grantFromAuthority('user-b', 'user-b');
    expect(lifecycle.getState()).toBe('granted');
    expect(client.identify).toHaveBeenLastCalledWith('user-b', undefined);
  });

  test('notifies subscribers only when authority newly transitions to granted', () => {
    const states: Array<{ state: string; scope: string | null; generation: number }> = [];
    lifecycle.setScope('anonymous');
    const cleanup = lifecycle.subscribe(state => states.push(state));

    lifecycle.grantFromAuthority('anonymous');
    lifecycle.grantFromAuthority('anonymous');
    expect(states).toEqual([{ state: 'granted', scope: 'anonymous', generation: 1 }]);

    cleanup();
    lifecycle.withdraw('anonymous');
    lifecycle.grantExplicitly('anonymous');
    expect(states).toEqual([{ state: 'granted', scope: 'anonymous', generation: 1 }]);
  });

  test('includes the new identity scope in pending and granted notifications', () => {
    const snapshots: Array<{ state: string; scope: string | null; generation: number }> = [];
    lifecycle.subscribe(snapshot => snapshots.push(snapshot));

    lifecycle.setScope('user-a');
    lifecycle.grantFromAuthority('user-a', 'user-a');
    lifecycle.setScope('user-b');

    expect(snapshots).toContainEqual({ state: 'granted', scope: 'user-a', generation: 1 });
    expect(snapshots).toContainEqual({ state: 'pending', scope: 'user-b', generation: 2 });
  });

  test('identifies a signed-in grant before notifying pageview subscribers', () => {
    const order: string[] = [];
    client.identify = mock(() => { order.push('identify'); });
    lifecycle.setScope('user-a');
    lifecycle.subscribe(() => { order.push('pageview'); });

    lifecycle.grantFromAuthority('user-a', 'user-a');

    expect(order).toEqual(['identify', 'pageview']);
  });
});
