import {
  PostHog,
  type DisplaySurveyType,
  type PostHogConfig,
  type Survey,
} from 'posthog-js'
import {
  clearPostHogQueuedEvents,
  clearPostHogSessionPersistence,
  guardPostHogRetryQueue,
  posthogConfig,
  sanitizeAnalyticsUrl,
  type PostHogWithQueues,
} from '../lib/posthog'

export interface VerifiedPostHogIdentity {
  distinct_id: string
  identity_hash: string
}

const POPOVER_SURVEY_DISPLAY = 'popover' as typeof DisplaySurveyType.Popover
const PINNED_POSTHOG_VERSION = '1.430.2'
const SUPPORT_PERSISTENCE_NAME = 'boostedbudget_support'
const SURVEY_PERSISTENCE_NAME = 'boostedbudget_surveys'
const SURVEY_STORAGE_KEYS = ['lastSeenSurveyDate']
const SURVEY_STORAGE_PREFIXES = ['seenSurvey_', 'inProgressSurvey_', 'abandonedSurvey_']

export interface EngagementPostHogClient {
  setIdentity(distinctId: string, identityHash: string): void
  identify(distinctId: string): void
  shutdown(options?: { preserveSurveyState?: boolean }): Promise<void>
  conversations: {
    hide(): void
    show(): void
    isAvailable(): boolean
    reset(): void
  }
  surveys: {
    dispose(): void
    reset(): void
    getActiveMatchingSurveys(callback: (surveys: Survey[]) => void): void
    displaySurvey(surveyId: string, options: {
      displayType: typeof DisplaySurveyType.Popover
      ignoreConditions: boolean
      ignoreDelay: boolean
    }): void
  }
}

export interface PostHogEngagementController {
  activateSupport(identity: VerifiedPostHogIdentity): void
  deactivateSupport(): void
  activateSurveys(identity: VerifiedPostHogIdentity): void
  deactivateSurveys(): void
  suspendSurveys(): void
  getSupportClient(): EngagementPostHogClient | null
  isSupportAvailable(): boolean
  showSupport(): void
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim() !== ''
}

function isSupportedSurveyQuestion(question: unknown): boolean {
  if (!isRecord(question) || !isNonEmptyString(question.question)) return false
  if (question.type === 'rating') {
    return (question.display === 'number' || question.display === 'emoji')
      && [2, 3, 5, 7, 10].includes(question.scale as number)
      && typeof question.lowerBoundLabel === 'string'
      && typeof question.upperBoundLabel === 'string'
  }
  if (question.type !== 'single_choice' && question.type !== 'multiple_choice') return false
  return Array.isArray(question.choices)
    && question.choices.length > 0
    && question.choices.every(isNonEmptyString)
    && (question.hasOpenChoice === undefined || question.hasOpenChoice === false)
}

export function isSupportedSurvey(survey: unknown): survey is Survey {
  if (!isRecord(survey) || !isNonEmptyString(survey.id)) return false
  return Array.isArray(survey.questions)
    && survey.questions.length > 0
    && survey.questions.every(isSupportedSurveyQuestion)
}

function sharedIsolatedConfig() {
  return {
    ...posthogConfig(() => false),
    capture_pageview: false,
    capture_pageleave: false,
    autocapture: false,
    capture_exceptions: false,
    disable_session_recording: true,
    disable_web_experiments: true,
    disable_product_tours: true,
    capture_heatmaps: false,
    capture_dead_clicks: false,
    before_send: () => null,
  }
}

interface InternalResponse {
  [key: string]: unknown
  statusCode: number
  error?: Error
}

type InternalRequest = {
  callback?: (response: InternalResponse) => void
  data?: unknown
  fetchOptions?: NonNullable<PostHogConfig['fetch_options']> & { signal?: AbortSignal }
  timeout?: number
  url?: string
} & Record<string, unknown>

type InternalPostHog = {
  version: string
  config: PostHog['config'] & {
    identity_distinct_id?: string
    identity_hash?: string
  }
  _send_request(options: InternalRequest): void
}

export interface PinnedPostHogRequestBoundary {
  isActive(): boolean
  shutdown(): void
}

function sanitizeConversationsPayload(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sanitizeConversationsPayload)
  if (!value || typeof value !== 'object') return value
  const sanitized: Record<string, unknown> = {}
  for (const [key, child] of Object.entries(value)) {
    if (key === 'session_id' || key === 'session_replay_url' || key === 'widget_session_id') continue
    if ((key === 'current_url' || key === 'request_url') && typeof child === 'string') {
      sanitized[key] = sanitizeAnalyticsUrl(child)
      continue
    }
    sanitized[key] = sanitizeConversationsPayload(child)
  }
  return sanitized
}

function inactiveResponse(): InternalResponse {
  return {
    statusCode: 0,
    error: new DOMException('PostHog product authority ended', 'AbortError'),
  }
}

export function installPinnedPostHogRequestBoundary(client: PostHog): PinnedPostHogRequestBoundary {
  const internal = client as unknown as InternalPostHog
  if (internal.version !== PINNED_POSTHOG_VERSION) {
    throw new Error(`Unsupported posthog-js version; expected ${PINNED_POSTHOG_VERSION}`)
  }
  if (typeof internal._send_request !== 'function') {
    throw new Error('Unsupported posthog-js internals; expected _send_request transport')
  }
  const send = internal._send_request.bind(internal)
  let active = true
  const aborters = new Set<AbortController>()
  const timers = new Set<ReturnType<typeof setTimeout>>()
  const settleOnShutdown = new Set<() => void>()

  internal._send_request = (options) => {
    if (!active) {
      options.callback?.(inactiveResponse())
      return
    }
    const isConversationsRequest = options.url?.includes('/api/conversations/') === true
    if (isConversationsRequest
      && (!internal.config.identity_distinct_id || !internal.config.identity_hash)) {
      options.callback?.(inactiveResponse())
      return
    }

    const aborter = new AbortController()
    aborters.add(aborter)
    const requestTimeout = typeof options.timeout === 'number' && options.timeout > 0
      ? options.timeout
      : 60_000
    const timeout = setTimeout(() => aborter.abort(), requestTimeout)
    timers.add(timeout)
    const callback = options.callback
    let settled = false
    const settleInactive = () => {
      if (!callback || settled) return
      settled = true
      settleOnShutdown.delete(settleInactive)
      callback(inactiveResponse())
    }
    if (callback) settleOnShutdown.add(settleInactive)
    send({
      ...options,
      data: isConversationsRequest ? sanitizeConversationsPayload(options.data) : options.data,
      fetchOptions: { ...options.fetchOptions, signal: aborter.signal },
      callback: response => {
        aborters.delete(aborter)
        clearTimeout(timeout)
        timers.delete(timeout)
        settleOnShutdown.delete(settleInactive)
        if (!callback || settled) return
        settled = true
        callback(active ? response as InternalResponse : inactiveResponse())
      },
    })
  }

  return {
    isActive: () => active,
    shutdown() {
      if (!active) return
      active = false
      for (const timer of timers) clearTimeout(timer)
      timers.clear()
      for (const aborter of aborters) aborter.abort()
      aborters.clear()
      for (const settle of [...settleOnShutdown]) settle()
      settleOnShutdown.clear()
    },
  }
}

function clearStorageKeys(storage: Storage, keys: string[], prefixes: string[]): void {
  try {
    for (const key of keys) storage.removeItem(key)
    for (let index = storage.length - 1; index >= 0; index -= 1) {
      const key = storage.key(index)
      if (key && prefixes.some(prefix => key.startsWith(prefix))) storage.removeItem(key)
    }
  } catch {
    // Storage may be unavailable in hardened browser contexts.
  }
}

function captureSurveyDisplayState(): Map<string, string> {
  const snapshot = new Map<string, string>()
  try {
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index)
      if (!key) continue
      const tracked = SURVEY_STORAGE_KEYS.includes(key)
        || SURVEY_STORAGE_PREFIXES.some(prefix => key.startsWith(prefix))
      const value = tracked ? localStorage.getItem(key) : null
      if (value !== null) snapshot.set(key, value)
    }
  } catch {
    // Storage may be unavailable in hardened browser contexts.
  }
  return snapshot
}

function restoreSurveyDisplayState(snapshot: Map<string, string>): void {
  try {
    for (const [key, value] of snapshot) localStorage.setItem(key, value)
  } catch {
    // Storage may be unavailable in hardened browser contexts.
  }
}

export function clearIsolatedSurveyState(): void {
  clearPostHogSessionPersistence(SURVEY_PERSISTENCE_NAME)
  clearStorageKeys(localStorage, SURVEY_STORAGE_KEYS, SURVEY_STORAGE_PREFIXES)
}

function clearIsolatedSupportState(token: string): void {
  clearPostHogSessionPersistence(SUPPORT_PERSISTENCE_NAME)
  clearStorageKeys(localStorage, [`ph_conv_${token}`], [])
}

function adaptPostHogClient(
  client: PostHog,
  boundary: PinnedPostHogRequestBoundary,
  product: 'support' | 'surveys',
  token: string,
  removeWindowListeners: () => void,
): EngagementPostHogClient {
  const stopRetryGuard = guardPostHogRetryQueue(
    client as unknown as PostHogWithQueues,
    () => 0,
    boundary.isActive,
  )
  return {
    setIdentity: (distinctId, hash) => client.setIdentity(distinctId, hash),
    identify: distinctId => client.identify(distinctId),
    shutdown: async (options) => {
      const preservedSurveyState = options?.preserveSurveyState
        ? captureSurveyDisplayState()
        : null
      boundary.shutdown()
      removeWindowListeners()
      if (product === 'support') client.conversations.reset()
      else {
        client.surveys.reset()
        client.surveys.dispose()
      }
      clearPostHogQueuedEvents(client as unknown as PostHogWithQueues)
      client.set_config({ disable_persistence: true })
      client.reset(true)
      clearPostHogQueuedEvents(client as unknown as PostHogWithQueues)
      const shutdown = client.shutdown()
      clearPostHogQueuedEvents(client as unknown as PostHogWithQueues)
      stopRetryGuard()
      if (product === 'support') clearIsolatedSupportState(token)
      else clearIsolatedSurveyState()
      if (preservedSurveyState) restoreSurveyDisplayState(preservedSurveyState)
      await shutdown
    },
    conversations: {
      hide: () => client.conversations.hide(),
      show: () => client.conversations.show(),
      isAvailable: () => client.conversations.isAvailable(),
      reset: () => client.conversations.reset(),
    },
    surveys: {
      dispose: () => client.surveys.dispose(),
      reset: () => client.surveys.reset(),
      getActiveMatchingSurveys: callback => client.getActiveMatchingSurveys(callback),
      displaySurvey: (surveyId, options) => client.displaySurvey(surveyId, options),
    },
  }
}

function resetSupportClient(client: EngagementPostHogClient): void {
  client.conversations.hide()
  void client.shutdown()
}

function resetSurveyClient(
  client: EngagementPostHogClient,
  options?: { preserveSurveyState?: boolean },
): void {
  client.surveys.dispose()
  void client.shutdown(options)
}

interface IsolatedClientDependencies {
  createPostHog?: () => PostHog
  host?: string
  key?: string
}

interface WindowListenerRegistration {
  listener: Parameters<Window['addEventListener']>[1]
  options?: Parameters<Window['addEventListener']>[2]
  type: string
}

function initializeWithCapturedWindowListeners(initialize: () => void): () => void {
  const registrations: WindowListenerRegistration[] = []
  const originalAdd = window.addEventListener
  const originalRemove = window.removeEventListener
  const removeCapturedListeners = () => {
    for (const registration of registrations.splice(0)) {
      originalRemove.call(window, registration.type, registration.listener, registration.options)
    }
  }
  window.addEventListener = ((
    type: string,
    listener: Parameters<Window['addEventListener']>[1],
    options?: Parameters<Window['addEventListener']>[2],
  ) => {
    registrations.push({ type, listener, options })
    originalAdd.call(window, type, listener, options)
  }) as typeof window.addEventListener
  window.removeEventListener = ((
    type: string,
    listener: Parameters<Window['removeEventListener']>[1],
    options?: Parameters<Window['removeEventListener']>[2],
  ) => {
    const index = registrations.findIndex(registration => (
      registration.type === type && registration.listener === listener
    ))
    if (index >= 0) registrations.splice(index, 1)
    originalRemove.call(window, type, listener, options)
  }) as typeof window.removeEventListener

  try {
    initialize()
  } catch (error) {
    removeCapturedListeners()
    throw error
  } finally {
    window.addEventListener = originalAdd
    window.removeEventListener = originalRemove
  }

  return removeCapturedListeners
}

function discardPartialClient(
  client: PostHog,
  boundary: PinnedPostHogRequestBoundary,
  product: 'support' | 'surveys',
  token: string,
  removeWindowListeners: () => void,
): void {
  boundary.shutdown()
  removeWindowListeners()
  try {
    if (product === 'support') client.conversations.reset()
    else {
      client.surveys.reset()
      client.surveys.dispose()
    }
  } catch {
    // Continue through every cleanup boundary after a partial SDK initialization.
  }
  clearPostHogQueuedEvents(client as unknown as PostHogWithQueues)
  try {
    client.set_config({ disable_persistence: true })
    client.reset(true)
  } catch {
    // The pinned SDK may not have completed enough initialization to reset normally.
  }
  clearPostHogQueuedEvents(client as unknown as PostHogWithQueues)
  try {
    void client.shutdown().catch(() => {})
  } catch {
    // A synchronous shutdown failure must not prevent app-owned storage cleanup.
  }
  if (product === 'support') clearIsolatedSupportState(token)
  else clearIsolatedSurveyState()
}

export function createIsolatedSupportClient(
  dependencies: IsolatedClientDependencies = {},
): EngagementPostHogClient {
  const key = dependencies.key
    || window.ENV?.POSTHOG_KEY
    || import.meta.env.VITE_POSTHOG_KEY
  if (!key) throw new Error('PostHog Support is not configured')
  clearIsolatedSupportState(key)
  const client = dependencies.createPostHog?.() ?? new PostHog()
  const boundary = installPinnedPostHogRequestBoundary(client)
  let removeWindowListeners = () => {}
  try {
    removeWindowListeners = initializeWithCapturedWindowListeners(() => {
      client.init(key, {
        ...sharedIsolatedConfig(),
        api_host: dependencies.host
          || window.ENV?.POSTHOG_HOST
          || import.meta.env.VITE_POSTHOG_HOST
          || 'https://us.i.posthog.com',
        advanced_disable_flags: false,
        disable_conversations: false,
        disable_surveys: true,
        disable_surveys_automatic_display: true,
        // Conversations is the only PostHog extension that this service client may load.
        disable_external_dependency_loading: false,
        persistence_name: SUPPORT_PERSISTENCE_NAME,
        api_transport: 'fetch',
        disable_beacon: true,
      })
    })
    client.get_session_id = () => ''
    client.get_session_replay_url = () => ''
    return adaptPostHogClient(client, boundary, 'support', key, removeWindowListeners)
  } catch (error) {
    discardPartialClient(client, boundary, 'support', key, removeWindowListeners)
    throw error
  }
}

export function createIsolatedSurveyClient(
  dependencies: IsolatedClientDependencies = {},
): EngagementPostHogClient {
  const key = dependencies.key
    || window.ENV?.POSTHOG_KEY
    || import.meta.env.VITE_POSTHOG_KEY
  if (!key) throw new Error('PostHog Surveys are not configured')
  const consentedConfig = posthogConfig(() => true)
  const client = dependencies.createPostHog?.() ?? new PostHog()
  const boundary = installPinnedPostHogRequestBoundary(client)
  let removeWindowListeners = () => {}
  try {
    removeWindowListeners = initializeWithCapturedWindowListeners(() => {
      client.init(key, {
        ...consentedConfig,
        api_host: dependencies.host
          || window.ENV?.POSTHOG_HOST
          || import.meta.env.VITE_POSTHOG_HOST
          || 'https://us.i.posthog.com',
        advanced_disable_flags: false,
        disable_persistence: false,
        persistence: 'sessionStorage',
        persistence_name: SURVEY_PERSISTENCE_NAME,
        disable_conversations: true,
        disable_surveys: false,
        // Never allow PostHog to auto-render a remotely-created free-text survey.
        disable_surveys_automatic_display: true,
        disable_external_dependency_loading: false,
        api_transport: 'fetch',
        disable_beacon: true,
      })
    })
    return adaptPostHogClient(client, boundary, 'surveys', key, removeWindowListeners)
  } catch (error) {
    discardPartialClient(client, boundary, 'surveys', key, removeWindowListeners)
    throw error
  }
}

export function createPostHogEngagementController({
  createSupportClient = createIsolatedSupportClient,
  createSurveyClient = createIsolatedSurveyClient,
}: {
  createSupportClient?: () => EngagementPostHogClient
  createSurveyClient?: () => EngagementPostHogClient
} = {}): PostHogEngagementController {
  let supportClient: EngagementPostHogClient | null = null
  let supportIdentity: string | null = null
  let surveyClient: EngagementPostHogClient | null = null
  let surveyIdentity: string | null = null
  let surveyGeneration = 0

  function deactivateSupport(): void {
    document.body.classList.remove('posthog-support-widget-active')
    if (!supportClient) return
    resetSupportClient(supportClient)
    supportClient = null
    supportIdentity = null
  }

  function deactivateSurveys(): void {
    surveyGeneration += 1
    if (surveyClient) resetSurveyClient(surveyClient)
    surveyClient = null
    surveyIdentity = null
    clearIsolatedSurveyState()
  }

  return {
    activateSupport(identity) {
      if (supportClient && supportIdentity === identity.distinct_id) return
      deactivateSupport()
      let client: EngagementPostHogClient | null = null
      try {
        client = createSupportClient()
        client.setIdentity(identity.distinct_id, identity.identity_hash)
        supportClient = client
        supportIdentity = identity.distinct_id
      } catch {
        if (client) resetSupportClient(client)
      }
    },
    deactivateSupport,
    activateSurveys(identity) {
      if (surveyClient && surveyIdentity === identity.distinct_id) return
      surveyGeneration += 1
      if (surveyClient) resetSurveyClient(surveyClient)
      surveyClient = null
      if (surveyIdentity !== identity.distinct_id) clearIsolatedSurveyState()
      surveyIdentity = null
      const generation = surveyGeneration
      let client: EngagementPostHogClient | null = null
      try {
        client = createSurveyClient()
        client.identify(identity.distinct_id)
        surveyClient = client
        surveyIdentity = identity.distinct_id
      } catch {
        if (client) resetSurveyClient(client)
        return
      }
      client.surveys.getActiveMatchingSurveys(surveys => {
        if (generation !== surveyGeneration || client !== surveyClient) return
        const survey = surveys.find(isSupportedSurvey)
        if (!survey) return
        client.surveys.displaySurvey(survey.id, {
          displayType: POPOVER_SURVEY_DISPLAY,
          ignoreConditions: false,
          ignoreDelay: false,
        })
      })
    },
    deactivateSurveys,
    suspendSurveys() {
      surveyGeneration += 1
      if (!surveyClient) return
      resetSurveyClient(surveyClient, { preserveSurveyState: true })
      surveyClient = null
    },
    getSupportClient: () => supportClient,
    isSupportAvailable: () => supportClient?.conversations.isAvailable() === true,
    showSupport() {
      if (!supportClient?.conversations.isAvailable()) return
      // CSS keeps the vendor-rendered project button suppressed until this
      // authenticated, app-owned control deliberately hands off to it.
      document.body.classList.add('posthog-support-widget-active')
      supportClient.conversations.show()
    },
  }
}
