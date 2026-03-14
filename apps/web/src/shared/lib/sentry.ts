import * as Sentry from '@sentry/react'

/**
 * Initialize Sentry from runtime config.
 * No-op if SENTRY_DSN is not configured.
 */
export function initSentry(): void {
  const dsn = window.ENV?.SENTRY_DSN
  if (!dsn) return

  Sentry.init({
    dsn,
    environment: window.ENV?.ENVIRONMENT ?? 'production',
    tracesSampleRate: 0.1,
    replaysSessionSampleRate: 0,
    replaysOnErrorSampleRate: 1,
    integrations: [Sentry.browserTracingIntegration()],
  })
}

export { Sentry }
