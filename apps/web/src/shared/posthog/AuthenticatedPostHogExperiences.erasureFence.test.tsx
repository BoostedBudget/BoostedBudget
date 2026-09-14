import { expect, mock, test } from 'bun:test'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AuthenticatedPostHogExperiences, type EngagementDependencies } from './AuthenticatedPostHogExperiences'
import type { PostHogEngagementController } from './engagement'

window.ENV = { ...window.ENV, POSTHOG_KEY: 'phc_test' }

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
