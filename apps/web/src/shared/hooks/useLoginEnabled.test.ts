import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'

// Must mock before dynamic import
const mockUseFeatureFlagEnabled = mock(() => undefined as boolean | undefined)

mock.module('posthog-js/react', () => ({
  PostHogProvider: ({ children }: { children: unknown }) => children,
  useFeatureFlagEnabled: mockUseFeatureFlagEnabled,
}))

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
    mockUseFeatureFlagEnabled.mockImplementation(() => undefined)
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = {}

    const { result } = renderHook(() => useLoginEnabled())

    expect(result.current).toEqual({ enabled: true, loading: false })
  })

  it('returns { enabled: false, loading: true } when PostHog is configured and flag is undefined (loading)', () => {
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = { POSTHOG_KEY: 'test-key' }
    mockUseFeatureFlagEnabled.mockImplementation(() => undefined)

    const { result } = renderHook(() => useLoginEnabled())

    expect(result.current).toEqual({ enabled: false, loading: true })
  })

  it('returns { enabled: true, loading: false } when PostHog is configured and flag is true', () => {
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = { POSTHOG_KEY: 'test-key' }
    mockUseFeatureFlagEnabled.mockImplementation(() => true)

    const { result } = renderHook(() => useLoginEnabled())

    expect(result.current).toEqual({ enabled: true, loading: false })
  })

  it('returns { enabled: false, loading: false } when PostHog is configured and flag is false', () => {
    ;(window as { ENV?: { POSTHOG_KEY?: string } }).ENV = { POSTHOG_KEY: 'test-key' }
    mockUseFeatureFlagEnabled.mockImplementation(() => false)

    const { result } = renderHook(() => useLoginEnabled())

    expect(result.current).toEqual({ enabled: false, loading: false })
  })
})
