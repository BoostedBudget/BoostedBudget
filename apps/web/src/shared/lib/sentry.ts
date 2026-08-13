import * as Sentry from '@sentry/react'
import type { Breadcrumb, ErrorEvent, EventHint } from '@sentry/react'
import { redactPii, redactUrlPath, sanitizeUrl } from './piiScrubbing'

// `SpanJSON`/`TransactionEvent` aren't part of `@sentry/react`'s public export surface
// (they live in `@sentry/core`, which this package doesn't declare as a direct
// dependency) — derived structurally from `Sentry.init`'s own options type instead of
// importing them by name.
type SentryInitOptions = NonNullable<Parameters<typeof Sentry.init>[0]>
type BeforeSendTransactionFn = NonNullable<SentryInitOptions['beforeSendTransaction']>
type BeforeSendSpanFn = NonNullable<SentryInitOptions['beforeSendSpan']>
type TransactionEvent = Parameters<BeforeSendTransactionFn>[0]

/**
 * Scrubbing hooks shared by `initSentry` and exercised directly in tests.
 *
 * Two halves of the same problem:
 * - `beforeBreadcrumb` covers the default `Breadcrumbs` integration, which
 *   records every `console.*` call verbatim. Several call sites in this app
 *   log third-party (Plaid/Paddle) payloads that can carry financial PII —
 *   dropping the whole `console` category is simpler and safer than trying
 *   to redact arbitrary, unstructured log arguments.
 * - `beforeSend` covers everything `beforeBreadcrumb` cannot: exceptions
 *   captured directly via `Sentry.captureException` (see `useApiError.ts`,
 *   `ErrorBoundary.tsx`, `FeatureErrorBoundary.tsx`), and any Axios error
 *   `config`/`response` data an error handler attaches to `extra`/`contexts`.
 *   It also strips query strings from request URLs, which can carry
 *   account/session identifiers, and redacts `event.exception`, `event.request`,
 *   `event.extra`, and `event.contexts` by property name — covering
 *   `event.request`'s `headers`/`data`/`query_string` fields (e.g. an
 *   `Authorization`/`Cookie` header) as well as stack-frame `vars` (local
 *   variables some SDKs attach per frame) inside `event.exception`. Redaction
 *   is by *key name* only (see `redactPii` in `piiScrubbing.ts`): `query_string`
 *   and `cookies` are both bare entries in `PII_PROPERTY_DENYLIST`, so a
 *   `request.query_string` value or a nested `cookies` object is redacted
 *   wholesale (the entire value replaced, not scanned) whenever that exact
 *   key is hit — regardless of what its own nested keys are named. The
 *   residual scope limit is narrower than that: a query string or cookie
 *   value nested under some *other*, non-denylisted key name (e.g. a
 *   third-party payload that renames the field) would still pass through
 *   unredacted, because matching is by key name, not by content, for PII.
 *   Capability-route redaction (fix round 7) is the one exception: `redactPii`
 *   runs every string value it visits — including `event.exception`'s `value`
 *   (message) field and breadcrumb `message` (handled directly in
 *   `beforeBreadcrumb` below, since it isn't nested under `data`) — through
 *   `redactUrlPath`, so a bearer token embedded in ordinary error text is
 *   still caught even though it isn't a denylisted key. This stays a targeted
 *   rewrite (only the known capability-route shapes match), not a general PII
 *   content scan — an ordinary error message is otherwise preserved verbatim.
 */
export function sentryScrubbingOptions(): {
  beforeBreadcrumb: (breadcrumb: Breadcrumb) => Breadcrumb | null
  beforeSend: (event: ErrorEvent, hint: EventHint) => ErrorEvent | null
  beforeSendTransaction: (event: TransactionEvent, hint: EventHint) => TransactionEvent | null
  beforeSendSpan: BeforeSendSpanFn
} {
  return {
    beforeBreadcrumb(breadcrumb) {
      if (breadcrumb.category === 'console') return null
      if (breadcrumb.data) {
        breadcrumb.data = redactPii(breadcrumb.data)
      }
      // Issue #1514 (fix round 7): `breadcrumb.message` (e.g. a 'navigation' breadcrumb's
      // "Navigated to <url>" text) was never touched — only `breadcrumb.data` was. A
      // capability URL (the account-deletion/household-invite page a user just navigated
      // to) reaches this field directly, not nested under `data`.
      if (typeof breadcrumb.message === 'string') {
        breadcrumb.message = redactUrlPath(breadcrumb.message)
      }
      return breadcrumb
    },
    beforeSend(event) {
      // Issue #1514 (fix round 6): `sanitizeUrl` strips the query string AND redacts any
      // known capability-token path segment (e.g. the account-deletion cancellation link
      // — a bearer credential embedded in the PATH, which a query-string-only strip never
      // touched). `redactPii` below applies the same `sanitizeUrl` to any nested `url` key
      // it finds (e.g. a nested Axios `config.url`/`response.config.url` under `extra`),
      // so this explicit line and that recursive pass are redundant on `event.request.url`
      // specifically — both must still call `sanitizeUrl` (not `stripQueryString`), since
      // either one running alone must still fully sanitize the URL.
      if (event.request?.url) {
        event.request = { ...event.request, url: sanitizeUrl(event.request.url) }
      }
      if (event.request) {
        event.request = redactPii(event.request)
      }
      if (event.exception) {
        event.exception = redactPii(event.exception)
      }
      if (event.extra) {
        event.extra = redactPii(event.extra)
      }
      if (event.contexts) {
        event.contexts = redactPii(event.contexts)
      }
      return event
    },
    // Issue #1514 (fix round 9): `beforeSend` is ERROR/message-only — it never sees
    // performance transaction events, which are a completely separate pipeline. With
    // `tracesSampleRate: 0.1` and `browserTracingIntegration()` enabled below, roughly
    // 1 in 10 page views of a capability route (e.g. /cancel-deletion/{token}) emitted a
    // transaction whose OWN name is the raw pathname — bypassing every fix in rounds 6-8
    // entirely, since none of them touch this hook.
    //
    // Verified empirically (not guessed): ran the real `browserTracingIntegration()`
    // against a pageload at a token-bearing path with a recording transport and inspected
    // the actual event. The token showed up in `event.transaction` (the transaction
    // "name"), `event.contexts.trace.data['url.path']`/`['url.full']`, and
    // `event.request.url` — reusing the exact same `redactUrlPath`/`sanitizeUrl`/
    // `redactPii` helpers `beforeSend` above uses, not a forked copy.
    beforeSendTransaction(event) {
      if (typeof event.transaction === 'string') {
        event.transaction = redactUrlPath(event.transaction)
      }
      if (event.request?.url) {
        event.request = { ...event.request, url: sanitizeUrl(event.request.url) }
      }
      if (event.request) {
        event.request = redactPii(event.request)
      }
      if (event.contexts) {
        event.contexts = redactPii(event.contexts)
      }
      if (event.extra) {
        event.extra = redactPii(event.extra)
      }
      if (event.spans) {
        event.spans = redactPii(event.spans)
      }
      return event
    },
    // Issue #1514 (fix round 9): child spans (e.g. an instrumented fetch/XHR call to a
    // capability route) are ALSO reachable independently of `beforeSendTransaction` — this
    // hook runs once per span. Verified the same way: a span's `description` (e.g.
    // "GET /households/invites/{token}/accept") and `data['url.path']`/`['url.full']`
    // carry the token exactly like the transaction-level fields above.
    beforeSendSpan(span) {
      if (typeof span.description === 'string') {
        span.description = redactUrlPath(span.description)
      }
      return redactPii(span)
    },
  }
}

/**
 * Initialize Sentry from runtime config.
 * Falls back to build-time env vars for Cloudflare Pages deployments.
 * No-op if SENTRY_DSN is not configured.
 */
export function initSentry(): void {
  const dsn = window.ENV?.SENTRY_DSN || import.meta.env.VITE_SENTRY_DSN
  if (!dsn) return

  Sentry.init({
    dsn,
    environment: window.ENV?.ENVIRONMENT || import.meta.env.VITE_ENVIRONMENT || 'production',
    tracesSampleRate: 0.1,
    replaysSessionSampleRate: 0,
    replaysOnErrorSampleRate: 1,
    integrations: [Sentry.browserTracingIntegration()],
    ...sentryScrubbingOptions(),
  })
}

export { Sentry }
