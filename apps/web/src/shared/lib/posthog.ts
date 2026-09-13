import posthog, {
  type CapturedNetworkRequest,
  type PostHogConfig,
  type Properties,
} from 'posthog-js'
import { redactCapabilityUrlPath } from '@boostedbudget/api-client/client'
import { ANALYTICS_EVENTS } from '../analytics'
import { isPiiKey } from './piiScrubbing'
import {
  createProductAnalyticsLifecycle,
  type AnalyticsLifecycleSnapshot,
  type AnalyticsLifecycleState,
} from './analyticsLifecycle'

export type { AnalyticsLifecycleSnapshot, AnalyticsLifecycleState } from './analyticsLifecycle'

const SENSITIVE_PATHS = [
  /^\/billing\/(?:manage|success)(?:\/|$)/,
  /^\/cancel-deletion(?:\/|$)/,
  /^\/households\/invites(?:\/|$)/,
  /^\/household\/invite(?:\/|$)/,
]
const CALLBACK_SEGMENT = /\/(?:oauth-)?callback(?:\/|$)/
const ANONYMOUS_PAGE_PATHS = new Set([
  '/',
  '/legal/privacy-policy',
  '/privacy',
  '/legal/terms',
  '/legal/cookie-policy',
  '/pricing',
  '/mcp',
])
const ENTITY_ROUTES: ReadonlyArray<[RegExp, string]> = [
  [/^(.*\/accounts)\/[^/]+(\/.*)?$/, '$1/:accountId$2'],
  [/^(.*\/budgets)\/[^/]+(\/.*)?$/, '$1/:budgetId$2'],
  [/^(.*\/boosts)\/[^/]+(\/.*)?$/, '$1/:id$2'],
]
const URL_PROPERTIES = new Set([
  '$current_url', '$pathname', '$initial_current_url', 'current_url',
  'pathname', 'page_url', 'request_url', 'url', 'href',
])
const REFERRER_PROPERTIES = new Set([
  '$referrer', '$referring_domain', '$initial_referrer',
  '$initial_referring_domain', 'referrer', 'referring_domain',
])
const CAMPAIGN_CLICK_ID_PROPERTIES = new Set([
  'gclid', 'fbclid', 'msclkid', '$gclid', '$fbclid', '$msclkid',
])

const WITHDRAWAL_STORAGE_KEY = 'boostedbudget:analytics-withdrawal'

function pathFromUrl(value: string): { origin: string; pathname: string } {
  try {
    const parsed = new URL(value, window.location.origin)
    const absolute = /^[a-z][a-z\d+.-]*:\/\//i.test(value)
    return { origin: absolute ? parsed.origin : '', pathname: parsed.pathname }
  } catch {
    return { origin: '', pathname: value.split(/[?#]/, 1)[0] || '/' }
  }
}

export function isSensitiveAnalyticsPath(value: string): boolean {
  const { pathname } = pathFromUrl(value)
  return redactCapabilityUrlPath(pathname) !== pathname
    || CALLBACK_SEGMENT.test(pathname)
    || SENSITIVE_PATHS.some(pattern => pattern.test(pathname))
}

export function isAnonymousAnalyticsPath(value: string): boolean {
  const pathname = pathFromUrl(value).pathname.toLowerCase().replace(/\/+$/, '') || '/'
  return ANONYMOUS_PAGE_PATHS.has(pathname)
}

export function sanitizeAnalyticsUrl(value: string): string {
  const { origin, pathname } = pathFromUrl(value)
  let safePath = redactCapabilityUrlPath(pathname) ?? pathname
  for (const [pattern, replacement] of ENTITY_ROUTES) {
    if (pattern.test(safePath)) {
      safePath = safePath.replace(pattern, replacement)
      break
    }
  }
  return `${origin}${safePath}`
}

function isCampaignProperty(key: string): boolean {
  const normalized = key.toLowerCase()
  return normalized.startsWith('utm_')
    || normalized.startsWith('$initial_utm_')
    || CAMPAIGN_CLICK_ID_PROPERTIES.has(normalized)
}

function containsSensitiveUrl(value: unknown): boolean {
  if (!value || typeof value !== 'object') return false
  for (const [key, child] of Object.entries(value)) {
    const isSensitiveUrl = URL_PROPERTIES.has(key.toLowerCase())
      && typeof child === 'string'
      && isSensitiveAnalyticsPath(child)
    if (isSensitiveUrl) {
      return true
    }
    if (containsSensitiveUrl(child)) return true
  }
  return false
}

function sanitizeValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sanitizeValue)
  if (!value || typeof value !== 'object') return value

  const sanitized: Record<string, unknown> = {}
  for (const [key, child] of Object.entries(value)) {
    const normalized = key.toLowerCase()
    if (isPiiKey(key) || REFERRER_PROPERTIES.has(normalized) || isCampaignProperty(normalized)) continue
    if (URL_PROPERTIES.has(normalized)) {
      if (typeof child === 'string') sanitized[key] = sanitizeAnalyticsUrl(child)
      continue
    }
    sanitized[key] = sanitizeValue(child)
  }
  return sanitized
}

export function sanitizeCapturedNetworkRequest(
  request: CapturedNetworkRequest,
): CapturedNetworkRequest {
  const copy = { ...request }
  copy.name = sanitizeAnalyticsUrl(request.name)
  copy.requestHeaders = undefined
  copy.responseHeaders = undefined
  copy.requestBody = undefined
  copy.responseBody = undefined
  return copy
}

export function posthogConfig(): Partial<PostHogConfig> {
  return {
    opt_out_capturing_by_default: true,
    opt_out_persistence_by_default: true,
    respect_dnt: true,
    capture_pageview: false,
    capture_pageleave: false,
    autocapture: false,
    capture_exceptions: false,
    disable_session_recording: true,
    disable_surveys: true,
    disable_surveys_automatic_display: true,
    disable_conversations: true,
    disable_web_experiments: true,
    disable_product_tours: true,
    disable_external_dependency_loading: true,
    capture_heatmaps: false,
    capture_dead_clicks: false,
    disable_scroll_properties: true,
    save_referrer: false,
    save_campaign_params: false,
    custom_campaign_params: [],
    advanced_disable_flags: true,
    session_recording: {
      maskAllInputs: true,
      maskTextSelector: '[data-ph-mask]',
      blockSelector: '[data-ph-block]',
      recordHeaders: false,
      recordBody: false,
      maskCapturedNetworkRequestFn: sanitizeCapturedNetworkRequest,
    },
    before_send: event => {
      if (!event || containsSensitiveUrl(event.properties)) return null
      return sanitizeValue(event) as typeof event
    },
  }
}

export function isPostHogConfigured(): boolean {
  return !!(window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY)
}

export function isDoNotTrackEnabled(): boolean {
  return navigator.doNotTrack === '1'
    || (window as Window & { doNotTrack?: string }).doNotTrack === '1'
}

export function getAnalyticsLifecycleState(): AnalyticsLifecycleState {
  return analyticsLifecycle.getState()
}

export function getAnalyticsLifecycleSnapshot(): AnalyticsLifecycleSnapshot {
  return analyticsLifecycle.getSnapshot()
}

export function setAnalyticsLifecyclePending(): void {
  analyticsLifecycle.setPending()
}

export function setProductAnalyticsScope(scope: string): void {
  analyticsLifecycle.setScope(scope)
}

export function initPostHog(): boolean {
  return analyticsLifecycle.init()
}

export function grantProductAnalytics(
  scope: string,
  distinctId?: string,
  personProperties?: Properties,
): void {
  analyticsLifecycle.grantExplicitly(scope, distinctId, personProperties)
}

export function applyProductAnalyticsAuthority(
  scope: string,
  distinctId?: string,
  personProperties?: Properties,
): void {
  analyticsLifecycle.grantFromAuthority(scope, distinctId, personProperties)
}

export function denyProductAnalytics(): void {
  analyticsLifecycle.deny()
}

export function withdrawProductAnalytics(scope: string): void {
  analyticsLifecycle.withdraw(scope)
  try {
    localStorage.setItem(WITHDRAWAL_STORAGE_KEY, JSON.stringify({ scope, nonce: Date.now() }))
    localStorage.removeItem(WITHDRAWAL_STORAGE_KEY)
  } catch {
    // Local denial is authoritative even when cross-tab storage is unavailable.
  }
}

export function listenForAnalyticsWithdrawal(): () => void {
  function handleStorage(event: StorageEvent): void {
    if (event.key === WITHDRAWAL_STORAGE_KEY && event.newValue !== null) {
      try {
        const message = JSON.parse(event.newValue) as { scope?: unknown }
        if (typeof message.scope === 'string') analyticsLifecycle.withdraw(message.scope)
      } catch {
        // Ignore malformed or legacy messages that cannot be scoped safely.
      }
    }
  }
  window.addEventListener('storage', handleStorage)
  return () => window.removeEventListener('storage', handleStorage)
}

export function subscribeToAnalyticsLifecycle(
  listener: (snapshot: AnalyticsLifecycleSnapshot) => void,
): () => void {
  return analyticsLifecycle.subscribe(listener)
}

export function capturePageView(url: string): void {
  if (getAnalyticsLifecycleState() !== 'granted') return
  if (isSensitiveAnalyticsPath(url)) {
    posthog.stopSessionRecording()
    return
  }
  const safeUrl = sanitizeAnalyticsUrl(url)
  analyticsLifecycle.capture(ANALYTICS_EVENTS.POSTHOG_PAGEVIEW, {
    $current_url: safeUrl,
    $pathname: pathFromUrl(safeUrl).pathname,
  })
}

export function captureProductEvent(event: string, properties?: Properties): void {
  analyticsLifecycle.capture(event, properties)
}

export function setProductAnalyticsPersonProperties(properties: Properties): void {
  analyticsLifecycle.setPersonProperties(properties)
}

/** Test-only reset; production authority transitions use pending/grant/deny above. */
export function resetAnalyticsLifecycleForTests(): void {
  analyticsLifecycle.resetForTests()
}

const analyticsLifecycle = createProductAnalyticsLifecycle({
  client: posthog,
  config: posthogConfig,
  getHost: () => window.ENV?.POSTHOG_HOST
    || import.meta.env.VITE_POSTHOG_HOST
    || 'https://us.i.posthog.com',
  getKey: () => window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY,
  isDoNotTrackEnabled,
})

export { posthog }
