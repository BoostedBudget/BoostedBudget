import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { createMockClerkUser, mockClerkUser, mockUseUser } from '@/test/mocks/clerk'
import { mockUseGetPrivacySettings } from '@/test/mocks/privacy-api'
import {
  mockApplyProductAnalyticsAuthority,
  mockDenyProductAnalytics,
  mockListenForAnalyticsWithdrawal,
  mockSetAnalyticsLifecyclePending,
  mockSetProductAnalyticsScope,
} from '@/test/mocks/posthog'
import { usePostHogIdentify } from './usePostHogIdentify'
import { privacySettingsQueryKey } from '../privacy/privacyQueryKey'

const lifecycle = {
  applyAuthority: mockApplyProductAnalyticsAuthority,
  deny: mockDenyProductAnalytics,
  listenForWithdrawal: mockListenForAnalyticsWithdrawal,
  setPending: mockSetAnalyticsLifecyclePending,
  setScope: mockSetProductAnalyticsScope,
  setPersonProperties: () => {},
}

type PrivacyResult = {
  data?: Record<string, boolean | undefined>
  isLoading: boolean
  isFetching: boolean
  error: Error | null
  refetch: () => Promise<unknown>
}

function setSignedIn(userId = 'user-a'): void {
  mockUseUser.mockReturnValue(createMockClerkUser({
    isLoaded: true,
    isSignedIn: true,
    user: { ...mockClerkUser, id: userId },
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
  beforeEach(() => {
    mockUseUser.mockClear()
    mockUseGetPrivacySettings.mockClear()
    mockApplyProductAnalyticsAuthority.mockClear()
    mockDenyProductAnalytics.mockClear()
    mockSetAnalyticsLifecyclePending.mockClear()
    mockSetProductAnalyticsScope.mockClear()
    mockListenForAnalyticsWithdrawal.mockClear()
    mockUseUser.mockReturnValue(createMockClerkUser({ isLoaded: true, user: null }))
    setPrivacy({})
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

        if (server === 'true') {
          expect(mockApplyProductAnalyticsAuthority).toHaveBeenCalledTimes(1)
          expect(mockApplyProductAnalyticsAuthority.mock.calls[0]?.slice(0, 2)).toEqual(['user-a', 'user-a'])
          expect(mockDenyProductAnalytics).not.toHaveBeenCalled()
        } else {
          expect(mockApplyProductAnalyticsAuthority).not.toHaveBeenCalled()
          expect(mockDenyProductAnalytics).toHaveBeenCalledTimes(1)
        }
      })
    }
  }

  it('stays pending without init, capture, or identify while signed-in privacy is loading', () => {
    setSignedIn()
    setPrivacy({ isLoading: true, isFetching: true })

    renderHook(() => usePostHogIdentify(true, lifecycle))

    expect(mockSetAnalyticsLifecyclePending).toHaveBeenCalledTimes(1)
    expect(mockApplyProductAnalyticsAuthority).not.toHaveBeenCalled()
    expect(mockDenyProductAnalytics).not.toHaveBeenCalled()
  })

  it.each([
    [true, 1, 0],
    [false, 0, 1],
    [null, 0, 1],
  ])('uses signed-out anonymous cookie %s as authority', (cookie, grants, denials) => {
    renderHook(() => usePostHogIdentify(cookie, lifecycle))
    expect(mockApplyProductAnalyticsAuthority).toHaveBeenCalledTimes(grants)
    expect(mockDenyProductAnalytics).toHaveBeenCalledTimes(denials)
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
    mockDenyProductAnalytics.mockImplementation(() => { calls.push('deny') })
    mockSetProductAnalyticsScope.mockImplementation(scope => { calls.push(`scope:${scope}`) })
    mockApplyProductAnalyticsAuthority.mockImplementation((_scope, id) => {
      calls.push(`grant:${id}`)
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
    mockDenyProductAnalytics.mockImplementation(() => { calls.push('deny') })
    mockSetProductAnalyticsScope.mockImplementation(scope => { calls.push(`scope:${scope}`) })
    mockApplyProductAnalyticsAuthority.mockImplementation((_scope, id) => {
      calls.push(`grant:${id ?? 'anonymous'}`)
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
})
