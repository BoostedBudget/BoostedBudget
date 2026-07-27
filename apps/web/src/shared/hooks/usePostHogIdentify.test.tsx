import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { ANALYTICS_EVENTS } from '../analytics'
// Issue #1323: Use centralized Clerk mock (preloaded via bunfig.toml).
// Inline mock.module('@clerk/react-router') with only {useUser} clobbers the full
// mock and pollutes tests that depend on other Clerk exports (e.g. useActiveAccounts).
import { mockUseUser } from '@/test/mocks/clerk'
// Issue #1413: Use the centralized preloaded posthog mock (see
// feedback_bun_test_mock_module_global.md) instead of an inline mock.module() call,
// so this file can't shadow the richer mock other files rely on (e.g. setPersonProperties).
import {
  mockPosthogIdentify,
  mockPosthogCapture,
  mockPosthogReset,
  mockPosthogSetPersonProperties,
} from '@/test/mocks/posthog'
// Issue #1413: Server-authoritative tier/comp/status come from the profile query
// (useProfile -> useGetUserProfile), centrally mocked to avoid a real network fetch.
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import type { User } from '@boostedbudget/api-client/models'

import { usePostHogIdentify } from './usePostHogIdentify'

// Mocked user shape
function makeUser(overrides?: Partial<{ createdAt: Date; id: string; publicMetadata: Record<string, unknown> }>) {
  return {
    id: 'user_123',
    primaryEmailAddress: { emailAddress: 'test@example.com' },
    fullName: 'Test User',
    publicMetadata: { subscription_tier: 'premium', primary_region: 'us-east-1' },
    createdAt: new Date(Date.now() - 120_000), // 2 minutes ago (not a new sign-up)
    ...overrides,
  }
}

function makeProfile(overrides?: Partial<User>): User {
  return {
    id: 'user_123',
    clerk_user_id: 'user_123',
    email: 'test@example.com',
    email_verified: true,
    had_trial: false,
    primary_region: 'us-east-1',
    subscription_tier: 'premium',
    subscription_status: 'active',
    effective_tier: 'premium',
    effective_status: 'active',
    is_comped: false,
    timezone: 'America/New_York',
    timezone_source: 'user',
    is_active: true,
    is_deleted: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
    ...overrides,
  }
}

function mockProfileQuery(overrides: { data?: User; isLoading?: boolean } = {}) {
  mockUseGetUserProfile.mockReturnValue({
    data: overrides.data,
    isLoading: overrides.isLoading ?? true,
    error: null,
    refetch: () => Promise.resolve(),
  })
}

describe('usePostHogIdentify', () => {
  beforeEach(() => {
    mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
    mockPosthogIdentify.mockClear()
    mockPosthogCapture.mockClear()
    mockPosthogReset.mockClear()
    mockPosthogSetPersonProperties.mockClear()
    mockProfileQuery() // default: loading, no data
  })

  describe('Initial render with no user', () => {
    it('should not call identify, capture, or reset when user is null on mount', () => {
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      renderHook(() => usePostHogIdentify())

      expect(mockPosthogIdentify).not.toHaveBeenCalled()
      expect(mockPosthogCapture).not.toHaveBeenCalled()
      expect(mockPosthogReset).not.toHaveBeenCalled()
    })
  })

  describe('Sign-in transition (null → user)', () => {
    it('should call identify and fire sign_in_completed when user appears', () => {
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUseUser.mockReturnValue({ user: makeUser(), isLoaded: true, isSignedIn: true })
      rerender()

      expect(mockPosthogIdentify).toHaveBeenCalledWith('user_123', {
        subscription_tier: 'premium',
        primary_region: 'us-east-1',
      })
      expect(mockPosthogCapture).toHaveBeenCalledWith(ANALYTICS_EVENTS.SIGN_IN_COMPLETED)
    })

    it('should NOT fire sign_up_completed for an existing user (createdAt > 60s ago)', () => {
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUseUser.mockReturnValue({ user: makeUser({ createdAt: new Date(Date.now() - 120_000) }), isLoaded: true, isSignedIn: true })
      rerender()

      const capturedEvents = mockPosthogCapture.mock.calls.map((c: unknown[]) => c[0])
      expect(capturedEvents).not.toContain(ANALYTICS_EVENTS.SIGN_UP_COMPLETED)
    })

    it('should fire sign_up_completed when createdAt is within last 60 seconds', () => {
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUseUser.mockReturnValue({ user: makeUser({ createdAt: new Date(Date.now() - 5_000) }), isLoaded: true, isSignedIn: true })
      rerender()

      const capturedEvents = mockPosthogCapture.mock.calls.map((c: unknown[]) => c[0])
      expect(capturedEvents).toContain(ANALYTICS_EVENTS.SIGN_IN_COMPLETED)
      expect(capturedEvents).toContain(ANALYTICS_EVENTS.SIGN_UP_COMPLETED)
    })

    it('should not fire sign_out_completed on sign-in', () => {
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUseUser.mockReturnValue({ user: makeUser(), isLoaded: true, isSignedIn: true })
      rerender()

      const capturedEvents = mockPosthogCapture.mock.calls.map((c: unknown[]) => c[0])
      expect(capturedEvents).not.toContain(ANALYTICS_EVENTS.SIGN_OUT_COMPLETED)
      expect(mockPosthogReset).not.toHaveBeenCalled()
    })
  })

  describe('Sign-out transition (user → null)', () => {
    it('should fire sign_out_completed and call reset when user becomes null', () => {
      mockUseUser.mockReturnValue({ user: makeUser(), isLoaded: true, isSignedIn: true })
      const { rerender } = renderHook(() => usePostHogIdentify())

      // Clear calls from initial mount (no transition from null, so nothing should fire)
      mockPosthogCapture.mockClear()
      mockPosthogReset.mockClear()

      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      rerender()

      expect(mockPosthogCapture).toHaveBeenCalledWith(ANALYTICS_EVENTS.SIGN_OUT_COMPLETED)
      expect(mockPosthogReset).toHaveBeenCalled()
    })

    it('should not call identify on sign-out', () => {
      mockUseUser.mockReturnValue({ user: makeUser(), isLoaded: true, isSignedIn: true })
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockPosthogIdentify.mockClear()
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      rerender()

      expect(mockPosthogIdentify).not.toHaveBeenCalled()
    })
  })

  describe('Stable user (no transition)', () => {
    it('should not fire any events when the same user object is re-rendered', () => {
      const user = makeUser()
      mockUseUser.mockReturnValue({ user, isLoaded: true, isSignedIn: true })
      const { rerender } = renderHook(() => usePostHogIdentify())

      // Same user reference re-render (no state change)
      mockPosthogCapture.mockClear()
      mockPosthogIdentify.mockClear()
      mockPosthogReset.mockClear()

      rerender()

      // useEffect only re-runs when user changes; same ref → no re-run
      expect(mockPosthogCapture).not.toHaveBeenCalled()
      expect(mockPosthogIdentify).not.toHaveBeenCalled()
      expect(mockPosthogReset).not.toHaveBeenCalled()
    })
  })

  describe('User with missing optional fields', () => {
    it('should omit email when primaryEmailAddress is null', () => {
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUseUser.mockReturnValue({
        user: {
          id: 'user_456',
          primaryEmailAddress: null,
          fullName: null,
          publicMetadata: {},
          createdAt: new Date(Date.now() - 120_000),
        } as unknown as ReturnType<typeof makeUser>,
        isLoaded: true,
        isSignedIn: true,
      })
      rerender()

      expect(mockPosthogIdentify).toHaveBeenCalledWith('user_456', {
        subscription_tier: undefined,
        primary_region: undefined,
      })
      // email and name should NOT be in the properties object (PII policy)
      const callArgs = mockPosthogIdentify.mock.calls[0][1]
      expect('email' in callArgs).toBe(false)
      expect('name' in callArgs).toBe(false)
    })

    it('should handle user with null createdAt (no sign_up_completed)', () => {
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      const { rerender } = renderHook(() => usePostHogIdentify())

      mockUseUser.mockReturnValue({
        user: { ...makeUser(), createdAt: null } as unknown as ReturnType<typeof makeUser>,
        isLoaded: true,
        isSignedIn: true,
      })
      rerender()

      const capturedEvents = mockPosthogCapture.mock.calls.map((c: unknown[]) => c[0])
      expect(capturedEvents).not.toContain(ANALYTICS_EVENTS.SIGN_UP_COMPLETED)
    })
  })

  // Issue #1413: comped users were reporting the stale Clerk-derived tier (or none at
  // all) to every tier-segmented funnel, with no way to segment out comped accounts.
  // The identify() call above stays untouched (still fires immediately on sign-in with
  // the Clerk tier, preserving sign_in_completed timing) — this is a *second*, independent
  // sync of server-authoritative fields once the profile query resolves.
  describe('Person-property sync from the profile (server-authoritative tier/comp)', () => {
    it('does not call setPersonProperties before the profile resolves', () => {
      mockUseUser.mockReturnValue({ user: makeUser(), isLoaded: true, isSignedIn: true })
      mockProfileQuery({ data: undefined, isLoading: true })

      renderHook(() => usePostHogIdentify())

      expect(mockPosthogSetPersonProperties).not.toHaveBeenCalled()
    })

    it('does not call setPersonProperties when signed out', () => {
      mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
      mockProfileQuery({ data: makeProfile(), isLoading: false })

      renderHook(() => usePostHogIdentify())

      expect(mockPosthogSetPersonProperties).not.toHaveBeenCalled()
    })

    it('sets person properties with the effective tier, status, and is_comped once the profile resolves', () => {
      mockUseUser.mockReturnValue({ user: makeUser(), isLoaded: true, isSignedIn: true })
      mockProfileQuery({
        data: makeProfile({ effective_tier: 'premium', effective_status: 'active', is_comped: false }),
        isLoading: false,
      })

      renderHook(() => usePostHogIdentify())

      expect(mockPosthogSetPersonProperties).toHaveBeenCalledWith({
        subscription_tier: 'premium',
        subscription_status_effective: 'active',
        is_comped: false,
      })
    })

    it('reports is_comped: true and the comp tier for a comped user, not the stale Clerk tier', () => {
      // Clerk's publicMetadata.subscription_tier is stale ("free") — the DB/profile says
      // this user is comped onto household_3. The bug this guards: identify() reads the
      // stale Clerk value, so without this second sync, is_comped never surfaces at all.
      mockUseUser.mockReturnValue({
        user: makeUser({ publicMetadata: { subscription_tier: 'free', primary_region: 'us-east-1' } }),
        isLoaded: true,
        isSignedIn: true,
      })
      mockProfileQuery({
        data: makeProfile({ effective_tier: 'household_3', effective_status: 'active', is_comped: true }),
        isLoading: false,
      })

      renderHook(() => usePostHogIdentify())

      expect(mockPosthogSetPersonProperties).toHaveBeenCalledWith({
        subscription_tier: 'household_3',
        subscription_status_effective: 'active',
        is_comped: true,
      })
    })
  })
})
