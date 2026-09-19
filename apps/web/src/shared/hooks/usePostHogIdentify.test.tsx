import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { act, renderHook } from '@testing-library/react'
import { createMockClerkUser, mockClerkUser, mockUseUser } from '@/test/mocks/clerk'
import { mockUseGetPrivacySettings } from '@/test/mocks/privacy-api'
import {
  mockApplyProductAnalyticsAuthority,
  mockListenForAnalyticsWithdrawal,
  mockSetAnalyticsLifecyclePending,
  mockSetProductAnalyticsScope,
} from '@/test/mocks/posthog'
import { usePostHogIdentify, type AnalyticsLifecycle } from './usePostHogIdentify'
import { privacySettingsQueryKey } from '../privacy/privacyQueryKey'
import type { AnalyticsLifecycleSnapshot } from '../lib/analyticsLifecycle'

function createLifecycle() {
  let listener: ((next: AnalyticsLifecycleSnapshot) => void) | undefined
  let current: AnalyticsLifecycleSnapshot = {
    generation: 1,
    state: 'denied',
    scope: 'user-a',
    withdrawn: false,
  }
  const setPersonProperties = mock((_properties: Record<string, unknown>) => {})

  return {
    apply: mockApplyProductAnalyticsAuthority,
    getSnapshot: () => ({ ...current }),
    listenForWithdrawal: mockListenForAnalyticsWithdrawal,
    setPending: mockSetAnalyticsLifecyclePending,
    setPersonProperties,
    setScope: mockSetProductAnalyticsScope,
    subscribe: (next: (value: AnalyticsLifecycleSnapshot) => void) => {
      listener = next
      return () => { listener = undefined }
    },
    publish(next: Partial<AnalyticsLifecycleSnapshot>) {
      current = { ...current, ...next }
      listener?.(current)
    },
  } satisfies AnalyticsLifecycle & {
    publish: (next: Partial<AnalyticsLifecycleSnapshot>) => void
  }
}

type PrivacyResult = {
  data?: Record<string, boolean | undefined>
  isLoading: boolean
  isFetching: boolean
  error: Error | null
  refetch: () => Promise<unknown>
}

function setSignedIn(userId = 'user-a', overrides: Record<string, unknown> = {}): void {
  mockUseUser.mockReturnValue(createMockClerkUser({
    isLoaded: true,
    isSignedIn: true,
    user: { ...mockClerkUser, id: userId, ...overrides },
  }))
}

function setPrivacy(result: Partial<PrivacyResult>): void {
  mockUseGetPrivacySettings.mockReturnValue({
    data: undefined,
    isLoading: false,
    isFetching: false,
    error: null,
    refetch: () => Promise.resolve(),
    ...result,
  })
}

describe('usePostHogIdentify consent authority', () => {
  let lifecycle: ReturnType<typeof createLifecycle>

  beforeEach(() => {
    mockUseUser.mockClear()
    mockUseGetPrivacySettings.mockClear()
    mockApplyProductAnalyticsAuthority.mockClear()
    mockSetAnalyticsLifecyclePending.mockClear()
    mockSetProductAnalyticsScope.mockClear()
    mockListenForAnalyticsWithdrawal.mockClear()
    mockUseUser.mockReturnValue(createMockClerkUser({ isLoaded: true, user: null }))
    setPrivacy({})
    lifecycle = createLifecycle()
  })

  for (const cookie of [true, false, null]) {
    for (const server of ['true', 'false', 'missing', 'error'] as const) {
      it(`ignores stale anonymous cookie ${String(cookie)} when signed-in server state is ${server}`, () => {
        setSignedIn()
        setPrivacy({
          data: server === 'true'
            ? { gdpr_analytics_consent: true }
            : server === 'false'
              ? { gdpr_analytics_consent: false }
              : server === 'missing'
                ? {}
                : undefined,
          error: server === 'error' ? new Error('privacy unavailable') : null,
        })

        renderHook(() => usePostHogIdentify(cookie, lifecycle))

        const calls = mockApplyProductAnalyticsAuthority.mock.calls
        expect(calls).toHaveLength(1)
        expect(calls[0]?.[0]?.scope).toBe('user-a')
        expect(calls[0]?.[0]?.purposes.analytics).toBe(server === 'true')
      })
    }
  }

  it('stays pending without applying authority while signed-in privacy is loading', () => {
    setSignedIn()
    setPrivacy({ isLoading: true, isFetching: true })

    renderHook(() => usePostHogIdentify(true, lifecycle))

    expect(mockSetAnalyticsLifecyclePending).toHaveBeenCalledTimes(1)
    expect(mockApplyProductAnalyticsAuthority).not.toHaveBeenCalled()
  })

  it.each([
    [true, true],
    [false, false],
    [null, false],
  ])('uses signed-out anonymous cookie %s as analytics authority', (cookie, analytics) => {
    renderHook(() => usePostHogIdentify(cookie, lifecycle))

    const calls = mockApplyProductAnalyticsAuthority.mock.calls
    expect(calls).toHaveLength(1)
    expect(calls[0]?.[0]?.scope).toBe('anonymous')
    expect(calls[0]?.[0]?.purposes).toEqual({
      analytics,
      identifiedDiagnostics: false,
      interactionCapture: false,
    })
  })

  it('does not identify again on same-user rerenders', () => {
    setSignedIn()
    setPrivacy({ data: { gdpr_analytics_consent: true } })
    const { rerender } = renderHook(() => usePostHogIdentify(false, lifecycle))

    rerender()

    expect(mockApplyProductAnalyticsAuthority).toHaveBeenCalledTimes(1)
  })

  it('tears down A before granting B on a direct user switch', () => {
    const calls: string[] = []
    mockSetProductAnalyticsScope.mockImplementation(scope => { calls.push(`scope:${scope}`) })
    mockApplyProductAnalyticsAuthority.mockImplementation((authority) => {
      calls.push(`grant:${authority.distinctId ?? 'anonymous'}`)
      return Promise.resolve()
    })
    setSignedIn('user-a')
    setPrivacy({ data: { gdpr_analytics_consent: true } })
    const { rerender } = renderHook(() => usePostHogIdentify(false, lifecycle))

    setSignedIn('user-b')
    rerender()

    expect(calls).toEqual(['scope:user-a', 'grant:user-a', 'scope:user-b', 'grant:user-b'])
    expect(mockUseGetPrivacySettings.mock.calls[0]?.[0]?.query?.queryKey).toEqual(
      privacySettingsQueryKey('user-a'),
    )
    expect(mockUseGetPrivacySettings.mock.calls.at(-1)?.[0]?.query?.queryKey).toEqual(
      privacySettingsQueryKey('user-b'),
    )
  })

  it('tears down the user before reconsidering an anonymous grant on logout', () => {
    const calls: string[] = []
    mockSetProductAnalyticsScope.mockImplementation(scope => { calls.push(`scope:${scope}`) })
    mockApplyProductAnalyticsAuthority.mockImplementation((authority) => {
      calls.push(`grant:${authority.distinctId ?? 'anonymous'}`)
      return Promise.resolve()
    })
    setSignedIn('user-a')
    setPrivacy({ data: { gdpr_analytics_consent: true } })
    const { rerender } = renderHook(() => usePostHogIdentify(true, lifecycle))

    mockUseUser.mockReturnValue(createMockClerkUser({ isLoaded: true, user: null }))
    rerender()

    expect(calls).toEqual(['scope:user-a', 'grant:user-a', 'scope:anonymous', 'grant:anonymous'])
  })

  it('installs and cleans up the cross-tab withdrawal listener', () => {
    const cleanup = mock(() => {})
    mockListenForAnalyticsWithdrawal.mockReturnValue(cleanup)
    const { unmount } = renderHook(() => usePostHogIdentify(null, lifecycle))

    expect(mockListenForAnalyticsWithdrawal).toHaveBeenCalledTimes(1)
    unmount()
    expect(cleanup).toHaveBeenCalledTimes(1)
  })

  it('ships only the Clerk display name and never the email', () => {
    setSignedIn('user-a', {
      fullName: 'NAME_SENTINEL',
      firstName: 'FIRST_SENTINEL',
      primaryEmailAddress: {
        emailAddress: 'EMAIL_SENTINEL@example.com',
        verification: { status: 'verified' },
      },
    })
    setPrivacy({ data: { gdpr_analytics_consent: true } })

    renderHook(() => usePostHogIdentify(false, lifecycle))

    const serialized = JSON.stringify(mockApplyProductAnalyticsAuthority.mock.calls)
    expect(serialized).toContain('NAME_SENTINEL')
    expect(serialized).not.toContain('EMAIL_SENTINEL')
    const personProperties = mockApplyProductAnalyticsAuthority.mock.calls[0]?.[0]?.personProperties
    expect(new Set(Object.keys(personProperties ?? {}))).toEqual(new Set(['name']))
    expect(personProperties?.name).toBe('NAME_SENTINEL')
  })

  it('falls back to the Clerk first name when no full name exists', () => {
    setSignedIn('user-a', { fullName: null, firstName: 'FIRST_SENTINEL' })
    setPrivacy({ data: { gdpr_analytics_consent: true } })

    renderHook(() => usePostHogIdentify(false, lifecycle))

    expect(mockApplyProductAnalyticsAuthority.mock.calls[0]?.[0]?.personProperties).toEqual({
      name: 'FIRST_SENTINEL',
    })
  })

  it('never calls setPersonProperties with a non-name key', () => {
    setSignedIn('user-a', { fullName: 'NAME_SENTINEL' })
    setPrivacy({ data: { gdpr_analytics_consent: true } })
    renderHook(() => usePostHogIdentify(false, lifecycle))

    for (const call of lifecycle.setPersonProperties.mock.calls) {
      expect(Object.keys(call[0] as Record<string, unknown>)).toEqual(['name'])
    }
  })

  it('re-invokes apply after a clearWithdrawal render', () => {
    setSignedIn()
    setPrivacy({ data: { gdpr_analytics_consent: true } })
    renderHook(() => usePostHogIdentify(false, lifecycle))
    expect(mockApplyProductAnalyticsAuthority).toHaveBeenCalledTimes(1)

    act(() => { lifecycle.publish({ withdrawn: true }) })
    expect(mockApplyProductAnalyticsAuthority).toHaveBeenCalledTimes(2)

    act(() => { lifecycle.publish({ withdrawn: false }) })
    expect(mockApplyProductAnalyticsAuthority).toHaveBeenCalledTimes(3)
  })
})
