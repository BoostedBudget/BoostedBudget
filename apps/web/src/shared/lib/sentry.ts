import * as Sentry from '@sentry/react'
import type { Breadcrumb, ErrorEvent, EventHint } from '@sentry/react'
import { redactPii, stripQueryString } from './piiScrubbing'

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
 *   unredacted, because matching is by key name, not by content. The
 *   exception `value` (message string) is never content-scanned, only passed through
 *   the same by-key redaction as everything else, so a plain error message
 *   is preserved unless it happens to sit under a denylisted key.
 */
export function sentryScrubbingOptions(): {
  beforeBreadcrumb: (breadcrumb: Breadcrumb) => Breadcrumb | null
  beforeSend: (event: ErrorEvent, hint: EventHint) => ErrorEvent | null
} {
  return {
    beforeBreadcrumb(breadcrumb) {
      if (breadcrumb.category === 'console') return null
      if (breadcrumb.data) {
        breadcrumb.data = redactPii(breadcrumb.data)
      }
      return breadcrumb
    },
    beforeSend(event) {
      if (event.request?.url) {
        event.request = { ...event.request, url: stripQueryString(event.request.url) }
      }
      // `redactPii` itself special-cases any property literally named `url`
      // by running it through `stripQueryString`, so redacting `event.request`
      // after the explicit strip above re-strips the (already-stripped) url
      // as a no-op — it cannot undo or bypass the strip above.
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
