import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import type { Survey } from 'posthog-js'
import {
  createPostHogEngagementController,
  isAuthenticatedAppPath,
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
    clearIdentity: mock(() => {}),
    identify: mock(() => {}),
    reset: mock(() => {}),
    conversations: {
      hide: mock(() => {}),
      show: mock(() => {}),
      isAvailable: mock(() => true),
    },
    surveys: {
      dispose: mock(() => {}),
      getActiveMatchingSurveys: mock((callback: (surveys: Survey[]) => void) => callback([])),
      displaySurvey: mock(() => {}),
    },
  }
  return client as unknown as EngagementPostHogClient & typeof client
}

describe('PostHog authenticated experiences policy', () => {
  test('only permits authenticated application-shell routes', () => {
    for (const path of ['/dashboard', '/accounts/1', '/transactions', '/profile', '/admin/health']) {
      expect(isAuthenticatedAppPath(path)).toBe(true)
    }
    for (const path of ['/', '/pricing', '/mcp', '/legal/privacy-policy', '/cancel-deletion/token', '/households/invites/token']) {
      expect(isAuthenticatedAppPath(path)).toBe(false)
    }
  })

  test('allows rating and closed choice questions but rejects every free-text path', () => {
    expect(isSupportedSurvey(surveyWith(['rating', 'single_choice', 'multiple_choice']))).toBe(true)
    expect(isSupportedSurvey(surveyWith(['open']))).toBe(false)
    expect(isSupportedSurvey(surveyWith(['link']))).toBe(false)
    expect(isSupportedSurvey(surveyWith(['single_choice'], true))).toBe(false)
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
})

describe('PostHog engagement controller', () => {
  beforeEach(() => document.body.classList.remove('posthog-support-widget-active'))
  afterEach(() => document.body.classList.remove('posthog-support-widget-active'))

  test('sets verified Support identity and clears it on logout', () => {
    const support = fakeClient()
    const controller = createPostHogEngagementController({
      createSupportClient: () => support,
      createSurveyClient: () => fakeClient(),
    })

    controller.activateSupport({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    expect(support.setIdentity).toHaveBeenCalledWith('user_A', 'signed-A')
    controller.deactivateSupport()
    expect(support.conversations.hide).toHaveBeenCalled()
    expect(support.clearIdentity).toHaveBeenCalled()
    expect(support.reset).toHaveBeenCalledWith(true)
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
    expect(first.clearIdentity).toHaveBeenCalled()
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
    expect(first.reset).toHaveBeenCalledWith(true)
    controller.activateSurveys({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    expect(second.identify).toHaveBeenCalledWith('user_A')
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
})
