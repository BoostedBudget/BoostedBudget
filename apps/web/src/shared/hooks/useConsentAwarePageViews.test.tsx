import { describe, expect, it, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'
import type { AnalyticsLifecycleState } from '../lib/posthog'
import { useConsentAwarePageViews } from './useConsentAwarePageViews'

describe('useConsentAwarePageViews', () => {
  it('captures a pending route once when consent becomes granted', () => {
    let state: AnalyticsLifecycleState = 'pending'
    let listener: ((next: AnalyticsLifecycleState) => void) | undefined
    const capture = mock((_url: string) => {})
    const dependencies = {
      capture,
      getState: () => state,
      subscribe: (next: (value: AnalyticsLifecycleState) => void) => {
        listener = next
        return () => { listener = undefined }
      },
    }
    const { rerender } = renderHook(
      ({ url }) => useConsentAwarePageViews(url, dependencies),
      { initialProps: { url: '/dashboard?sentinel=query#sentinel-hash' } },
    )
    expect(capture).not.toHaveBeenCalled()

    state = 'granted'
    listener?.('granted')
    expect(capture).toHaveBeenCalledWith('/dashboard?sentinel=query#sentinel-hash')

    rerender({ url: '/dashboard?sentinel=query#sentinel-hash' })
    expect(capture).toHaveBeenCalledTimes(1)
  })

  it('captures each granted navigation once and cleans up its subscriber', () => {
    let listener: ((next: AnalyticsLifecycleState) => void) | undefined
    const capture = mock((_url: string) => {})
    const cleanup = mock(() => {})
    const dependencies = {
      capture,
      getState: () => 'granted' as const,
      subscribe: (next: (value: AnalyticsLifecycleState) => void) => {
        listener = next
        return cleanup
      },
    }
    const { rerender, unmount } = renderHook(
      ({ url }) => useConsentAwarePageViews(url, dependencies),
      { initialProps: { url: '/dashboard' } },
    )
    expect(capture).toHaveBeenCalledWith('/dashboard')

    rerender({ url: '/accounts/account-sentinel?secret=query' })
    rerender({ url: '/accounts/account-sentinel?secret=query' })
    expect(capture.mock.calls.map(call => call[0])).toEqual([
      '/dashboard',
      '/accounts/account-sentinel?secret=query',
    ])

    expect(listener).toBeFunction()
    unmount()
    expect(cleanup).toHaveBeenCalledTimes(1)
  })
})
