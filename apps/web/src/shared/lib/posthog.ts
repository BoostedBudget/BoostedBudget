import posthog, { type PostHogConfig } from 'posthog-js'
import { PII_PROPERTY_DENYLIST } from './piiScrubbing'

export function posthogConfig(): Partial<PostHogConfig> {
  return {
    opt_out_capturing_by_default: true,
    respect_dnt: true,
    capture_pageview: false,
    capture_pageleave: true,
    persistence: 'localStorage+cookie',
    // Autocapture records $el_text and attributes on every click/change — leaks
    // PII even when elements are data-ph-mask'd in session replay.
    autocapture: false,
    session_recording: {
      maskAllInputs: true,
      maskTextSelector: '[data-ph-mask]',
      recordHeaders: false,
      recordBody: false,
    },
    before_send: (event) => {
      if (!event) return null
      if (event.properties) {
        for (const key of PII_PROPERTY_DENYLIST) {
          delete (event.properties as Record<string, unknown>)[key]
        }
      }
      return event
    },
  }
}

/**
 * Returns true when a PostHog key is available (runtime config or build-time env).
 * Note: the VITE_POSTHOG_KEY path is a build-time constant and cannot be mutated in
 * tests; cover it via integration tests or accept the window.ENV path as the test proxy.
 */
export function isPostHogConfigured(): boolean {
  return !!(window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY)
}

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
    ...posthogConfig(),
  })
}

export { posthog }
