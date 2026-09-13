import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import posthog, { PostHog, type Survey } from 'posthog-js'
import {
  clearIsolatedSurveyState,
  createIsolatedSupportClient,
  createIsolatedSurveyClient,
  createPostHogEngagementController,
  installPinnedPostHogRequestBoundary,
  isPostHogEngagementRoute,
  isSupportedSurvey,
  type EngagementPostHogClient,
} from './engagement'

type SupportedQuestionType = 'rating' | 'single_choice' | 'multiple_choice' | 'open' | 'link'

function surveyQuestion(type: SupportedQuestionType, hasOpenChoice: boolean) {
  if (type === 'rating') {
    return {
      type,
      question: 'Rate this',
      display: 'number',
      scale: 5,
      lowerBoundLabel: 'Low',
      upperBoundLabel: 'High',
    }
  }
  if (type === 'single_choice' || type === 'multiple_choice') {
    return { type, question: 'Choose', choices: ['A', 'B'], hasOpenChoice }
  }
  if (type === 'link') return { type, question: 'Continue' }
  return { type, question: 'Tell us' }
}

function surveyWith(types: SupportedQuestionType[], hasOpenChoice = false): Survey {
  return {
    id: 'survey-1',
    name: 'Safe survey',
    type: 'popover',
    feature_flag_keys: null,
    linked_flag_key: null,
    targeting_flag_key: null,
    internal_targeting_flag_key: null,
    questions: types.map(type => surveyQuestion(type, hasOpenChoice)),
    appearance: null,
    conditions: null,
    start_date: null,
    end_date: null,
    current_iteration: null,
    current_iteration_start_date: null,
  } as Survey
}

function fakeClient() {
  const client = {
    setIdentity: mock(() => {}),
    identify: mock(() => {}),
    shutdown: mock((_options?: { preserveSurveyState?: boolean }) => Promise.resolve()),
    conversations: {
      hide: mock(() => {}),
      show: mock(() => {}),
      isAvailable: mock(() => true),
      reset: mock(() => {}),
    },
    surveys: {
      dispose: mock(() => {}),
      reset: mock(() => {}),
      getActiveMatchingSurveys: mock((callback: (surveys: Survey[]) => void) => callback([])),
      displaySurvey: mock(() => {}),
    },
  }
  return client as unknown as EngagementPostHogClient & typeof client
}

describe('PostHog authenticated experiences policy', () => {
  test('enables product routes by default and excludes public, auth, callback, and admin routes', () => {
    for (const path of ['/dashboard', '/budgets/id', '/auto-categorization', '/boosts/new', '/billing/manage']) {
      expect(isPostHogEngagementRoute(path)).toBe(true)
    }
    for (const path of ['/', '/pricing', '/mcp', '/legal/terms', '/oauth-callback', '/admin/health', '/sign-in']) {
      expect(isPostHogEngagementRoute(path)).toBe(false)
    }
    expect(isPostHogEngagementRoute('/not-a-real-app-route')).toBe(false)
  })
  test('allows rating and closed choice questions but rejects every free-text path', () => {
    expect(isSupportedSurvey(surveyWith(['rating', 'single_choice', 'multiple_choice']))).toBe(true)
    expect(isSupportedSurvey(surveyWith(['open']))).toBe(false)
    expect(isSupportedSurvey(surveyWith(['link']))).toBe(false)
    expect(isSupportedSurvey(surveyWith(['single_choice'], true))).toBe(false)
    expect(isSupportedSurvey({
      ...surveyWith(['single_choice']),
      questions: [{
        ...surveyQuestion('single_choice', false),
        hasOpenChoice: 'true',
      }],
    } as never)).toBe(false)
  })

  test('the pinned SDK supplies reply polling with bounded cadence and generation cleanup', () => {
    const source = readFileSync(resolve(
      import.meta.dir,
      '../../../node_modules/posthog-js/lib/src/extensions/conversations/external/index.js',
    ), 'utf8')
    expect(source).toContain('POLL_INTERVAL_OPEN_MS = 5000')
    expect(source).toContain('POLL_INTERVAL_CLOSED_MS = 15000')
    expect(source).toContain('POLL_RATE_LIMIT_MAX_MS = 60000')
    expect(source).toContain('generation !== this._pollGeneration')
    expect(source).toContain('this._stopPolling()')
  })

  test('the pinned SDK clearIdentity path reloads tickets after entering anonymous mode', () => {
    const source = readFileSync(resolve(
      import.meta.dir,
      '../../../node_modules/posthog-js/lib/src/extensions/conversations/external/index.js',
    ), 'utf8')
    const clearIdentity = source.slice(
      source.indexOf('ConversationsManager.prototype.clearIdentity'),
      source.indexOf('ConversationsManager.prototype._resetConversationState'),
    )
    expect(clearIdentity).toContain('setIdentityMode(false)')
    expect(clearIdentity).toContain('this._loadTicketsAndReconcileView()')
  })
})

describe('PostHog isolated-client privacy boundary', () => {
  test('keeps remote config enabled for the isolated products that require it', async () => {
    const supportSdk = new PostHog()
    ;(supportSdk as unknown as { _send_request(options: Record<string, unknown>): void })
      ._send_request = () => {}
    const support = createIsolatedSupportClient({
      key: 'test-project-token',
      createPostHog: () => supportSdk,
    })
    expect(supportSdk.config.advanced_disable_flags).toBe(false)

    const surveySdk = new PostHog()
    ;(surveySdk as unknown as { _send_request(options: Record<string, unknown>): void })
      ._send_request = () => {}
    const surveys = createIsolatedSurveyClient({
      key: 'test-project-token',
      createPostHog: () => surveySdk,
    })
    expect(surveySdk.config.advanced_disable_flags).toBe(false)

    await support.shutdown()
    await surveys.shutdown()
  })

  test('removes every SDK window lifecycle listener captured during isolated init', async () => {
    const tracked = new Set<Parameters<Window['addEventListener']>[1]>()
    const originalAdd = window.addEventListener.bind(window)
    const originalRemove = window.removeEventListener.bind(window)
    window.addEventListener = ((type: string, listener: Parameters<Window['addEventListener']>[1], options?: Parameters<Window['addEventListener']>[2]) => {
      if (['beforeunload', 'pagehide', 'pageshow', 'unload'].includes(type)) tracked.add(listener)
      originalAdd(type, listener, options)
    }) as typeof window.addEventListener
    window.removeEventListener = ((type: string, listener: Parameters<Window['removeEventListener']>[1], options?: Parameters<Window['removeEventListener']>[2]) => {
      if (['beforeunload', 'pagehide', 'pageshow', 'unload'].includes(type)) tracked.delete(listener)
      originalRemove(type, listener, options)
    }) as typeof window.removeEventListener

    try {
      const sdk = new PostHog()
      ;(sdk as unknown as { _send_request(options: Record<string, unknown>): void })
        ._send_request = () => {}
      const surveys = createIsolatedSurveyClient({
        key: 'test-project-token',
        createPostHog: () => sdk,
      })
      expect(tracked.size).toBeGreaterThan(0)
      await surveys.shutdown()
      expect(tracked.size).toBe(0)
    } finally {
      window.addEventListener = originalAdd
      window.removeEventListener = originalRemove
    }
  })

  test('sanitizes every Conversations URL field at the outbound payload boundary', () => {
    const sent: Array<Record<string, unknown>> = []
    const client = new PostHog()
    Object.assign(client.config, { identity_distinct_id: 'user_A', identity_hash: 'signed-A' })
    const transportClient = client as unknown as {
      _send_request(options: Record<string, unknown>): void
    }
    transportClient._send_request = options => sent.push(options)
    const boundary = installPinnedPostHogRequestBoundary(client)

    for (const currentUrl of [
      'https://app.example.com/accounts/acct-secret?token=raw#private',
      'https://app.example.com/budgets/budget-secret?merchant=raw#private',
      'https://app.example.com/billing/success?checkout_id=secret#private',
      'https://app.example.com/households/invites/capability-secret?email=raw#private',
    ]) {
      transportClient._send_request({
        url: 'https://us.i.posthog.com/api/conversations/v1/widget/message',
        method: 'POST',
        data: {
          session_id: 'must-not-leave',
          session_context: {
            current_url: currentUrl,
            session_replay_url: 'https://us.posthog.com/replay/raw-secret',
          },
          current_url: currentUrl,
          request_url: currentUrl,
        },
      })
    }

    const serialized = JSON.stringify(sent)
    expect(serialized).not.toContain('acct-secret')
    expect(serialized).not.toContain('budget-secret')
    expect(serialized).not.toContain('capability-secret')
    expect(serialized).not.toContain('checkout_id')
    expect(serialized).not.toContain('?')
    expect(serialized).not.toContain('#')
    expect(serialized).not.toContain('session_replay_url')
    expect(serialized).not.toContain('session_id')
    expect(serialized).toContain('/accounts/:accountId')
    expect(serialized).toContain('/budgets/:budgetId')
    boundary.shutdown()
  })

  test('drops anonymous Conversations requests before they cross the network boundary', () => {
    const send = mock((_options: Record<string, unknown>) => {})
    const callback = mock(() => {})
    const client = {
      version: '1.430.2',
      config: {},
      _send_request: send,
    }
    installPinnedPostHogRequestBoundary(client as never)

    client._send_request({
      url: 'https://us.i.posthog.com/api/conversations/v1/widget/tickets?widget_session_id=anonymous',
      callback,
    })

    expect(send).not.toHaveBeenCalled()
    expect(callback).toHaveBeenCalledWith(expect.objectContaining({ statusCode: 0 }))
  })

  test('aborts in-flight work and generation-fences a late success callback', () => {
    let capturedOptions: Record<string, unknown> | undefined
    const callback = mock(() => {})
    const client = {
      version: '1.430.2',
      config: { identity_distinct_id: 'user_A', identity_hash: 'signed-A' },
      _send_request: (options: Record<string, unknown>) => { capturedOptions = options },
    }
    const boundary = installPinnedPostHogRequestBoundary(client as never)

    client._send_request({
      url: 'https://us.i.posthog.com/api/conversations/v1/widget/tickets',
      callback,
    })
    const signal = (capturedOptions?.fetchOptions as { signal: AbortSignal }).signal
    expect(signal.aborted).toBe(false)

    boundary.shutdown()
    expect(signal.aborted).toBe(true)
    expect(callback).toHaveBeenCalledTimes(1)
    expect(callback).toHaveBeenCalledWith(expect.objectContaining({ statusCode: 0 }))
    ;(capturedOptions?.callback as (response: unknown) => void)({ statusCode: 200, json: { ticket_id: 'stale' } })
    expect(callback).toHaveBeenCalledTimes(1)
  })

  test('fails loudly when the pinned private transport contract changes', () => {
    expect(() => installPinnedPostHogRequestBoundary({
      version: '1.430.3',
      _send_request: () => {},
    } as never)).toThrow(/1\.430\.2/)
    expect(() => installPinnedPostHogRequestBoundary({ version: '1.430.2' } as never))
      .toThrow(/_send_request/)
  })

  test('post-init compatibility failure disables transport and shuts the partial client down', () => {
    const send = mock((_options: Record<string, unknown>) => {})
    const shutdown = mock(() => Promise.resolve())
    const partialClient = {
      version: '1.430.2',
      config: {},
      _send_request: send,
      init(_key: string, config: Record<string, unknown>) {
        this.config = config
        return this
      },
      conversations: { reset: mock(() => {}) },
      surveys: { reset: mock(() => {}), dispose: mock(() => {}) },
      set_config: mock(() => {}),
      reset: mock(() => {}),
      shutdown,
      get_session_id: mock(() => ''),
      get_session_replay_url: mock(() => ''),
    }

    expect(() => createIsolatedSupportClient({
      key: 'test-project-token',
      createPostHog: () => partialClient as never,
    })).toThrow(/retry queue/i)

    partialClient._send_request({ url: 'https://us.i.posthog.com/flags' })
    expect(send).not.toHaveBeenCalled()
    expect(shutdown).toHaveBeenCalledTimes(1)
  })

  test('clears only isolated Survey UI state and preserves primary analytics persistence', () => {
    sessionStorage.setItem('ph_project-token_posthog', JSON.stringify({ distinct_id: 'primary-user' }))
    sessionStorage.setItem('ph_boostedbudget_surveys', JSON.stringify({ distinct_id: 'survey-user' }))
    localStorage.setItem('lastSeenSurveyDate', 'today')
    localStorage.setItem('seenSurvey_survey-1', 'true')
    localStorage.setItem('inProgressSurvey_survey-1', 'true')
    localStorage.setItem('abandonedSurvey_survey-1', 'true')

    clearIsolatedSurveyState()

    expect(sessionStorage.getItem('ph_project-token_posthog')).toContain('primary-user')
    expect(sessionStorage.getItem('ph_boostedbudget_surveys')).toBeNull()
    expect(localStorage.getItem('lastSeenSurveyDate')).toBeNull()
    expect(localStorage.getItem('seenSurvey_survey-1')).toBeNull()
    expect(localStorage.getItem('inProgressSurvey_survey-1')).toBeNull()
    expect(localStorage.getItem('abandonedSurvey_survey-1')).toBeNull()
    sessionStorage.removeItem('ph_project-token_posthog')
  })

  test('real SDK survey lifecycle cannot inherit or erase the primary identity', async () => {
    const primary = new PostHog()
    primary.init('test-project-token', {
      persistence: 'sessionStorage',
      persistence_name: 'boostedbudget_primary_test',
      disable_persistence: false,
      reuseAnonymousId: true,
      autocapture: false,
      capture_pageview: false,
      capture_pageleave: false,
      disable_session_recording: true,
      disable_surveys: true,
      disable_conversations: true,
      disable_external_dependency_loading: true,
      advanced_disable_flags: true,
      request_batching: false,
      before_send: () => null,
    })
    primary.identify('primary-user')

    const registryBefore = Object.keys(posthog).sort()
    const surveySdk = new PostHog()
    const surveyTransport = mock((_options: Record<string, unknown>) => {})
    ;(surveySdk as unknown as { _send_request: typeof surveyTransport })._send_request = surveyTransport
    const surveys = createIsolatedSurveyClient({
      key: 'test-project-token',
      createPostHog: () => surveySdk,
    })
    surveys.identify('survey-user')
    const primaryBefore = sessionStorage.getItem('ph_boostedbudget_primary_test')
    const surveyState = sessionStorage.getItem('ph_boostedbudget_surveys')
    expect(primaryBefore).toContain('primary-user')
    expect(surveyState).toContain('survey-user')
    expect(surveyState).not.toContain('primary-user')

    await surveys.shutdown()
    expect(sessionStorage.getItem('ph_boostedbudget_primary_test')).toBe(primaryBefore)
    expect(sessionStorage.getItem('ph_boostedbudget_surveys')).toBeNull()
    const requestsAtShutdown = surveyTransport.mock.calls.length
    window.dispatchEvent(new Event('pagehide'))
    expect(surveyTransport).toHaveBeenCalledTimes(requestsAtShutdown)
    expect(Object.keys(posthog).sort()).toEqual(registryBefore)

    await primary.shutdown()
    sessionStorage.removeItem('ph_boostedbudget_primary_test')
  })

  test('real SDK Support teardown purges verified and widget identity without anonymous traffic', async () => {
    const sdk = new PostHog()
    const transport = mock((_options: Record<string, unknown>) => {})
    const internal = sdk as unknown as {
      config: { identity_distinct_id?: string; identity_hash?: string }
      _send_request(options: Record<string, unknown>): void
    }
    internal._send_request = transport
    const support = createIsolatedSupportClient({
      key: 'test-project-token',
      createPostHog: () => sdk,
    })
    support.setIdentity('user_A', 'signed-A')
    localStorage.setItem('ph_conv_test-project-token', 'verified-widget-state')
    expect(internal.config.identity_distinct_id).toBe('user_A')

    await support.shutdown()
    const requestsAtShutdown = transport.mock.calls.length
    expect(internal.config.identity_distinct_id).toBeUndefined()
    expect(internal.config.identity_hash).toBeUndefined()
    expect(localStorage.getItem('ph_conv_test-project-token')).toBeNull()

    internal._send_request({
      url: 'https://us.i.posthog.com/api/conversations/v1/widget/tickets?widget_session_id=anonymous',
    })
    window.dispatchEvent(new Event('pagehide'))
    expect(transport).toHaveBeenCalledTimes(requestsAtShutdown)
  })

  test('real SDK route suspension preserves survey seen state while clearing its identity', async () => {
    const sdk = new PostHog()
    ;(sdk as unknown as { _send_request(options: Record<string, unknown>): void })._send_request = () => {}
    const surveys = createIsolatedSurveyClient({
      key: 'test-project-token',
      createPostHog: () => sdk,
    })
    surveys.identify('survey-user')
    localStorage.setItem('seenSurvey_survey-1', 'true')

    await surveys.shutdown({ preserveSurveyState: true })

    expect(localStorage.getItem('seenSurvey_survey-1')).toBe('true')
    expect(sessionStorage.getItem('ph_boostedbudget_surveys')).toBeNull()
    expect(sdk.get_distinct_id()).not.toBe('survey-user')
    clearIsolatedSurveyState()
  })
})

describe('PostHog engagement controller', () => {
  beforeEach(() => document.body.classList.remove('posthog-support-widget-active'))
  afterEach(() => document.body.classList.remove('posthog-support-widget-active'))

  test('sets verified Support identity and resets it without anonymous reload on logout', () => {
    const support = fakeClient()
    const controller = createPostHogEngagementController({
      createSupportClient: () => support,
      createSurveyClient: () => fakeClient(),
    })

    controller.activateSupport({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    expect(support.setIdentity).toHaveBeenCalledWith('user_A', 'signed-A')
    controller.deactivateSupport()
    expect(support.conversations.hide).toHaveBeenCalled()
    expect(support.shutdown).toHaveBeenCalled()
  })

  test('never reuses a support client across an account switch', () => {
    const clients = [fakeClient(), fakeClient()]
    const controller = createPostHogEngagementController({
      createSupportClient: () => clients.shift()!,
      createSurveyClient: () => fakeClient(),
    })

    controller.activateSupport({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    const first = controller.getSupportClient()!
    controller.activateSupport({ distinct_id: 'user_B', identity_hash: 'signed-B' })
    expect(first.shutdown).toHaveBeenCalled()
    expect(controller.getSupportClient()!.setIdentity).toHaveBeenCalledWith('user_B', 'signed-B')
  })

  test('surveys require verified identity and only display allowlisted question types', () => {
    const surveyClient = fakeClient()
    surveyClient.surveys.getActiveMatchingSurveys.mockImplementation(callback => callback([
      surveyWith(['open']),
      surveyWith(['rating', 'multiple_choice']),
    ]))
    const controller = createPostHogEngagementController({
      createSupportClient: () => fakeClient(),
      createSurveyClient: () => surveyClient,
    })

    controller.activateSurveys({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    expect(surveyClient.identify).toHaveBeenCalledWith('user_A')
    expect(surveyClient.surveys.displaySurvey).toHaveBeenCalledTimes(1)
    expect(surveyClient.surveys.displaySurvey).toHaveBeenCalledWith('survey-1', expect.objectContaining({
      ignoreConditions: false,
      ignoreDelay: false,
    }))
  })

  test('survey withdrawal disposes rendered UI and creates a fresh client on regrant', () => {
    const first = fakeClient()
    const second = fakeClient()
    const clients = [first, second]
    const controller = createPostHogEngagementController({
      createSupportClient: () => fakeClient(),
      createSurveyClient: () => clients.shift()!,
    })

    controller.activateSurveys({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    controller.deactivateSurveys()
    expect(first.surveys.dispose).toHaveBeenCalled()
    expect(first.shutdown).toHaveBeenCalled()
    controller.activateSurveys({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    expect(second.identify).toHaveBeenCalledWith('user_A')
  })

  test('route suspension shuts down the old client and resumes with preserved display state', () => {
    const first = fakeClient()
    const second = fakeClient()
    first.surveys.getActiveMatchingSurveys.mockImplementation(callback => callback([
      surveyWith(['rating']),
    ]))
    second.surveys.getActiveMatchingSurveys.mockImplementation(callback => callback([]))
    const clients = [first, second]
    const controller = createPostHogEngagementController({
      createSupportClient: () => fakeClient(),
      createSurveyClient: () => clients.shift()!,
    })
    const identity = { distinct_id: 'user_A', identity_hash: 'signed-A' }

    controller.activateSurveys(identity)
    controller.suspendSurveys()
    controller.activateSurveys(identity)

    expect(first.surveys.dispose).toHaveBeenCalledTimes(1)
    expect(first.shutdown).toHaveBeenCalledWith({ preserveSurveyState: true })
    expect(first.surveys.displaySurvey).toHaveBeenCalledTimes(1)
    expect(second.identify).toHaveBeenCalledWith('user_A')
    expect(second.surveys.displaySurvey).not.toHaveBeenCalled()
  })

  test('shows exactly one support control by replacing the app button with the SDK widget', () => {
    const support = fakeClient()
    const controller = createPostHogEngagementController({
      createSupportClient: () => support,
      createSurveyClient: () => fakeClient(),
    })
    controller.activateSupport({ distinct_id: 'user_A', identity_hash: 'signed-A' })

    controller.showSupport()
    expect(document.body.classList.contains('posthog-support-widget-active')).toBe(true)
    expect(support.conversations.show).toHaveBeenCalledTimes(1)
  })

  test('fails closed when an isolated SDK cannot initialize', () => {
    const controller = createPostHogEngagementController({
      createSupportClient: () => { throw new Error('blocked') },
      createSurveyClient: () => { throw new Error('blocked') },
    })
    expect(() => controller.activateSupport({ distinct_id: 'user_A', identity_hash: 'signed-A' })).not.toThrow()
    expect(() => controller.activateSurveys({ distinct_id: 'user_A', identity_hash: 'signed-A' })).not.toThrow()
    expect(controller.getSupportClient()).toBeNull()
  })

  test('activation failure resets directly without clearing into anonymous mode', () => {
    const support = fakeClient()
    support.setIdentity.mockImplementation(() => { throw new Error('verification rejected') })
    const controller = createPostHogEngagementController({
      createSupportClient: () => support,
      createSurveyClient: () => fakeClient(),
    })

    controller.activateSupport({ distinct_id: 'user_A', identity_hash: 'signed-A' })

    expect(support.shutdown).toHaveBeenCalled()
  })

  test('logout purges Support session state without crossing the anonymous request boundary', () => {
    const anonymousRequest = mock(() => {})
    const support = {
      ...fakeClient(),
      clearIdentity: () => anonymousRequest(),
    }
    support.shutdown.mockImplementation(async () => sessionStorage.removeItem('ph_support_widget_session'))
    sessionStorage.setItem('ph_support_widget_session', 'authenticated-session')
    const controller = createPostHogEngagementController({
      createSupportClient: () => support,
      createSurveyClient: () => fakeClient(),
    })

    controller.activateSupport({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    controller.deactivateSupport()

    expect(anonymousRequest).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('ph_support_widget_session')).toBeNull()
  })
})
