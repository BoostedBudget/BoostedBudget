import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { ANALYTICS_EVENTS } from '../analytics'

const mockIdentify = mock(() => {})
const mockCapture = mock(() => {})
const mockReset = mock(() => {})

mock.module('../lib/posthog', () => ({
  posthog: {
    identify: mockIdentify,
    capture: mockCapture,
    reset: mockReset,
  },
}))

// Mocked user shape
function makeUser(overrides?: Partial<{ createdAt: Date; id: string }>) {
  return {
    id: 'user_123',
    primaryEmailAddress: { emailAddress: 'test@example.com' },
    fullName: 'Test User',
    publicMetadata: { subscription_tier: 'premium', primary_region: 'us-east-1' },
    createdAt: new Date(Date.now() - 120_000), // 2 minutes ago (not a new sign-up)
    ...overrides,
  }
}

let mockUser: ReturnType<typeof makeUser> | null = null

mock.module('@clerk/react-router', () => ({
  useUser: () => ({ user: mockUser }),
}))

// Import after mocks are set up
const { usePostHogIdentify } = await import('./usePostHogIdentify')

describe('usePostHogIdentify', () => {
  beforeEach(() => {
    mockUser = null
    mockIdentify.mockClear()
    mockCapture.mockClear()
    mockReset.mockClear()
  })

  describe('Initial render with no user', () => {
    it('should not call identify, capture, or reset when user is null on mount', () => {
      mockUser = null
      renderHook(() => usePostHogIdentify())

      expect(mockIdentify).not.toHaveBeenCalled()
      expect(mockCapture).not.toHaveBeenCalled()
      expect(mockReset).not.toHaveBeenCalled()
    })
  })

  describe('Sign-in transition (null → user)', () => {
    it('should call identify and fire sign_in_completed when user appears', () => {
      mockUser = null
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUser = makeUser()
      rerender()

      expect(mockIdentify).toHaveBeenCalledWith('user_123', {
        subscription_tier: 'premium',
        primary_region: 'us-east-1',
      })
      expect(mockCapture).toHaveBeenCalledWith(ANALYTICS_EVENTS.SIGN_IN_COMPLETED)
    })

    it('should NOT fire sign_up_completed for an existing user (createdAt > 60s ago)', () => {
      mockUser = null
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUser = makeUser({ createdAt: new Date(Date.now() - 120_000) })
      rerender()

      const capturedEvents = mockCapture.mock.calls.map((c: unknown[]) => c[0])
      expect(capturedEvents).not.toContain(ANALYTICS_EVENTS.SIGN_UP_COMPLETED)
    })

    it('should fire sign_up_completed when createdAt is within last 60 seconds', () => {
      mockUser = null
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUser = makeUser({ createdAt: new Date(Date.now() - 5_000) })
      rerender()

      const capturedEvents = mockCapture.mock.calls.map((c: unknown[]) => c[0])
      expect(capturedEvents).toContain(ANALYTICS_EVENTS.SIGN_IN_COMPLETED)
      expect(capturedEvents).toContain(ANALYTICS_EVENTS.SIGN_UP_COMPLETED)
    })

    it('should not fire sign_out_completed on sign-in', () => {
      mockUser = null
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUser = makeUser()
      rerender()

      const capturedEvents = mockCapture.mock.calls.map((c: unknown[]) => c[0])
      expect(capturedEvents).not.toContain(ANALYTICS_EVENTS.SIGN_OUT_COMPLETED)
      expect(mockReset).not.toHaveBeenCalled()
    })
  })

  describe('Sign-out transition (user → null)', () => {
    it('should fire sign_out_completed and call reset when user becomes null', () => {
      mockUser = makeUser()
      const { rerender } = renderHook(() => usePostHogIdentify())

      // Clear calls from initial mount (no transition from null, so nothing should fire)
      mockCapture.mockClear()
      mockReset.mockClear()

      mockUser = null
      rerender()

      expect(mockCapture).toHaveBeenCalledWith(ANALYTICS_EVENTS.SIGN_OUT_COMPLETED)
      expect(mockReset).toHaveBeenCalled()
    })

    it('should not call identify on sign-out', () => {
      mockUser = makeUser()
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockIdentify.mockClear()
      mockUser = null
      rerender()

      expect(mockIdentify).not.toHaveBeenCalled()
    })
  })

  describe('Stable user (no transition)', () => {
    it('should not fire any events when the same user object is re-rendered', () => {
      const user = makeUser()
      mockUser = user
      const { rerender } = renderHook(() => usePostHogIdentify())

      // Same user reference re-render (no state change)
      mockCapture.mockClear()
      mockIdentify.mockClear()
      mockReset.mockClear()

      rerender()

      // useEffect only re-runs when user changes; same ref → no re-run
      expect(mockCapture).not.toHaveBeenCalled()
      expect(mockIdentify).not.toHaveBeenCalled()
      expect(mockReset).not.toHaveBeenCalled()
    })
  })

  describe('User with missing optional fields', () => {
    it('should omit email when primaryEmailAddress is null', () => {
      mockUser = null
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUser = {
        id: 'user_456',
        primaryEmailAddress: null,
        fullName: null,
        publicMetadata: {},
        createdAt: new Date(Date.now() - 120_000),
      } as unknown as ReturnType<typeof makeUser>
      rerender()

      expect(mockIdentify).toHaveBeenCalledWith('user_456', {
        subscription_tier: undefined,
        primary_region: undefined,
      })
      // email and name should NOT be in the properties object (PII policy)
      const callArgs = mockIdentify.mock.calls[0][1]
      expect('email' in callArgs).toBe(false)
      expect('name' in callArgs).toBe(false)
    })

    it('should handle user with null createdAt (no sign_up_completed)', () => {
      mockUser = null
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUser = {
        ...makeUser(),
        createdAt: null,
      } as unknown as ReturnType<typeof makeUser>
      rerender()

      const capturedEvents = mockCapture.mock.calls.map((c: unknown[]) => c[0])
      expect(capturedEvents).not.toContain(ANALYTICS_EVENTS.SIGN_UP_COMPLETED)
    })
  })
})
