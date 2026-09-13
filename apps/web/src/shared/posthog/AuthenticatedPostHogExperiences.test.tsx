import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AnalyticsLifecycleSnapshot } from '../lib/posthog'
import { AuthenticatedPostHogExperiences, type EngagementDependencies } from './AuthenticatedPostHogExperiences'
import type { PostHogEngagementController } from './engagement'

function controller(available = true) {
  return {
    activateSupport: mock(() => {}),
    deactivateSupport: mock(() => {}),
    activateSurveys: mock(() => {}),
    deactivateSurveys: mock(() => {}),
    suspendSurveys: mock(() => {}),
    getSupportClient: mock(() => null),
    isSupportAvailable: mock(() => available),
    showSupport: mock(() => {}),
  } satisfies PostHogEngagementController
}

function deps({
  loaded = true,
  userId = 'user_A',
  identity = { distinct_id: 'user_A', identity_hash: 'signed-A' },
  identityLoading = false,
  identityError = false,
  analytics = { generation: 1, state: 'granted', scope: 'user_A' } as AnalyticsLifecycleSnapshot,
}: Partial<{
  loaded: boolean
  userId: string | null
  identity: { distinct_id: string; identity_hash: string } | null
  identityLoading: boolean
  identityError: boolean
  analytics: AnalyticsLifecycleSnapshot
}> = {}): EngagementDependencies {
  return {
    useAuthState: () => ({ isLoaded: loaded, userId }),
    useVerifiedIdentity: (_userId, enabled) => ({
      data: enabled ? identity ?? undefined : undefined,
      isLoading: enabled && identityLoading,
      isError: enabled && identityError,
    }),
    useAnalyticsSnapshot: () => analytics,
  }
}

describe('AuthenticatedPostHogExperiences', () => {
  beforeEach(() => {
    window.ENV = { ...window.ENV, POSTHOG_KEY: 'phc_test' }
  })
  afterEach(() => document.body.classList.remove('posthog-support-widget-active'))

  test('does not fetch, activate, or render on the logged-out homepage', () => {
    const c = controller()
    const useVerifiedIdentity = mock(() => ({ data: undefined, isLoading: false, isError: false }))
    const dependencies = { ...deps({ userId: null }), useVerifiedIdentity }
    render(<AuthenticatedPostHogExperiences controller={c} dependencies={dependencies} />)

    expect(useVerifiedIdentity).toHaveBeenCalledWith(null, false)
    expect(c.activateSupport).not.toHaveBeenCalled()
    expect(c.activateSurveys).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: /support/i })).toBeNull()
  })

  test('activates consented surveys immediately but defers Support until the user clicks', async () => {
    const c = controller()
    render(<AuthenticatedPostHogExperiences controller={c} dependencies={deps()} />)
    expect(c.activateSurveys).toHaveBeenCalledWith({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    expect(c.activateSupport).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Support' }))
    expect(c.activateSupport).toHaveBeenCalledWith({ distinct_id: 'user_A', identity_hash: 'signed-A' })
    expect(c.showSupport).toHaveBeenCalled()
  })

  test('support remains click-available without analytics consent while surveys stay disabled', async () => {
    const c = controller()
    render(<AuthenticatedPostHogExperiences controller={c} dependencies={deps({
      analytics: { generation: 2, state: 'denied', scope: 'user_A' },
    })} />)
    expect(c.activateSupport).not.toHaveBeenCalled()
    expect(c.activateSurveys).not.toHaveBeenCalled()
    expect(c.deactivateSurveys).toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Support' }))
    expect(c.activateSupport).toHaveBeenCalled()
  })

  test('rejects a stale identity response after an A to B switch', () => {
    const c = controller()
    render(<AuthenticatedPostHogExperiences controller={c} dependencies={deps({
      userId: 'user_B',
      identity: { distinct_id: 'user_A', identity_hash: 'signed-A' },
      analytics: { generation: 2, state: 'granted', scope: 'user_B' },
    })} />)
    expect(c.activateSupport).not.toHaveBeenCalled()
    expect(c.activateSurveys).not.toHaveBeenCalled()
    expect(c.deactivateSupport).toHaveBeenCalled()
  })

  test('shows loading and unavailable states only after Support is requested', async () => {
    const c = controller()
    const { rerender } = render(<AuthenticatedPostHogExperiences controller={c} dependencies={deps({
      identity: null,
      identityLoading: true,
    })} />)
    expect(screen.getByRole('button', { name: 'Support' })).toBeEnabled()
    await userEvent.click(screen.getByRole('button', { name: 'Support' }))
    expect(screen.getByRole('button', { name: 'Support loading' })).toBeDisabled()

    rerender(<AuthenticatedPostHogExperiences controller={c} dependencies={deps({
      identity: null,
      identityError: true,
    })} />)
    expect(screen.getByRole('button', { name: 'Support unavailable' })).toBeDisabled()
    expect(c.activateSupport).not.toHaveBeenCalled()
  })

  test('cleans both products on logout', () => {
    const c = controller()
    const { rerender } = render(<AuthenticatedPostHogExperiences controller={c} dependencies={deps()} />)
    rerender(<AuthenticatedPostHogExperiences controller={c} dependencies={deps({ userId: null })} />)
    expect(c.deactivateSupport).toHaveBeenCalled()
    expect(c.deactivateSurveys).toHaveBeenCalled()
  })

  test('keeps one owner across product routes and suspends on admin before returning', () => {
    const c = controller()
    const dependencies = deps()
    const { rerender } = render(
      <AuthenticatedPostHogExperiences controller={c} dependencies={dependencies} routeEligible />,
    )
    expect(c.activateSurveys).toHaveBeenCalledTimes(1)

    rerender(<AuthenticatedPostHogExperiences controller={c} dependencies={dependencies} routeEligible />)
    expect(c.activateSurveys).toHaveBeenCalledTimes(1)

    rerender(<AuthenticatedPostHogExperiences controller={c} dependencies={dependencies} routeEligible={false} />)
    expect(c.suspendSurveys).toHaveBeenCalled()

    rerender(<AuthenticatedPostHogExperiences controller={c} dependencies={dependencies} routeEligible />)
    expect(c.activateSurveys).toHaveBeenCalledTimes(2)
  })

  test('hands off from the single app button to the vendor widget', async () => {
    const c = controller()
    render(<AuthenticatedPostHogExperiences controller={c} dependencies={deps()} />)
    await userEvent.click(screen.getByRole('button', { name: 'Support' }))
    expect(c.showSupport).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: 'Support' })).toBeNull()
  })

  test('cancels availability timers on cleanup', async () => {
    const clearIntervalSpy = mock(() => {})
    const originalClearInterval = window.clearInterval
    window.clearInterval = clearIntervalSpy as typeof window.clearInterval
    const c = controller(false)
    const { unmount } = render(<AuthenticatedPostHogExperiences controller={c} dependencies={deps()} />)
    await userEvent.click(screen.getByRole('button', { name: 'Support' }))
    act(() => unmount())
    expect(clearIntervalSpy).toHaveBeenCalled()
    window.clearInterval = originalClearInterval
  })

  test('pauses the handed-off widget in the background and resumes it when visible', async () => {
    const hide = mock(() => {})
    const c = controller()
    c.getSupportClient.mockReturnValue({ conversations: { hide } } as never)
    render(<AuthenticatedPostHogExperiences controller={c} dependencies={deps()} />)
    await userEvent.click(screen.getByRole('button', { name: 'Support' }))

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(hide).toHaveBeenCalledTimes(1)

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(c.showSupport).toHaveBeenCalledTimes(2)
  })
})
