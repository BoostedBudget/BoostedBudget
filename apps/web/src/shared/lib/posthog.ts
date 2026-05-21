import posthog, { type PostHogConfig } from 'posthog-js'

const PII_PROPERTY_DENYLIST = new Set([
  'merchant',
  'merchant_name',
  'nickname',
  'institution',
  'institution_name',
  'description',
  'account_name',
  'balance',
  'amount',
  'current_balance',
  'target_amount',
  'current_amount',
  'total_saved',
])

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
