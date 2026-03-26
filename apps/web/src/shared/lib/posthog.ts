import posthog from 'posthog-js'

/**
 * Initialize PostHog from runtime config.
 * No-op if POSTHOG_KEY is not configured.
 */
export function initPostHog(): void {
  const key = window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY
  if (!key) return

  const host = window.ENV?.POSTHOG_HOST || import.meta.env.VITE_POSTHOG_HOST || 'https://us.i.posthog.com'

  posthog.init(key, {
    api_host: host,
    ui_host: 'https://us.posthog.com',
    opt_out_capturing_by_default: true,
    respect_dnt: true,
    capture_pageview: false,
    capture_pageleave: true,
    persistence: 'localStorage+cookie',
    session_recording: {
      maskAllInputs: true,
      maskTextSelector: '[data-ph-mask]',
      recordHeaders: false,
      recordBody: false,
    },
  })
}

export { posthog }
