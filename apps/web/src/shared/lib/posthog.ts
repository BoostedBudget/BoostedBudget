import posthog, {
  type CapturedNetworkRequest,
  type PostHogConfig,
  type Properties,
} from 'posthog-js'
import { redactCapabilityUrlPath } from '@boostedbudget/api-client/client'
import { ANALYTICS_EVENTS } from '../analytics'
import { isPiiKey } from './piiScrubbing'
import {
  createAnalyticsCaptureGate,
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
  '$session_entry_current_url', '$session_entry_pathname', '$session_entry_url',
  'pathname', 'page_url', 'request_url', 'url', 'href',
])
const REFERRER_PROPERTIES = new Set([
  '$referrer', '$referring_domain', '$initial_referrer',
  '$initial_referring_domain', '$session_entry_referrer',
  '$session_entry_referring_domain', 'referrer', 'referring_domain',
])
const CAMPAIGN_CLICK_ID_PROPERTIES = new Set([
  'gclid', 'fbclid', 'msclkid', '$gclid', '$fbclid', '$msclkid',
])

const WITHDRAWAL_STORAGE_KEY = 'boostedbudget:analytics-withdrawal'
const YES_LIKE_PRIVACY_SIGNAL_VALUES = new Set<unknown>([true, 'true', 1, '1', 'yes'])

export interface ClearablePostHogQueue {
  // Pinned 1.430.2 ESM names: enqueue Ls, interval ca, polling ha, queue ia, timeout ma.
  Ls?: (request: unknown) => unknown
  ca?: number
  ha?: boolean
  ia?: unknown[]
  ma?: ReturnType<typeof setTimeout>
  _enqueue?: (request: unknown) => unknown
  _flushTimeout?: ReturnType<typeof setTimeout>
  _isPolling?: boolean
  _poller?: ReturnType<typeof setTimeout>
  _queue?: unknown[]
  retriableRequest?: (request: unknown) => unknown
}

export interface PostHogWithQueues {
  // `Du` is `_retryQueue` in the pinned 1.430.2 ESM bundle.
  Du?: ClearablePostHogQueue
  __request_queue?: unknown[]
  _requestQueue?: ClearablePostHogQueue
  _retryQueue?: ClearablePostHogQueue
}

interface PersistenceConfigurablePostHog {
  config: Pick<PostHogConfig, 'persistence_name' | 'token'>
  set_config(config: Partial<PostHogConfig>): void
}

export function clearPostHogSessionPersistence(persistenceName: string): void {
  const prefix = `ph_${persistenceName}`
  try {
    for (let index = sessionStorage.length - 1; index >= 0; index -= 1) {
      const key = sessionStorage.key(index)
      if (key === prefix || key?.startsWith(`${prefix}_`)) sessionStorage.removeItem(key)
    }
  } catch {
    // The SDK already has persistence disabled; inaccessible storage cannot be used by it.
  }
}

export function setPostHogPersistenceEnabled(
  sdk: PersistenceConfigurablePostHog,
  enabled: boolean,
): void {
  sdk.set_config({ disable_persistence: !enabled })
  if (enabled) return
  const persistenceName = sdk.config.persistence_name || sdk.config.token
  if (persistenceName) clearPostHogSessionPersistence(persistenceName)
}

export function clearPostHogQueuedEvents(
  sdk: PostHogWithQueues = posthog as unknown as PostHogWithQueues,
): void {
  // posthog-js 1.430.2 has no public queue-discard API. Clear both pinned SDK
  // queues so withdrawal cannot later transmit an event accepted beforehand.
  for (const queue of [sdk._requestQueue, sdk._retryQueue ?? sdk.Du]) {
    if (!queue) continue
    if (queue._flushTimeout) clearTimeout(queue._flushTimeout)
    if (queue._poller) clearTimeout(queue._poller)
    queue._flushTimeout = undefined
    queue._poller = undefined
    queue._isPolling = false
    queue._queue = []
    if (queue.ma) clearTimeout(queue.ma)
    queue.ma = undefined
    queue.ha = false
    queue.ia = []
  }
  sdk.__request_queue = []
}

const retryGuardCleanups = new WeakMap<object, () => void>()
const RETRY_AUTHORITY_GENERATION = Symbol('analytics-authority-generation')

type GenerationTaggedRequest = Record<PropertyKey, unknown> & {
  [RETRY_AUTHORITY_GENERATION]?: number
}

export function guardPostHogRetryQueue(
  sdk: PostHogWithQueues,
  getGeneration: () => number,
  isGranted: () => boolean,
): () => void {
  const queue = sdk._retryQueue ?? sdk.Du
  if (!queue) {
    throw new Error('Initialized PostHog retry queue is unavailable; pinned SDK compatibility failed')
  }
  const installedCleanup = retryGuardCleanups.get(queue)
  if (installedCleanup) return installedCleanup
  const enqueueKey = queue._enqueue ? '_enqueue' : 'Ls'
  const originalEnqueue = queue[enqueueKey]
  const originalRetriableRequest = queue.retriableRequest
  if (!originalEnqueue) {
    throw new Error('Initialized PostHog retry queue is missing _enqueue/Ls; pinned SDK compatibility failed')
  }
  if (!originalRetriableRequest) {
    throw new Error('Initialized PostHog retry queue is missing retriableRequest; pinned SDK compatibility failed')
  }
  const requestGenerations = new WeakMap<object, number>()

  function getRequestGeneration(request: unknown): number {
    if (!request || typeof request !== 'object') return getGeneration()
    return (request as GenerationTaggedRequest)[RETRY_AUTHORITY_GENERATION]
      ?? requestGenerations.get(request)
      ?? getGeneration()
  }

  queue.retriableRequest = function guardedRetriableRequest(request) {
    const generation = getRequestGeneration(request)
    if (!isGranted() || generation !== getGeneration()) return undefined
    if (!request || typeof request !== 'object') {
      return originalRetriableRequest.call(this, request)
    }
    requestGenerations.set(request, generation)
    const tagged = { ...(request as Record<PropertyKey, unknown>), [RETRY_AUTHORITY_GENERATION]: generation }
    requestGenerations.set(tagged, generation)
    return originalRetriableRequest.call(this, tagged)
  }
  queue[enqueueKey] = function guardedEnqueue(request) {
    const generation = getRequestGeneration(request)
    if (!isGranted() || generation !== getGeneration()) return undefined
    return originalEnqueue.call(this, request)
  }

  const cleanup = () => {
    queue.retriableRequest = originalRetriableRequest
    queue[enqueueKey] = originalEnqueue
    retryGuardCleanups.delete(queue)
  }
  retryGuardCleanups.set(queue, cleanup)
  return cleanup
}

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
    || normalized.startsWith('$session_entry_utm_')
    || CAMPAIGN_CLICK_ID_PROPERTIES.has(normalized)
}

function containsSensitiveUrl(value: unknown, visited = new WeakSet<object>()): boolean {
  if (!value || typeof value !== 'object') return false
  if (visited.has(value)) return false
  visited.add(value)
  for (const [key, child] of Object.entries(value)) {
    const isSensitiveUrl = URL_PROPERTIES.has(key.toLowerCase())
      && typeof child === 'string'
      && isSensitiveAnalyticsPath(child)
    if (isSensitiveUrl) {
      return true
    }
    if (containsSensitiveUrl(child, visited)) return true
  }
  return false
}

function sanitizeValue(value: unknown, ancestors = new WeakSet<object>()): unknown {
  if (!value || typeof value !== 'object') return value
  if (value instanceof Date) return new Date(value.getTime())
  if (ancestors.has(value)) return null
  ancestors.add(value)

  if (Array.isArray(value)) {
    const sanitized = value.map(child => sanitizeValue(child, ancestors))
    ancestors.delete(value)
    return sanitized
  }

  const sanitized: Record<string, unknown> = {}
  for (const [key, child] of Object.entries(value)) {
    const normalized = key.toLowerCase()
    if (isPiiKey(key) || REFERRER_PROPERTIES.has(normalized) || isCampaignProperty(normalized)) continue
    if (URL_PROPERTIES.has(normalized)) {
      if (typeof child === 'string') sanitized[key] = sanitizeAnalyticsUrl(child)
      continue
    }
    sanitized[key] = sanitizeValue(child, ancestors)
  }
  ancestors.delete(value)
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

export function posthogConfig(
  isCaptureAllowed: () => boolean = analyticsCaptureGate.isGranted,
): Partial<PostHogConfig> {
  return {
    opt_out_capturing_by_default: false,
    opt_out_persistence_by_default: false,
    disable_persistence: true,
    persistence: 'sessionStorage',
    reuseAnonymousId: true,
    request_batching: false,
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
      if (!isCaptureAllowed() || !event || containsSensitiveUrl(event.properties)) return null
      return sanitizeValue(event) as typeof event
    },
  }
}

export function isPostHogConfigured(): boolean {
  return !!(window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY)
}

export function isDoNotTrackEnabled(): boolean {
  const privacyNavigator = navigator as Navigator & {
    globalPrivacyControl?: boolean | number | string
    msDoNotTrack?: boolean | number | string
  }
  const privacyWindow = window as Window & { doNotTrack?: boolean | number | string }
  return [
    navigator.doNotTrack,
    privacyNavigator.msDoNotTrack,
    privacyWindow.doNotTrack,
    privacyNavigator.globalPrivacyControl,
  ].some(value => YES_LIKE_PRIVACY_SIGNAL_VALUES.has(value))
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

export function listenForAnalyticsWithdrawal(
  onWithdrawal: (scope: string) => void = scope => analyticsLifecycle.withdraw(scope),
): () => void {
  function handleStorage(event: StorageEvent): void {
    if (event.key === WITHDRAWAL_STORAGE_KEY && event.newValue !== null) {
      try {
        const message = JSON.parse(event.newValue) as { scope?: unknown }
        if (typeof message.scope === 'string') onWithdrawal(message.scope)
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
  analyticsLifecycle.resetForTests(true)
}

const analyticsCaptureGate = createAnalyticsCaptureGate()

const analyticsClient = {
  capture: (event: string, properties?: Properties) => posthog.capture(event, properties),
  clearLegacyConsent: () => posthog.clear_opt_in_out_capturing(),
  clearPersistence: () => {
    const persistenceName = posthog.config.persistence_name
      || posthog.config.token
      || window.ENV?.POSTHOG_KEY
      || import.meta.env.VITE_POSTHOG_KEY
    if (persistenceName) clearPostHogSessionPersistence(persistenceName)
  },
  clearQueuedEvents: clearPostHogQueuedEvents,
  identify: (distinctId: string, properties?: Properties) => posthog.identify(distinctId, properties),
  init: (key: string, config: Partial<PostHogConfig>) => {
    clearPostHogSessionPersistence(config.persistence_name || key)
    const initialized = posthog.init(key, config)
    guardPostHogRetryQueue(
      posthog as unknown as PostHogWithQueues,
      analyticsCaptureGate.getGeneration,
      analyticsCaptureGate.isGranted,
    )
    return initialized
  },
  reset: (resetDeviceId: boolean) => posthog.reset(resetDeviceId),
  setPersistenceEnabled: (enabled: boolean) => setPostHogPersistenceEnabled(posthog, enabled),
  setPersonProperties: (properties: Properties) => posthog.setPersonProperties(properties),
  stopSessionRecording: () => posthog.stopSessionRecording(),
}

const analyticsLifecycle = createProductAnalyticsLifecycle({
  captureGate: analyticsCaptureGate,
  client: analyticsClient,
  config: posthogConfig,
  getHost: () => window.ENV?.POSTHOG_HOST
    || import.meta.env.VITE_POSTHOG_HOST
    || 'https://us.i.posthog.com',
  getKey: () => window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY,
  isDoNotTrackEnabled,
})

export { posthog }
