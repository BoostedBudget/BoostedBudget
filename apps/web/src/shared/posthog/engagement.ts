import posthog, {
  DisplaySurveyType,
  type PostHog,
  type Survey,
  SurveyQuestionType,
} from 'posthog-js'
import {
  clearPostHogQueuedEvents,
  posthogConfig,
  type PostHogWithQueues,
} from '../lib/posthog'

export interface VerifiedPostHogIdentity {
  distinct_id: string
  identity_hash: string
}

export interface EngagementPostHogClient {
  setIdentity(distinctId: string, identityHash: string): void
  clearIdentity(): void
  identify(distinctId: string): void
  reset(resetDeviceId?: boolean): void
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
  getSupportClient(): EngagementPostHogClient | null
  isSupportAvailable(): boolean
  showSupport(): void
}

const AUTHENTICATED_APP_PATHS = [
  '/dashboard',
  '/accounts',
  '/transactions',
  '/review',
  '/insights',
  '/planning',
  '/rules',
  '/activity',
  '/profile',
  '/billing',
  '/admin',
]

export function isAuthenticatedAppPath(pathname: string): boolean {
  return AUTHENTICATED_APP_PATHS.some(prefix => pathname === prefix || pathname.startsWith(`${prefix}/`))
}

export function isSupportedSurvey(survey: Survey): boolean {
  return survey.questions.length > 0 && survey.questions.every(question => {
    if (question.type === SurveyQuestionType.Rating) return true
    if (question.type === SurveyQuestionType.SingleChoice || question.type === SurveyQuestionType.MultipleChoice) {
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

function adaptPostHogClient(client: PostHog): EngagementPostHogClient {
  return {
    setIdentity: (distinctId, hash) => client.setIdentity(distinctId, hash),
    clearIdentity: () => client.clearIdentity(),
    identify: distinctId => client.identify(distinctId),
    reset: resetDeviceId => {
      clearPostHogQueuedEvents(client as unknown as PostHogWithQueues)
      client.set_config({ disable_persistence: true })
      client.reset(resetDeviceId)
    },
    conversations: {
      hide: () => client.conversations.hide(),
      show: () => client.conversations.show(),
      isAvailable: () => client.conversations.isAvailable(),
    },
    surveys: {
      dispose: () => client.surveys.dispose(),
      getActiveMatchingSurveys: callback => client.getActiveMatchingSurveys(callback),
      displaySurvey: (surveyId, options) => client.displaySurvey(surveyId, options),
    },
  }
}

let clientSequence = 0

export function createIsolatedSupportClient(): EngagementPostHogClient {
  const key = window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY
  if (!key) throw new Error('PostHog Support is not configured')
  const client = posthog.init(key, {
    ...sharedIsolatedConfig(),
    api_host: window.ENV?.POSTHOG_HOST || import.meta.env.VITE_POSTHOG_HOST || 'https://us.i.posthog.com',
    disable_conversations: false,
    disable_surveys: true,
    disable_surveys_automatic_display: true,
    // Conversations is the only PostHog extension that this service client may load.
    disable_external_dependency_loading: false,
  }, `authenticated_support_${++clientSequence}`)
  return adaptPostHogClient(client)
}

export function createIsolatedSurveyClient(): EngagementPostHogClient {
  const key = window.ENV?.POSTHOG_KEY || import.meta.env.VITE_POSTHOG_KEY
  if (!key) throw new Error('PostHog Surveys are not configured')
  const consentedConfig = posthogConfig(() => true)
  const client = posthog.init(key, {
    ...consentedConfig,
    api_host: window.ENV?.POSTHOG_HOST || import.meta.env.VITE_POSTHOG_HOST || 'https://us.i.posthog.com',
    disable_persistence: false,
    persistence: 'sessionStorage',
    disable_conversations: true,
    disable_surveys: false,
    // Never allow PostHog to auto-render a remotely-created free-text survey.
    disable_surveys_automatic_display: true,
    disable_external_dependency_loading: false,
  }, `consented_surveys_${++clientSequence}`)
  return adaptPostHogClient(client)
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
    supportClient.conversations.hide()
    supportClient.clearIdentity()
    supportClient.reset(true)
    supportClient = null
    supportIdentity = null
  }

  function deactivateSurveys(): void {
    surveyGeneration += 1
    if (!surveyClient) return
    surveyClient.surveys.dispose()
    surveyClient.reset(true)
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
        client?.conversations.hide()
        client?.clearIdentity()
        client?.reset(true)
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
        client?.surveys.dispose()
        client?.reset(true)
        return
      }
      client.surveys.getActiveMatchingSurveys(surveys => {
        if (generation !== surveyGeneration || client !== surveyClient) return
        const survey = surveys.find(isSupportedSurvey)
        if (!survey) return
        client.surveys.displaySurvey(survey.id, {
          displayType: DisplaySurveyType.Popover,
          ignoreConditions: false,
          ignoreDelay: false,
        })
      })
    },
    deactivateSurveys,
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
