import { describe, expect, test } from 'bun:test';
import { sentryScrubbingOptions } from './sentry';
import type { Breadcrumb } from '@sentry/react';

describe('sentry scrubbing options', () => {
  const { beforeBreadcrumb, beforeSend } = sentryScrubbingOptions();

  describe('beforeBreadcrumb', () => {
    test('drops a console breadcrumb carrying a Paddle payload with an email', () => {
      const breadcrumb: Breadcrumb = {
        category: 'console',
        level: 'log',
        message: 'Checkout completed:',
        data: {
          arguments: [
            {
              transactionId: 'txn_123',
              event: {
                name: 'checkout.completed',
                data: {
                  customer: { email: 'jane.doe@example.com' },
                  transaction_id: 'txn_123',
                },
              },
            },
          ],
        },
      };

      const result = beforeBreadcrumb(breadcrumb);

      expect(result).toBeNull();
    });

    test('drops a console breadcrumb carrying Plaid institution/account metadata', () => {
      const breadcrumb: Breadcrumb = {
        category: 'console',
        level: 'log',
        message: 'OAuth flow completed successfully:',
        data: {
          arguments: [
            {
              institution: { name: 'Chase', institution_id: 'ins_1' },
              accounts: [{ id: 'acc_1', name: 'Checking', mask: '0000' }],
            },
          ],
        },
      };

      const result = beforeBreadcrumb(breadcrumb);

      expect(result).toBeNull();
    });

    test('does not over-scrub a benign non-console breadcrumb', () => {
      const breadcrumb: Breadcrumb = {
        category: 'ui.click',
        level: 'info',
        message: 'button clicked',
        data: {
          method: 'GET',
          status_code: 200,
        },
      };

      const result = beforeBreadcrumb(breadcrumb);

      expect(result).not.toBeNull();
      expect(result?.category).toBe('ui.click');
      expect(result?.message).toBe('button clicked');
      expect(result?.data?.method).toBe('GET');
      expect(result?.data?.status_code).toBe(200);
    });

    test('strips query strings from a benign xhr breadcrumb url without dropping it', () => {
      const breadcrumb: Breadcrumb = {
        category: 'xhr',
        level: 'info',
        data: {
          url: 'https://api.boostedbudget.com/v1/accounts?account_id=acc_123',
          method: 'GET',
        },
      };

      const result = beforeBreadcrumb(breadcrumb);

      expect(result).not.toBeNull();
      expect(result?.data?.url).toBe('https://api.boostedbudget.com/v1/accounts');
    });
  });

  describe('beforeSend', () => {
    test('scrubs an Axios error with populated config, response.data, and a URL with a query string', () => {
      const event = {
        exception: {
          values: [{ type: 'AxiosError', value: 'Request failed with status code 500' }],
        },
        request: {
          url: 'https://api.boostedbudget.com/v1/accounts/acc_123?account_id=acc_123&token=secret',
        },
        extra: {
          axiosError: {
            message: 'Request failed with status code 500',
            config: {
              url: 'https://api.boostedbudget.com/v1/accounts?account_id=acc_123',
              headers: { Authorization: 'Bearer super-secret-token' },
            },
            response: {
              status: 500,
              data: {
                email: 'jane.doe@example.com',
                account_name: 'Checking',
                detail: 'Internal error',
              },
            },
          },
        },
      };

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {});

      expect(result).not.toBeNull();
      const extra = result?.extra as Record<string, unknown>;
      expect(extra.axiosError.response.data.email).toBe('[Redacted]');
      expect(extra.axiosError.response.data.account_name).toBe('[Redacted]');
      expect(extra.axiosError.config.headers.Authorization).toBe('[Redacted]');
      expect(extra.axiosError.config.url).toBe('https://api.boostedbudget.com/v1/accounts');
      expect(result?.request?.url).toBe('https://api.boostedbudget.com/v1/accounts/acc_123');
      // Non-PII diagnostic data must survive
      expect(extra.axiosError.response.status).toBe(500);
      expect(extra.axiosError.response.data.detail).toBe('Internal error');
    });

    test('scrubs denylisted properties from exception extra/contexts', () => {
      const event = {
        exception: { values: [{ type: 'Error', value: 'boom' }] },
        extra: { requestId: 'req-1', nickname: 'My Checking', status: 500 },
        contexts: {
          plaid: { institution_name: 'Chase', link_session_id: 'sess_abc' },
        },
      };

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {});

      const extra = result?.extra as Record<string, unknown>;
      const contexts = result?.contexts as Record<string, unknown>;
      expect(extra.nickname).toBe('[Redacted]');
      expect(extra.requestId).toBe('req-1');
      expect(extra.status).toBe(500);
      expect(contexts.plaid.institution_name).toBe('[Redacted]');
      expect(contexts.plaid.link_session_id).toBe('[Redacted]');
    });

    test('does not over-scrub a benign event with no sensitive fields', () => {
      const event = {
        message: 'Something went wrong',
        exception: { values: [{ type: 'Error', value: 'Network request failed' }] },
        extra: { step: 'checkout', accountCount: 3, requestId: 'req-42' },
      };

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {});

      expect(result).not.toBeNull();
      expect(result?.message).toBe('Something went wrong');
      expect(result?.exception?.values?.[0]?.value).toBe('Network request failed');
      const extra = result?.extra as Record<string, unknown>;
      expect(extra.step).toBe('checkout');
      expect(extra.accountCount).toBe(3);
      expect(extra.requestId).toBe('req-42');
    });

    test('redacts camelCase and PascalCase near-misses of denylisted PII tokens', () => {
      // Property names are split on camelCase boundaries (and _/-) into tokens;
      // any token that matches a denylisted word must be redacted. These are
      // real shapes found in apps/web/src (currentBalance 43x, institutionName
      // 38x, availableBalance 14x, merchantName 3x) — genuine PII, not benign
      // near-misses. `accountBalance`/`userEmail` were previously (wrongly)
      // asserted here as *surviving* redaction; that was the bug this test now
      // guards against.
      const event = {
        exception: { values: [{ type: 'Error', value: 'boom' }] },
        extra: {
          currentBalance: 100,
          institutionName: 'Chase',
          availableBalance: 200,
          merchantName: 'Starbucks',
          accountBalance: 500,
          userEmail: 'jane@example.com',
        },
      };

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {});

      const extra = result?.extra as Record<string, unknown>;
      expect(extra.currentBalance).toBe('[Redacted]');
      expect(extra.institutionName).toBe('[Redacted]');
      expect(extra.availableBalance).toBe('[Redacted]');
      expect(extra.merchantName).toBe('[Redacted]');
      expect(extra.accountBalance).toBe('[Redacted]');
      expect(extra.userEmail).toBe('[Redacted]');
    });

    test('does not over-scrub benign compounds that merely share a token with a denylisted word', () => {
      // `account` and `description` are deliberately NOT bare tokens on
      // PII_PROPERTY_DENYLIST (see piiScrubbing.ts) precisely because
      // token-split matching would otherwise redact these — a count and a
      // length are not PII values, even though they share a word with
      // `accountBalance`/`institutionName`-style PII fields.
      const event = {
        exception: { values: [{ type: 'Error', value: 'boom' }] },
        extra: {
          accountCount: 3,
          descriptionLength: 12,
          requestId: 'req-42',
          step: 'checkout',
        },
      };

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {});

      const extra = result?.extra as Record<string, unknown>;
      expect(extra.accountCount).toBe(3);
      expect(extra.descriptionLength).toBe(12);
      expect(extra.requestId).toBe('req-42');
      expect(extra.step).toBe('checkout');
    });

    test('redacts snake_case denylisted keys reached via token matching', () => {
      const event = {
        exception: { values: [{ type: 'Error', value: 'boom' }] },
        extra: { current_balance: 100, institution_name: 'Chase' },
      };

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {});

      const extra = result?.extra as Record<string, unknown>;
      expect(extra.current_balance).toBe('[Redacted]');
      expect(extra.institution_name).toBe('[Redacted]');
    });

    test('scrubs event.exception stack-frame vars by property name', () => {
      const event = {
        exception: {
          values: [
            {
              type: 'AxiosError',
              value: 'Request failed with status code 500',
              stacktrace: {
                frames: [
                  {
                    filename: 'app.js',
                    function: 'submitCheckout',
                    vars: {
                      email: 'jane.doe@example.com',
                      account_name: 'Checking',
                      step: 'checkout',
                    },
                  },
                ],
              },
            },
          ],
        },
      };

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {});

      const values = result?.exception?.values as Array<Record<string, unknown>>;
      const frame = values[0].stacktrace.frames[0];
      // The exception message itself is passed through unchanged — it is
      // never content-scanned, only redacted by property name like
      // everything else (see sentry.ts for the documented rationale).
      expect(values[0].value).toBe('Request failed with status code 500');
      expect(frame.vars.email).toBe('[Redacted]');
      expect(frame.vars.account_name).toBe('[Redacted]');
      expect(frame.vars.step).toBe('checkout');
      expect(frame.filename).toBe('app.js');
      expect(frame.function).toBe('submitCheckout');
    });

    test('scrubs PII carried directly on event.request (headers, cookies, data) not just its url', () => {
      // Regression test: event.request previously only had its `url` field
      // rewritten (query-stripped) by beforeSend; every other Sentry request
      // field — query_string, headers, cookies, data — passed through
      // untouched. This event puts PII under `request` itself (not `extra`),
      // which is exactly the shape the earlier Axios test above never
      // exercised (that test only ever put `url` under `request`).
      const event = {
        request: {
          url: 'https://api.example/accounts?account_id=acc_123',
          query_string: 'account_id=acc_123&token=secret',
          headers: { Authorization: 'Bearer secret', Cookie: 'session=abc' },
          cookies: { session: 'abc' },
          data: { account_name: 'Private Checking', currentBalance: 1234.56 },
        },
      };

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {});

      const request = result?.request as Record<string, unknown>;
      const headers = request.headers as Record<string, unknown>;
      const data = request.data as Record<string, unknown>;

      // url must still be query-stripped (pre-existing behaviour, must not regress)
      expect(request.url).toBe('https://api.example/accounts');
      // headers carrying auth/session secrets must be redacted
      expect(headers.Authorization).toBe('[Redacted]');
      expect(headers.Cookie).toBe('[Redacted]');
      // financial PII in request body data must be redacted
      expect(data.account_name).toBe('[Redacted]');
      expect(data.currentBalance).toBe('[Redacted]');
      // `query_string` and `cookies` are now bare PII_PROPERTY_DENYLIST
      // entries (see piiScrubbing.ts): `query_string`'s raw value can carry
      // account/session identifiers the same way a URL's query string can,
      // and `cookies` is a nested container whose inner keys (e.g. `session`)
      // don't themselves match a denylisted token, so the only way to close
      // it is to redact the whole container. These previously survived as a
      // documented scope limit; that gap is now closed.
      expect(request.query_string).toBe('[Redacted]');
      expect(request.cookies).toBe('[Redacted]');
    });
  });
});
