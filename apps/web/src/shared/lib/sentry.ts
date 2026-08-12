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
 *   `ErrorBoundary.tsx`), and any Axios error `config`/`response` data an
 *   error handler attaches to `extra`/`contexts`. It also strips query
 *   strings from request URLs, which can carry account/session identifiers.
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
