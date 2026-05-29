import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { mockUseFeatureFlagEnabled } from '@/test/mocks/posthog'

// Note: the VITE_POSTHOG_KEY path in isPostHogConfigured is a build-time constant and
// cannot be mutated in tests. It is covered by the posthog.ts lib tests. Here we exercise
// the window.ENV.POSTHOG_KEY path to control configured vs. non-configured states.

const { useLoginEnabled } = await import('./useLoginEnabled')

describe('useLoginEnabled', () => {
  beforeEach(() => {
    mockUseFeatureFlagEnabled.mockClear()
    // Default: no PostHog key configured
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = { POSTHOG_KEY: undefined }
  })

  afterEach(() => {
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = undefined
  })

  it('returns { enabled: true, loading: false } when PostHog is NOT configured', () => {
    mockUseFeatureFlagEnabled.mockReturnValue(undefined)
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = {}

    const { result } = renderHook(() => useLoginEnabled())

    expect(result.current).toEqual({ enabled: true, loading: false })
  })

  it('returns { enabled: false, loading: true } when PostHog is configured and flag is undefined (loading)', () => {
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = { POSTHOG_KEY: 'test-key' }
    mockUseFeatureFlagEnabled.mockReturnValue(undefined)

    const { result } = renderHook(() => useLoginEnabled())

    expect(result.current).toEqual({ enabled: false, loading: true })
  })

  it('returns { enabled: true, loading: false } when PostHog is configured and flag is true', () => {
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = { POSTHOG_KEY: 'test-key' }
    mockUseFeatureFlagEnabled.mockReturnValue(true)

    const { result } = renderHook(() => useLoginEnabled())

    expect(result.current).toEqual({ enabled: true, loading: false })
  })

  it('returns { enabled: false, loading: false } when PostHog is configured and flag is false', () => {
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = { POSTHOG_KEY: 'test-key' }
    mockUseFeatureFlagEnabled.mockReturnValue(false)

    const { result } = renderHook(() => useLoginEnabled())

    expect(result.current).toEqual({ enabled: false, loading: false })
  })
})
