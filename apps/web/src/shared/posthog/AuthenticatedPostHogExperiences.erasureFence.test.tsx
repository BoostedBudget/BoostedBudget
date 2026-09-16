import { expect, mock, test } from 'bun:test'
import { act, render, renderHook, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { mockUseGetPostHogSupportIdentity } from '@/test/mocks/support-api'
import {
  AuthenticatedPostHogExperiences,
  defaultDependencies,
  type EngagementDependencies,
} from './AuthenticatedPostHogExperiences'
import {
  createPostHogEngagementController,
  type EngagementPostHogClient,
  type PostHogEngagementController,
} from './engagement'

window.ENV = { ...window.ENV, POSTHOG_KEY: 'phc_test' }

// Metis M5: the 15s fence-detection cadence lives in the real defaultDependencies, which every
// other test replaces with an injected useVerifiedIdentity. Exercise the production dependency
// itself and assert the query options it hands to the generated Support identity hook.
test('the real default identity dependency polls every 15s in the background while Support is open', () => {
  mockUseGetPostHogSupportIdentity.mockClear()
  renderHook(() => defaultDependencies.useVerifiedIdentity('clerk_subject', true, true))

  expect(mockUseGetPostHogSupportIdentity).toHaveBeenLastCalledWith({
    query: expect.objectContaining({
      refetchInterval: 15_000,
      refetchIntervalInBackground: true,
    }),
  })
})

test('the real default identity dependency stops polling when Support is not open', () => {
  mockUseGetPostHogSupportIdentity.mockClear()
  renderHook(() => defaultDependencies.useVerifiedIdentity('clerk_subject', true, false))

  expect(mockUseGetPostHogSupportIdentity).toHaveBeenLastCalledWith({
    query: expect.objectContaining({
      refetchInterval: false,
      refetchIntervalInBackground: true,
    }),
  })
})

test('an open Support client is deactivated when authority revalidation is fenced', async () => {
  let identityError = false
  const controller = {
    activateSupport: mock(() => {}),
    deactivateSupport: mock(() => {}),
    activateSurveys: mock(() => {}),
    deactivateSurveys: mock(() => {}),
    suspendSurveys: mock(() => {}),
    getSupportClient: mock(() => null),
    isSupportAvailable: mock(() => true),
    showSupport: mock(() => {}),
  } satisfies PostHogEngagementController
  const dependencies: EngagementDependencies = {
    useAuthState: () => ({ isLoaded: true, userId: 'clerk_subject' }),
    useVerifiedIdentity: () => ({
      data: { distinct_id: 'clerk_subject', identity_hash: 'verified' },
      isLoading: false,
      isError: identityError,
    }),
    useAnalyticsSnapshot: () => ({ generation: 1, state: 'denied', scope: 'clerk_subject' }),
  }

  const view = render(
    <AuthenticatedPostHogExperiences controller={controller} dependencies={dependencies} />,
  )
  await userEvent.click(screen.getByRole('button', { name: 'Support' }))
  expect(controller.activateSupport).toHaveBeenCalled()

  identityError = true
  view.rerender(
    <AuthenticatedPostHogExperiences controller={controller} dependencies={dependencies} />,
  )
  expect(controller.deactivateSupport).toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Support unavailable' })).toBeDisabled()
})

test('an open Support client is torn down when the erasure fence activates', async () => {
  let identityError = false
  const hide = mock(() => {})
  const shutdown = mock(() => Promise.resolve())
  const supportClient = {
    setIdentity: mock(() => {}),
    identify: mock(() => {}),
    shutdown,
    conversations: {
      hide,
      show: mock(() => {}),
      isAvailable: mock(() => true),
      reset: mock(() => {}),
    },
    surveys: {
      dispose: mock(() => {}),
      reset: mock(() => {}),
      getActiveMatchingSurveys: mock(() => {}),
      displaySurvey: mock(() => {}),
    },
  } satisfies EngagementPostHogClient
  const controller = createPostHogEngagementController({
    createSupportClient: () => supportClient,
    createSurveyClient: () => { throw new Error('surveys are denied') },
  })
  const dependencies: EngagementDependencies = {
    useAuthState: () => ({ isLoaded: true, userId: 'clerk_subject' }),
    useVerifiedIdentity: () => ({
      data: { distinct_id: 'clerk_subject', identity_hash: 'verified' },
      isLoading: false,
      isError: identityError,
    }),
    useAnalyticsSnapshot: () => ({ generation: 1, state: 'denied', scope: 'clerk_subject' }),
  }

  const view = render(
    <AuthenticatedPostHogExperiences controller={controller} dependencies={dependencies} />,
  )
  await userEvent.click(screen.getByRole('button', { name: 'Support' }))
  expect(controller.getSupportClient()).toBe(supportClient)
  expect(supportClient.conversations.show).toHaveBeenCalled()

  identityError = true
  view.rerender(
    <AuthenticatedPostHogExperiences controller={controller} dependencies={dependencies} />,
  )
  expect(controller.getSupportClient()).toBeNull()
  expect(hide).toHaveBeenCalled()
  expect(shutdown).toHaveBeenCalled()
  expect(document.body).not.toHaveClass('posthog-support-widget-active')
  expect(screen.getByRole('button', { name: 'Support unavailable' })).toBeDisabled()
})

test('a background Support client revalidates before it can resume visibly', async () => {
  let finishRevalidation: (() => void) | undefined
  const refetch = mock(() => new Promise<void>((resolve) => {
    finishRevalidation = resolve
  }))
  const hide = mock(() => {})
  const controller = {
    activateSupport: mock(() => {}),
    deactivateSupport: mock(() => {}),
    activateSurveys: mock(() => {}),
    deactivateSurveys: mock(() => {}),
    suspendSurveys: mock(() => {}),
    getSupportClient: mock(() => ({ conversations: { hide } }) as never),
    isSupportAvailable: mock(() => true),
    showSupport: mock(() => {}),
  } satisfies PostHogEngagementController
  const dependencies: EngagementDependencies = {
    useAuthState: () => ({ isLoaded: true, userId: 'clerk_subject' }),
    useVerifiedIdentity: () => ({
      data: { distinct_id: 'clerk_subject', identity_hash: 'verified' },
      isLoading: false,
      isError: false,
      refetch,
    }),
    useAnalyticsSnapshot: () => ({ generation: 1, state: 'denied', scope: 'clerk_subject' }),
  }
  render(<AuthenticatedPostHogExperiences controller={controller} dependencies={dependencies} />)
  await userEvent.click(screen.getByRole('button', { name: 'Support' }))
  const activationsBeforeResume = controller.activateSupport.mock.calls.length

  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
  act(() => document.dispatchEvent(new Event('visibilitychange')))
  expect(hide).toHaveBeenCalled()

  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  act(() => document.dispatchEvent(new Event('visibilitychange')))
  expect(controller.deactivateSupport).toHaveBeenCalled()
  expect(refetch).toHaveBeenCalled()
  expect(controller.activateSupport).toHaveBeenCalledTimes(activationsBeforeResume)

  await act(async () => {
    finishRevalidation?.()
  })
  await waitFor(() => {
    expect(controller.activateSupport.mock.calls.length).toBeGreaterThan(activationsBeforeResume)
  })
})
