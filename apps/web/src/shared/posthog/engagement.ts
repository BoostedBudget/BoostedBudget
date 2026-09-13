import {
  PostHog,
  type DisplaySurveyType,
  type Survey,
} from 'posthog-js'
import {
  clearPostHogQueuedEvents,
  posthogConfig,
  sanitizeAnalyticsUrl,
  type PostHogWithQueues,
} from '../lib/posthog'

export interface VerifiedPostHogIdentity {
  distinct_id: string
  identity_hash: string
}

const POPOVER_SURVEY_DISPLAY = 'popover' as typeof DisplaySurveyType.Popover

const EXCLUDED_ENGAGEMENT_ROUTES = [
  /^\/$/,
  /^\/(?:oauth-)?callback(?:\/|$)/,
  /^\/(?:pricing|mcp|privacy)(?:\/|$)/,
  /^\/legal(?:\/|$)/,
  /^\/sign-(?:in|up)(?:\/|$)/,
  /^\/admin(?:\/|$)/,
  /^\/cancel-deletion(?:\/|$)/,
  /^\/households?\/invite(?:s)?(?:\/|$)/,
]

export function isPostHogEngagementRoute(pathname: string): boolean {
  return !EXCLUDED_ENGAGEMENT_ROUTES.some(pattern => pattern.test(pathname))
}

export interface EngagementPostHogClient {
  setIdentity(distinctId: string, identityHash: string): void
  identify(distinctId: string): void
  shutdown(): Promise<void>
  conversations: {
    hide(): void
    show(): void
    isAvailable(): boolean
  }
  surveys: {
    dispose(): void
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

export function isSupportedSurvey(survey: Survey): boolean {
  return survey.questions.length > 0 && survey.questions.every(question => {
    if (question.type === 'rating') return true
    if (question.type === 'single_choice' || question.type === 'multiple_choice') {
      return question.hasOpenChoice !== true
    }
    return false
  })
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

type InternalRequest = { callback?: (response: unknown) => void } & Record<string, unknown>
type InternalPostHog = PostHog & {
  _send_request(options: InternalRequest): void
  conversations: PostHog['conversations'] & {
    _conversationsManager?: { _currentUrl?: () => string }
  }
}

function guardTransport(client: PostHog): () => void {
  const internal = client as InternalPostHog
  const send = internal._send_request.bind(client)
  let active = true
  internal._send_request = (options) => {
    if (!active) return
    const callback = options.callback
    send({
      ...options,
      callback: callback ? response => {
        if (active) callback(response)
      } : undefined,
    })
  }
  return () => { active = false }
}

export function installConversationsPrivacyBoundary(client: PostHog): void {
  const manager = (client as InternalPostHog).conversations._conversationsManager
  if (!manager || typeof manager._currentUrl !== 'function') {
    throw new Error('Unsupported posthog-js Conversations internals; expected 1.430.2 privacy boundary')
  }
  manager._currentUrl = () => sanitizeAnalyticsUrl(window.location.href)
  client.get_session_id = () => undefined
  client.get_session_replay_url = () => null
}

function adaptPostHogClient(client: PostHog, stopTransport: () => void): EngagementPostHogClient {
  return {
    setIdentity: (distinctId, hash) => client.setIdentity(distinctId, hash),
    identify: distinctId => client.identify(distinctId),
    shutdown: async () => {
      stopTransport()
      clearPostHogQueuedEvents(client as unknown as PostHogWithQueues)
      client.set_config({ disable_persistence: true })
      client.reset(true)
      await client.shutdown()
      clearPostHogQueuedEvents(client as unknown as PostHogWithQueues)
    },
    conversations: {
      hide: () => client.conversations.hide(),
      show: () => client.conversations.show(),
      isAvailable: () => {
        if (!client.conversations.isAvailable()) return false
        installConversationsPrivacyBoundary(client)
        return true
      },
    },
    surveys: {
      dispose: () => client.surveys.dispose(),
      getActiveMatchingSurveys: callback => client.getActiveMatchingSurveys(callback),
      displaySurvey: (surveyId, options) => client.displaySurvey(surveyId, options),
    },
  }
}

function resetSupportClient(client: EngagementPostHogClient): void {
  client.conversations.hide()
  void client.shutdown()
}

function resetSurveyClient(client: EngagementPostHogClient): void {
  client.surveys.dispose()
  void client.shutdown()
}

export function createIsolatedSupportClient(): EngagementPostHogClient {
  const key = window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY
  if (!key) throw new Error('PostHog Support is not configured')
  const client = new PostHog()
  const stopTransport = guardTransport(client)
  client.init(key, {
    ...sharedIsolatedConfig(),
    api_host: window.ENV?.POSTHOG_HOST || import.meta.env.VITE_POSTHOG_HOST || 'https://us.i.posthog.com',
    disable_conversations: false,
    disable_surveys: true,
    disable_surveys_automatic_display: true,
    // Conversations is the only PostHog extension that this service client may load.
    disable_external_dependency_loading: false,
    persistence_name: 'boostedbudget_support',
  })
  return adaptPostHogClient(client, stopTransport)
}

export function createIsolatedSurveyClient(): EngagementPostHogClient {
  const key = window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY
  if (!key) throw new Error('PostHog Surveys are not configured')
  const consentedConfig = posthogConfig(() => true)
  const client = new PostHog()
  const stopTransport = guardTransport(client)
  client.init(key, {
    ...consentedConfig,
    api_host: window.ENV?.POSTHOG_HOST || import.meta.env.VITE_POSTHOG_HOST || 'https://us.i.posthog.com',
    disable_persistence: false,
    persistence: 'sessionStorage',
    persistence_name: 'boostedbudget_surveys',
    disable_conversations: true,
    disable_surveys: false,
    // Never allow PostHog to auto-render a remotely-created free-text survey.
    disable_surveys_automatic_display: true,
    disable_external_dependency_loading: false,
  })
  return adaptPostHogClient(client, stopTransport)
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
    if (!surveyClient) return
    resetSurveyClient(surveyClient)
    surveyClient = null
    surveyIdentity = null
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
      deactivateSurveys()
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
      surveyClient?.surveys.dispose()
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
