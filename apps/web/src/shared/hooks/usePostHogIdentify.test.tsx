import { beforeEach, describe, expect, it } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { createMockClerkUser, mockClerkUser, mockUseUser } from '@/test/mocks/clerk'
import { mockUseGetPrivacySettings } from '@/test/mocks/privacy-api'
import {
  mockDenyProductAnalytics,
  mockGrantProductAnalytics,
  mockListenForAnalyticsWithdrawal,
  mockSetAnalyticsLifecyclePending,
} from '@/test/mocks/posthog'
import { usePostHogIdentify } from './usePostHogIdentify'

const lifecycle = {
  deny: mockDenyProductAnalytics,
  grant: mockGrantProductAnalytics,
  listenForWithdrawal: mockListenForAnalyticsWithdrawal,
  setPending: mockSetAnalyticsLifecyclePending,
  setPersonProperties: () => {},
}

type PrivacyResult = {
  data?: { gdpr_analytics_consent?: boolean }
  isLoading: boolean
  isFetching: boolean
  error: Error | null
  refetch: () => Promise<void>
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
    mockGrantProductAnalytics.mockClear()
    mockDenyProductAnalytics.mockClear()
    mockSetAnalyticsLifecyclePending.mockClear()
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
          expect(mockGrantProductAnalytics).toHaveBeenCalledTimes(1)
          expect(mockGrantProductAnalytics.mock.calls[0]?.[0]).toBe('user-a')
          expect(mockDenyProductAnalytics).not.toHaveBeenCalled()
        } else {
          expect(mockGrantProductAnalytics).not.toHaveBeenCalled()
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
    expect(mockGrantProductAnalytics).not.toHaveBeenCalled()
    expect(mockDenyProductAnalytics).not.toHaveBeenCalled()
  })

  it.each([
    [true, 1, 0],
    [false, 0, 1],
    [null, 0, 1],
  ])('uses signed-out anonymous cookie %s as authority', (cookie, grants, denials) => {
    renderHook(() => usePostHogIdentify(cookie, lifecycle))
    expect(mockGrantProductAnalytics).toHaveBeenCalledTimes(grants)
    expect(mockDenyProductAnalytics).toHaveBeenCalledTimes(denials)
  })

  it('does not identify again on same-user rerenders', () => {
    setSignedIn()
    setPrivacy({ data: { gdpr_analytics_consent: true } })
    const { rerender } = renderHook(() => usePostHogIdentify(false, lifecycle))

    rerender()

    expect(mockGrantProductAnalytics).toHaveBeenCalledTimes(1)
  })

  it('tears down A before granting B on a direct user switch', () => {
    const calls: string[] = []
    mockDenyProductAnalytics.mockImplementation(() => { calls.push('deny') })
    mockGrantProductAnalytics.mockImplementation(id => { calls.push(`grant:${id}`) })
    setSignedIn('user-a')
    setPrivacy({ data: { gdpr_analytics_consent: true } })
    const { rerender } = renderHook(() => usePostHogIdentify(false, lifecycle))

    setSignedIn('user-b')
    rerender()

    expect(calls).toEqual(['grant:user-a', 'deny', 'grant:user-b'])
  })

  it('tears down the user before reconsidering an anonymous grant on logout', () => {
    const calls: string[] = []
    mockDenyProductAnalytics.mockImplementation(() => { calls.push('deny') })
    mockGrantProductAnalytics.mockImplementation(id => { calls.push(`grant:${id ?? 'anonymous'}`) })
    setSignedIn('user-a')
    setPrivacy({ data: { gdpr_analytics_consent: true } })
    const { rerender } = renderHook(() => usePostHogIdentify(true, lifecycle))

    mockUseUser.mockReturnValue(createMockClerkUser({ isLoaded: true, user: null }))
    rerender()

    expect(calls).toEqual(['grant:user-a', 'deny', 'grant:anonymous'])
  })

  it('installs and cleans up the cross-tab withdrawal listener', () => {
    const cleanup = () => {}
    mockListenForAnalyticsWithdrawal.mockReturnValue(cleanup)
    const { unmount } = renderHook(() => usePostHogIdentify(null, lifecycle))

    expect(mockListenForAnalyticsWithdrawal).toHaveBeenCalledTimes(1)
    unmount()
    expect(mockListenForAnalyticsWithdrawal.mock.results[0]?.value).toBe(cleanup)
  })
})
