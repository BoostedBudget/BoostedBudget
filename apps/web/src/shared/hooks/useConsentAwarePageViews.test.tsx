import { describe, expect, it, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'
import type { AnalyticsLifecycleSnapshot } from '../lib/posthog'
import { useConsentAwarePageViews } from './useConsentAwarePageViews'

function createDependencies(initialSnapshot: AnalyticsLifecycleSnapshot) {
  let snapshot = initialSnapshot
  let listener: ((next: AnalyticsLifecycleSnapshot) => void) | undefined
  const capture = mock((_url: string) => {})

  return {
    capture,
    dependencies: {
      capture,
      getSnapshot: () => snapshot,
      subscribe: (next: (value: AnalyticsLifecycleSnapshot) => void) => {
        listener = next
        return () => { listener = undefined }
      },
    },
    publish(next: AnalyticsLifecycleSnapshot) {
      snapshot = next
      listener?.(next)
    },
  }
}

describe('useConsentAwarePageViews', () => {
  it('captures a pending route once when consent becomes granted', () => {
    const lifecycle = createDependencies({ state: 'pending', scope: 'user-a', generation: 1 })
    const { rerender } = renderHook(
      ({ url }) => useConsentAwarePageViews(url, lifecycle.dependencies),
      { initialProps: { url: '/dashboard?sentinel=query#sentinel-hash' } },
    )
    expect(lifecycle.capture).not.toHaveBeenCalled()

    lifecycle.publish({ state: 'granted', scope: 'user-a', generation: 1 })
    expect(lifecycle.capture).toHaveBeenCalledWith('/dashboard?sentinel=query#sentinel-hash')

    rerender({ url: '/dashboard?sentinel=query#sentinel-hash' })
    expect(lifecycle.capture).toHaveBeenCalledTimes(1)
  })

  it('does not recapture the unchanged route after a same-scope privacy refetch', () => {
    const lifecycle = createDependencies({ state: 'granted', scope: 'user-a', generation: 1 })
    renderHook(() => useConsentAwarePageViews('/dashboard', lifecycle.dependencies))
    expect(lifecycle.capture).toHaveBeenCalledTimes(1)

    lifecycle.publish({ state: 'pending', scope: 'user-a', generation: 1 })
    lifecycle.publish({ state: 'granted', scope: 'user-a', generation: 1 })

    expect(lifecycle.capture).toHaveBeenCalledTimes(1)
  })

  it('deduplicates query and hash variants by their sanitized route', () => {
    const lifecycle = createDependencies({ state: 'granted', scope: 'user-a', generation: 1 })
    const { rerender } = renderHook(
      ({ url }) => useConsentAwarePageViews(url, lifecycle.dependencies),
      { initialProps: { url: '/accounts/account-secret?query=sentinel' } },
    )

    rerender({ url: '/accounts/account-secret#hash-sentinel' })

    expect(lifecycle.capture).toHaveBeenCalledTimes(1)
  })

  it('captures navigation between entity IDs while transmitting only sanitized templates', () => {
    const lifecycle = createDependencies({ state: 'granted', scope: 'user-a', generation: 1 })
    const { rerender } = renderHook(
      ({ url }) => useConsentAwarePageViews(url, lifecycle.dependencies),
      { initialProps: { url: '/accounts/account-a?query=one' } },
    )

    rerender({ url: '/accounts/account-b#private-fragment' })

    expect(lifecycle.capture.mock.calls.map(call => call[0])).toEqual([
      '/accounts/account-a?query=one',
      '/accounts/account-b#private-fragment',
    ])
  })

  it('suppresses a protected route on logout until the anonymous public redirect', () => {
    const lifecycle = createDependencies({ state: 'granted', scope: 'anonymous', generation: 1 })
    const { rerender } = renderHook(
      ({ url }) => useConsentAwarePageViews(url, lifecycle.dependencies),
      { initialProps: { url: '/' } },
    )

    rerender({ url: '/dashboard' })
    lifecycle.publish({ state: 'pending', scope: 'user-a', generation: 2 })
    lifecycle.publish({ state: 'granted', scope: 'user-a', generation: 2 })
    lifecycle.publish({ state: 'pending', scope: 'anonymous', generation: 3 })
    lifecycle.publish({ state: 'granted', scope: 'anonymous', generation: 3 })
    expect(lifecycle.capture.mock.calls.map(call => call[0])).toEqual(['/', '/dashboard'])

    rerender({ url: '/' })
    expect(lifecycle.capture.mock.calls.map(call => call[0])).toEqual(['/', '/dashboard', '/'])
  })

  it('captures the same route once for each identified user scope', () => {
    const lifecycle = createDependencies({ state: 'granted', scope: 'user-a', generation: 1 })
    renderHook(() => useConsentAwarePageViews('/legal/privacy-policy', lifecycle.dependencies))

    lifecycle.publish({ state: 'pending', scope: 'user-b', generation: 2 })
    lifecycle.publish({ state: 'granted', scope: 'user-b', generation: 2 })

    expect(lifecycle.capture.mock.calls.map(call => call[0])).toEqual([
      '/legal/privacy-policy',
      '/legal/privacy-policy',
    ])
  })

  it('captures each granted navigation once and cleans up its subscriber', () => {
    const capture = mock((_url: string) => {})
    const cleanup = mock(() => {})
    const dependencies = {
      capture,
      getSnapshot: () => ({ state: 'granted', scope: 'user-a', generation: 1 }) as const,
      subscribe: (_next: (value: AnalyticsLifecycleSnapshot) => void) => {
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
    unmount()
    expect(cleanup).toHaveBeenCalledTimes(1)
  })

  it('captures a new visit when navigation returns to a previously visited route', () => {
    const lifecycle = createDependencies({ state: 'granted', scope: 'user-a', generation: 1 })
    const { rerender } = renderHook(
      ({ url }) => useConsentAwarePageViews(url, lifecycle.dependencies),
      { initialProps: { url: '/dashboard' } },
    )

    rerender({ url: '/planning' })
    rerender({ url: '/dashboard' })

    expect(lifecycle.capture.mock.calls.map(call => call[0])).toEqual([
      '/dashboard',
      '/planning',
      '/dashboard',
    ])
  })
})
