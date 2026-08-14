import { describe, expect, test } from 'bun:test';
import { sentryScrubbingOptions } from './sentry';
import type { Breadcrumb } from '@sentry/react';

describe('sentry scrubbing options', () => {
  const { beforeBreadcrumb, beforeSend, beforeSendTransaction, beforeSendSpan } = sentryScrubbingOptions();

  // Issue #1514 (fix round 6): the account-deletion cancellation token is a bearer
  // CAPABILITY embedded as a raw URL PATH SEGMENT (not a query param) — see
  // backend/internal/huma/operations/deletion.go's CancelDeletion (public endpoint,
  // "the token validates the request"). `stripQueryString` never touched path segments,
  // so the token previously survived beforeSend/beforeBreadcrumb intact. These are RED
  // against the OLD `stripQueryString`-only behaviour and prove the fix closes all three
  // paths the token can reach: event.request.url, an xhr breadcrumb's data.url, and a
  // nested Axios config/response object under event.extra.
  const CANCEL_TOKEN = 'eyJhbGciOiJIUzI1NiJ9.SENTINEL_CANCEL_TOKEN.xyz'

  describe('capability token in URL path (#1514 fix round 6)', () => {
    test('redacts the token from event.request.url', () => {
      const event = {
        request: { url: `https://app.example.com/api/v1/user/delete/cancel/${CANCEL_TOKEN}` },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      expect(result?.request?.url).not.toContain(CANCEL_TOKEN)
      expect(result?.request?.url).toBe('https://app.example.com/api/v1/user/delete/cancel/[redacted]')
    })

    test('redacts the token from an xhr breadcrumb data.url', () => {
      const breadcrumb: Breadcrumb = {
        category: 'xhr',
        level: 'info',
        data: {
          url: `https://app.example.com/api/v1/user/delete/cancel/${CANCEL_TOKEN}`,
          method: 'GET',
        },
      }

      const result = beforeBreadcrumb(breadcrumb)

      expect(result?.data?.url).not.toContain(CANCEL_TOKEN)
      expect(result?.data?.url).toBe('https://app.example.com/api/v1/user/delete/cancel/[redacted]')
    })

    test('redacts the token from a nested Axios config.url under event.extra', () => {
      const event = {
        exception: { values: [{ type: 'AxiosError', value: 'Request failed with status code 400' }] },
        extra: {
          axiosError: {
            config: { url: `https://app.example.com/api/v1/user/delete/cancel/${CANCEL_TOKEN}` },
            response: { status: 400, config: { url: `https://app.example.com/api/v1/user/delete/cancel/${CANCEL_TOKEN}` } },
          },
        },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      const extra = result?.extra as {
        axiosError: { config: { url: string }; response: { config: { url: string } } }
      }
      expect(extra.axiosError.config.url).not.toContain(CANCEL_TOKEN)
      expect(extra.axiosError.response.config.url).not.toContain(CANCEL_TOKEN)
      expect(extra.axiosError.config.url).toBe('https://app.example.com/api/v1/user/delete/cancel/[redacted]')
    })

    test('redacts a household-invite token the same way', () => {
      const event = {
        request: { url: `https://app.example.com/api/v1/households/invites/${CANCEL_TOKEN}/accept` },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      expect(result?.request?.url).not.toContain(CANCEL_TOKEN)
      expect(result?.request?.url).toBe('https://app.example.com/api/v1/households/invites/[redacted]/accept')
    })
  })

  // Issue #1514 (fix round 7): round 6 only covered the API path shape
  // (`/api/v1/user/delete/cancel/{token}`). The actual exposure is the FRONTEND route the
  // emailed link points at (`/cancel-deletion/{token}`, App.tsx) — the token lands in the
  // PAGE URL once a user clicks the email, so it reaches event.request.url (the current
  // page, not an API call), the Referer header, breadcrumb messages, and exception
  // values/stack-frame text — none of which the round-6 fix touched.
  describe('capability token in the PAGE URL, not just an API call URL (#1514 fix round 7)', () => {
    test('redacts the token when event.request.url IS the cancel-deletion page itself', () => {
      const event = {
        request: { url: `https://app.example.com/cancel-deletion/${CANCEL_TOKEN}` },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      expect(result?.request?.url).not.toContain(CANCEL_TOKEN)
      expect(result?.request?.url).toBe('https://app.example.com/cancel-deletion/[redacted]')
    })

    test('redacts the token from a Referer header carrying the page URL', () => {
      const event = {
        request: {
          headers: { Referer: `https://app.example.com/cancel-deletion/${CANCEL_TOKEN}` },
        },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      const headers = result?.request?.headers as Record<string, string>
      expect(headers.Referer).not.toContain(CANCEL_TOKEN)
      expect(headers.Referer).toBe('https://app.example.com/cancel-deletion/[redacted]')
    })

    test('redacts the token from a breadcrumb message (never touched by beforeBreadcrumb before)', () => {
      const breadcrumb: Breadcrumb = {
        category: 'navigation',
        level: 'info',
        message: `Navigated to https://app.example.com/cancel-deletion/${CANCEL_TOKEN}`,
      }

      const result = beforeBreadcrumb(breadcrumb)

      expect(result?.message).not.toContain(CANCEL_TOKEN)
      expect(result?.message).toBe('Navigated to https://app.example.com/cancel-deletion/[redacted]')
    })

    test('redacts the token from an exception value (message) carrying the page URL', () => {
      const event = {
        exception: {
          values: [
            {
              type: 'Error',
              value: `Failed to load https://app.example.com/cancel-deletion/${CANCEL_TOKEN}`,
            },
          ],
        },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      expect(result?.exception?.values?.[0]?.value).not.toContain(CANCEL_TOKEN)
      expect(result?.exception?.values?.[0]?.value).toBe(
        'Failed to load https://app.example.com/cancel-deletion/[redacted]'
      )
    })

    test('redacts the token from the legacy /household/invite/{token} alias route', () => {
      const event = {
        request: { url: `https://app.example.com/household/invite/${CANCEL_TOKEN}` },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      expect(result?.request?.url).not.toContain(CANCEL_TOKEN)
      expect(result?.request?.url).toBe('https://app.example.com/household/invite/[redacted]')
    })
  })

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

  // Issue #1514 (fix round 9): `tracesSampleRate: 0.1` + `Sentry.browserTracingIntegration()`
  // means ~1 in 10 page views of a capability-bearing route (e.g. /cancel-deletion/{token})
  // emit a PERFORMANCE TRANSACTION event — a completely separate pipeline from
  // `beforeSend`, which is error/message-only and never sees transaction events at all.
  // No `beforeSendTransaction` existed before this round, so every field browser tracing
  // populates went out unredacted regardless of everything fixed in rounds 6-8.
  //
  // The event/span shapes below are NOT guessed — they were captured by actually running
  // `@sentry/react`'s `browserTracingIntegration()` against a real pageload at
  // `/cancel-deletion/{token}` (via a custom transport that records the envelope instead of
  // sending it) and inspecting the real output. The token showed up in FOUR places:
  //   - `event.transaction` (the transaction "name" — literally the pathname)
  //   - `event.contexts.trace.data['url.path']` and `['url.full']`
  //   - `event.request.url` (same field `beforeSend` handles for error events, but
  //     transaction events go through a separate hook that never touched it before)
  //   - the pageload root span's own `description` and `data['url.path']`/`['url.full']`,
  //     reachable via the separate `beforeSendSpan` hook (called per-span, not per-event)
  describe('capability token in performance transactions (#1514 fix round 9)', () => {
    test('redacts the token from event.transaction, contexts.trace.data, and request.url', () => {
      const event = {
        type: 'transaction',
        transaction: `/cancel-deletion/${CANCEL_TOKEN}`,
        contexts: {
          trace: {
            op: 'pageload',
            origin: 'auto.pageload.browser',
            data: {
              'sentry.origin': 'auto.pageload.browser',
              'sentry.op': 'pageload',
              'url.path': `/cancel-deletion/${CANCEL_TOKEN}`,
              'url.full': `http://app.example.com/cancel-deletion/${CANCEL_TOKEN}`,
            },
          },
        },
        request: {
          url: `http://app.example.com/cancel-deletion/${CANCEL_TOKEN}`,
        },
        transaction_info: { source: 'url' },
      }

      const result = beforeSendTransaction(event as Parameters<typeof beforeSendTransaction>[0], {})

      expect(result?.transaction).not.toContain(CANCEL_TOKEN)
      expect(result?.transaction).toBe('/cancel-deletion/[redacted]')

      const traceData = (result?.contexts as Record<string, { data: Record<string, string> }> | undefined)?.trace.data
      expect(traceData?.['url.path']).not.toContain(CANCEL_TOKEN)
      expect(traceData?.['url.full']).not.toContain(CANCEL_TOKEN)

      expect(result?.request?.url).not.toContain(CANCEL_TOKEN)
      expect(result?.request?.url).toBe('http://app.example.com/cancel-deletion/[redacted]')
    })

    test('redacts the token from a child span (fetch/xhr) description and data via beforeSendSpan', () => {
      const span = {
        description: `GET /households/invites/${CANCEL_TOKEN}/accept`,
        op: 'http.client',
        span_id: 'be2b2580bf3910d9',
        trace_id: 'c7354c615a484009a21cf9c2d4d9c729',
        start_timestamp: 0,
        timestamp: 1,
        origin: 'auto.http.browser',
        data: {
          'url.path': `/households/invites/${CANCEL_TOKEN}/accept`,
          'url.full': `http://app.example.com/api/v1/households/invites/${CANCEL_TOKEN}/accept`,
          'http.method': 'GET',
        },
      }

      const result = beforeSendSpan(span as Parameters<typeof beforeSendSpan>[0])

      expect(result.description).not.toContain(CANCEL_TOKEN)
      expect(result.data?.['url.path']).not.toContain(CANCEL_TOKEN)
      expect(result.data?.['url.full']).not.toContain(CANCEL_TOKEN)
      // Non-URL span fields must survive untouched.
      expect(result.op).toBe('http.client')
      expect(result.data?.['http.method']).toBe('GET')
    })

    // Issue #1514 (fix round 10): a second empirical SDK probe — extending the round-9
    // probe to inject the fields a transaction event can ALSO carry (tags, measurements,
    // user, sdkProcessingMetadata, breadcrumbs, server_name) — found the token surviving
    // in `tags.route` specifically (browser tracing sets route-shaped tags), confirmed by
    // a real transport capturing the actual envelope. `event.tags` was never touched by
    // round 9's `beforeSendTransaction`. This test covers every field the probe checked,
    // not just the one named, per the reviewer's explicit instruction not to fix only the
    // cited field.
    test('redacts the token from tags, user, sdkProcessingMetadata, breadcrumbs, and server_name — not just tags', () => {
      const event = {
        type: 'transaction',
        transaction: `/cancel-deletion/${CANCEL_TOKEN}`,
        tags: { route: `/cancel-deletion/${CANCEL_TOKEN}`, custom: 'foo' },
        measurements: { lcp: { value: 123, unit: 'millisecond' } },
        user: { id: 'user_1', email: `user+${CANCEL_TOKEN}@example.com` },
        // `server_name` is a Node-SDK-flavored field the browser SDK realistically never
        // populates with a URL — this value is deliberately shaped like a capability route
        // (not a realistic hostname) to prove the defensive `redactUrlPath` pass on it
        // works IF it ever did carry one, not because the probe found this happening.
        server_name: `https://app.example.com/cancel-deletion/${CANCEL_TOKEN}`,
        sdkProcessingMetadata: {
          dynamicSamplingContext: { transaction: `/cancel-deletion/${CANCEL_TOKEN}` },
        },
        breadcrumbs: [
          { category: 'navigation', message: `Navigated to https://app.example.com/cancel-deletion/${CANCEL_TOKEN}` },
        ],
      }

      const result = beforeSendTransaction(event as Parameters<typeof beforeSendTransaction>[0], {})

      const r = result as unknown as Record<string, unknown>
      expect(JSON.stringify(r.tags)).not.toContain(CANCEL_TOKEN)
      expect((r.tags as Record<string, string>).custom).toBe('foo')
      expect(JSON.stringify(r.user)).not.toContain(CANCEL_TOKEN)
      expect((r.user as Record<string, string>).id).toBe('user_1')
      expect(r.server_name).not.toContain(CANCEL_TOKEN)
      expect(JSON.stringify(r.sdkProcessingMetadata)).not.toContain(CANCEL_TOKEN)
      expect(JSON.stringify(r.breadcrumbs)).not.toContain(CANCEL_TOKEN)
      // measurements never carry a URL in practice (numeric value + unit), but must
      // survive the pass through unchanged rather than being dropped.
      expect((r.measurements as Record<string, { value: number }>).lcp.value).toBe(123)
    })
  });

  // Issue #1514 (fix round 11): a live SDK probe (captureException on a real page at a
  // token-bearing route, with `browserTracingIntegration` active) found the SAME fields
  // round 10 fixed on TransactionEvent were NEVER fixed on ErrorEvent — `beforeSend` only
  // ever walked `request`/`exception`/`extra`/`contexts`. `browserTracingIntegration`
  // populates `tags`/`transaction` on error events too (they share the same active page),
  // so this was a live gap, not theoretical, even though nothing in this app currently
  // calls `Sentry.setUser`/`setTag` directly (verified by grep) to also populate `user`.
  describe('error events cover the SAME field set as transaction events (#1514 fix round 11)', () => {
    test('redacts the token from event.transaction and tags on an ErrorEvent (not just TransactionEvent)', () => {
      const event = {
        exception: { values: [{ type: 'Error', value: 'boom' }] },
        transaction: `/cancel-deletion/${CANCEL_TOKEN}`,
        tags: { route: `/cancel-deletion/${CANCEL_TOKEN}`, custom: 'foo' },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      expect(result?.transaction).not.toContain(CANCEL_TOKEN)
      expect(result?.transaction).toBe('/cancel-deletion/[redacted]')
      const tags = result?.tags as Record<string, string>
      expect(tags.route).not.toContain(CANCEL_TOKEN)
      expect(tags.custom).toBe('foo')
    })

    test('redacts the token from user, breadcrumbs, and sdkProcessingMetadata on an ErrorEvent', () => {
      const event = {
        exception: { values: [{ type: 'Error', value: 'boom' }] },
        user: { id: 'user_1', email: `user+${CANCEL_TOKEN}@example.com` },
        breadcrumbs: [
          { category: 'navigation', message: `Navigated to https://app.example.com/cancel-deletion/${CANCEL_TOKEN}` },
        ],
        sdkProcessingMetadata: {
          dynamicSamplingContext: { transaction: `/cancel-deletion/${CANCEL_TOKEN}` },
        },
      }

      const result = beforeSend(event as Parameters<typeof beforeSend>[0], {})

      const r = result as unknown as Record<string, unknown>
      expect(JSON.stringify(r.user)).not.toContain(CANCEL_TOKEN)
      expect((r.user as Record<string, string>).id).toBe('user_1')
      expect(JSON.stringify(r.breadcrumbs)).not.toContain(CANCEL_TOKEN)
      expect(JSON.stringify(r.sdkProcessingMetadata)).not.toContain(CANCEL_TOKEN)
    })
  });
});
