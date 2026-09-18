import type {
  CapturedNetworkRequest,
  PostHog,
  PostHogConfig,
  Properties,
} from 'posthog-js'
import { redactCapabilityUrlPath } from '@boostedbudget/api-client/client'
import {
  ANALYTICS_EVENTS,
  normalizeAnalyticsEventPayload,
  type AnalyticsEventArguments,
  type AnalyticsEventName,
} from '../analytics'
import { isPiiKey } from './piiScrubbing'
import {
  createAnalyticsCaptureGate,
  createProductAnalyticsLifecycle,
  type AnalyticsAuthority,
  type AnalyticsLifecycleSnapshot,
  type AnalyticsLifecycleState,
} from './analyticsLifecycle'

export type {
  AnalyticsAuthority,
  AnalyticsLifecycleSnapshot,
  AnalyticsLifecycleState,
  AnalyticsPurposes,
} from './analyticsLifecycle'

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
// Person properties are default-deny: `$set`/`$set_once` maps keep ONLY these
// keys. The browser sends `{name}` (Task 6); the rest are the server/legacy floor.
const ALLOWED_PERSON_PROPERTIES = new Set([
  'is_comped',
  'name',
  'primary_region',
  'subscription_status_effective',
  'subscription_tier',
])
const PERSON_PROPERTY_MAPS = new Set(['$set', '$set_once'])
// Envelope keys posthog-js puts on every event's `properties` inside
// `calculateEventProperties` (installed posthog-js 1.430.2,
// node_modules/posthog-js/dist/module.js). A `$set`-NAMED event's top-level
// properties are default-denied like a person-property map, but these are the
// event envelope, not person properties: the SDK's own before_send wrapper
// snapshots `token` and DROPS the whole event if it is removed ("This property
// is required for ingestion"), and the rest are the metadata every capture
// carries. Person properties live in the `$set`/`$set_once` sub-maps, which
// stay default-denied. Keys are taken verbatim from that function:
// `token`/`$config_defaults`/`$cookieless_mode`/`distinct_id`/`$session_id`/
// `$window_id`/`$is_identified`/`$process_person_profile`/`$groups`/
// `$duration`/`$browser_type`/`$lib_custom_api_host` are set directly there;
// `$device_id`/`$had_persisted_distinct_id` come from persistence properties
// merged in; `$pageview_id` from `pageViewManager.doEvent()`; the `$os`/
// `$browser`/`$device`/`$screen`/`$viewport`/`$timezone`/`$insert_id`/`$time`
// family from its browser-properties helper. `$sdk_debug_*` keys are omitted
// deliberately: they are diagnostic-only and never required for ingestion.
const SDK_TRANSPORT_PROPERTIES = new Set([
  'token',
  '$config_defaults',
  '$cookieless_mode',
  'distinct_id',
  '$device_id',
  '$had_persisted_distinct_id',
  '$session_id',
  '$window_id',
  '$pageview_id',
  '$lib',
  '$lib_version',
  '$lib_custom_api_host',
  '$insert_id',
  '$time',
  '$os',
  '$os_version',
  '$browser',
  '$browser_version',
  '$browser_language',
  '$browser_language_prefix',
  '$browser_type',
  '$device',
  '$device_type',
  '$webview_app',
  '$webview_app_version',
  '$current_url',
  '$host',
  '$pathname',
  '$raw_user_agent',
  '$screen_height',
  '$screen_width',
  '$viewport_height',
  '$viewport_width',
  '$timezone',
  '$timezone_offset',
  '$sdk_dist_channel',
  '$duration',
  '$groups',
  '$is_identified',
  '$process_person_profile',
])

const WITHDRAWAL_STORAGE_KEY = 'boostedbudget:analytics-withdrawal'
const YES_LIKE_PRIVACY_SIGNAL_VALUES = new Set<unknown>([true, 'true', 1, '1', 'yes'])

let posthogSdk: PostHog | null = null
let posthogSdkLoad: Promise<void> | null = null
let currentRoute: string | null = null

/**
 * Loads the consent-gated posthog-js SDK on demand. The SDK is ~92 kB gzip and
 * nothing on the landing route needs it, so it must stay off the eager path;
 * every analyticsClient method below is a no-op until this resolves.
 */
function loadPostHogSdk(): Promise<void> {
  if (posthogSdk) return Promise.resolve()
  posthogSdkLoad ??= import('posthog-js').then(
    module => { posthogSdk = module.default },
    error => {
      posthogSdkLoad = null
      throw error
    },
  )
  return posthogSdkLoad
}

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
  sdk: PostHogWithQueues = (posthogSdk ?? {}) as unknown as PostHogWithQueues,
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

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value) && !(value instanceof Date)
}

function sanitizeValue(
  value: unknown,
  ancestors = new WeakSet<object>(),
  personPropertyMap = false,
  isEventRoot = false,
  sdkEventProperties = false,
): unknown {
  if (!value || typeof value !== 'object') return value
  if (value instanceof Date) return new Date(value.getTime())
  if (ancestors.has(value)) return null
  ancestors.add(value)

  if (Array.isArray(value)) {
    const sanitized = value.map(child => sanitizeValue(child, ancestors))
    ancestors.delete(value)
    return sanitized
  }

  const record = value as Record<string, unknown>
  const isSetEvent = isEventRoot && record.event === '$set'
  const sanitized: Record<string, unknown> = {}
  for (const [key, child] of Object.entries(record)) {
    const normalized = key.toLowerCase()
    // `$unset` is not a person property: preserve the array verbatim wherever it appears.
    if (normalized === '$unset') {
      sanitized[key] = child
      continue
    }
    // Person properties are default-deny: only the allowlist survives, and the old
    // `isPiiKey` denylist does NOT gate them. Applies to the keys of a `$set`/`$set_once`
    // sub-object and to the direct properties of an event NAMED `$set`.
    if (personPropertyMap && !ALLOWED_PERSON_PROPERTIES.has(key)) {
      // A `$set`-named event's own properties also carry the SDK envelope: the
      // transport keys every capture needs to be ingested (`token` is required or the
      // SDK drops the event) and the `$set`/`$set_once` sub-maps the SDK's
      // setPersonProperties/unsetPersonProperties calls actually send.
      const isSdkEnvelopeKey = sdkEventProperties
        && (SDK_TRANSPORT_PROPERTIES.has(key) || PERSON_PROPERTY_MAPS.has(normalized))
      if (!isSdkEnvelopeKey) continue
    }
    if (PERSON_PROPERTY_MAPS.has(normalized)) {
      // Fail closed: a non-record `$set`/`$set_once` cannot be allowlisted key-by-key,
      // and generic recursion would apply only the weaker `isPiiKey` denylist.
      if (isPlainRecord(child)) sanitized[key] = sanitizeValue(child, ancestors, true)
      continue
    }
    if (isSetEvent && normalized === 'properties') {
      if (isPlainRecord(child)) sanitized[key] = sanitizeValue(child, ancestors, true, false, true)
      continue
    }
    // `$heatmap_data` is URL-keyed; remap each key and drop sensitive routes.
    if (normalized === '$heatmap_data' && isPlainRecord(child) && !ancestors.has(child)) {
      ancestors.add(child)
      const heatmap: Record<string, unknown> = {}
      for (const [urlKey, data] of Object.entries(child)) {
        if (isSensitiveAnalyticsPath(urlKey)) continue
        heatmap[sanitizeAnalyticsUrl(urlKey)] = sanitizeValue(data, ancestors)
      }
      ancestors.delete(child)
      sanitized[key] = heatmap
      continue
    }
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
    capture_performance: true,
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
      return sanitizeValue(event, new WeakSet(), false, true) as typeof event
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

export function grantProductAnalytics(
  scope: string,
  distinctId?: string,
  personProperties?: Properties,
): Promise<void> {
  return analyticsLifecycle.apply({
    scope,
    purposes: { analytics: true, identifiedDiagnostics: false, interactionCapture: false },
    ...(distinctId ? { distinctId } : {}),
    ...(personProperties ? { personProperties } : {}),
  })
}

export function applyProductAnalyticsAuthority(authority: AnalyticsAuthority): Promise<void> {
  return analyticsLifecycle.apply(authority)
}

export function clearProductAnalyticsWithdrawal(scope: string): void {
  analyticsLifecycle.clearWithdrawal(scope)
}

function applyHeatmapCapture(analyticsEnabled: boolean): void {
  const enabled = analyticsEnabled
    && currentRoute !== null
    && isAnonymousAnalyticsPath(currentRoute)
  posthogSdk?.set_config({ capture_heatmaps: enabled })
}

/**
 * Records the route before gating heatmaps, so a mid-page analytics re-grant can
 * re-enable heatmaps on an anonymous route without waiting for a pageview.
 */
export function setRoute(url: string): void {
  currentRoute = url
  if (getAnalyticsLifecycleState() !== 'granted') return
  applyHeatmapCapture(true)
}

export function denyProductAnalytics(): void {
  analyticsLifecycle.deny()
}

export function withdrawProductAnalytics(scope: string, purpose: string = 'all'): void {
  analyticsLifecycle.withdraw(scope)
  try {
    localStorage.setItem(
      WITHDRAWAL_STORAGE_KEY,
      JSON.stringify({ scope, purpose, nonce: Date.now() }),
    )
    localStorage.removeItem(WITHDRAWAL_STORAGE_KEY)
  } catch {
    // Local denial is authoritative even when cross-tab storage is unavailable.
  }
}

export function listenForAnalyticsWithdrawal(
  onWithdrawal: (scope: string, purpose: string) => void =
    scope => analyticsLifecycle.withdraw(scope),
): () => void {
  function handleStorage(event: StorageEvent): void {
    if (event.key === WITHDRAWAL_STORAGE_KEY && event.newValue !== null) {
      try {
        const message = JSON.parse(event.newValue) as { scope?: unknown; purpose?: unknown }
        if (typeof message.scope === 'string') {
          onWithdrawal(
            message.scope,
            typeof message.purpose === 'string' ? message.purpose : 'all',
          )
        }
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
    analyticsClient.stopSessionRecording()
    return
  }
  const safeUrl = sanitizeAnalyticsUrl(url)
  analyticsLifecycle.capture(ANALYTICS_EVENTS.POSTHOG_PAGEVIEW, {
    $current_url: safeUrl,
    $pathname: pathFromUrl(safeUrl).pathname,
  })
}

export function captureProductEvent<Event extends AnalyticsEventName>(
  event: Event,
  ...[properties]: AnalyticsEventArguments<Event>
): void {
  const normalized = normalizeAnalyticsEventPayload(event, properties)
  if (normalized === null) return
  analyticsLifecycle.capture(event, normalized)
}

export function setProductAnalyticsPersonProperties(properties: Properties): void {
  analyticsLifecycle.setPersonProperties(properties)
}

/** Test-only reset; production authority transitions use pending/grant/deny above. */
export function resetAnalyticsLifecycleForTests(): void {
  analyticsLifecycle.resetForTests(true)
  currentRoute = null
}

const analyticsCaptureGate = createAnalyticsCaptureGate()

const analyticsClient = {
  capture: (event: string, properties?: Properties) => posthogSdk?.capture(event, properties),
  clearLegacyConsent: () => posthogSdk?.clear_opt_in_out_capturing(),
  clearPersistence: () => {
    const persistenceName = posthogSdk?.config.persistence_name
      || posthogSdk?.config.token
      || window.ENV?.POSTHOG_KEY
      || import.meta.env.VITE_POSTHOG_KEY
    if (persistenceName) clearPostHogSessionPersistence(persistenceName)
  },
  clearQueuedEvents: clearPostHogQueuedEvents,
  identify: (distinctId: string, properties?: Properties) => posthogSdk?.identify(distinctId, properties),
  init: (key: string, config: Partial<PostHogConfig>) => {
    if (!posthogSdk) return false
    clearPostHogSessionPersistence(config.persistence_name || key)
    const initialized = posthogSdk.init(key, config)
    guardPostHogRetryQueue(
      posthogSdk as unknown as PostHogWithQueues,
      analyticsCaptureGate.getGeneration,
      analyticsCaptureGate.isGranted,
    )
    return initialized
  },
  ready: loadPostHogSdk,
  reset: (resetDeviceId: boolean) => posthogSdk?.reset(resetDeviceId),
  setCaptureHeatmaps: (analyticsEnabled: boolean) => applyHeatmapCapture(analyticsEnabled),
  setPersistenceEnabled: (enabled: boolean) => {
    if (posthogSdk) setPostHogPersistenceEnabled(posthogSdk, enabled)
  },
  setPersonProperties: (properties: Properties) => posthogSdk?.setPersonProperties(properties),
  startSessionRecording: () => posthogSdk?.startSessionRecording(),
  stopSessionRecording: () => posthogSdk?.stopSessionRecording(),
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
