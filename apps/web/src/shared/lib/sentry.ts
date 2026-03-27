import * as Sentry from '@sentry/react'

/**
 * Initialize Sentry from build-time env vars.
 * No-op if VITE_SENTRY_DSN is not configured.
 */
export function initSentry(): void {
  const dsn = import.meta.env.VITE_SENTRY_DSN
  if (!dsn) return

  Sentry.init({
    dsn,
    environment: import.meta.env.VITE_ENVIRONMENT || 'production',
    tracesSampleRate: 0.1,
    replaysSessionSampleRate: 0,
    replaysOnErrorSampleRate: 1,
    integrations: [Sentry.browserTracingIntegration()],
  })
}

export { Sentry }
