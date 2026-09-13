/**
 * useProfile Hook Tests
 * Issue #575: Migrated from MSW to centralized API mocks
 *
 * Tests the React hook with React Query caching strategy.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { useProfile } from './useProfile'
import type { User } from '@boostedbudget/api-client/models'
import {
  mockUseGetUserProfile,
  mockRefetchUserProfile,
} from '@/test/mocks/profile-api'

const mockUser: User = {
  id: 'user-123',
  clerk_user_id: 'user_123',
  email: 'test@example.com',
  email_verified: true,
  primary_region: 'iad',
  subscription_tier: 'free',
  subscription_status: 'active',
  is_active: true,
  is_deleted: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-15T10:00:00Z',
  last_login_at: '2025-01-15T10:00:00Z',
}

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('useProfile', () => {
  beforeEach(() => {
    mockUseGetUserProfile.mockClear()
    mockRefetchUserProfile.mockClear()

    // Default successful response
    mockUseGetUserProfile.mockReturnValue({
      data: mockUser,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })
  })

  describe('Initial State', () => {
    it('scopes the root analytics profile cache to the current Clerk identity', () => {
      renderHook(() => useProfile({ enabled: true, userId: 'user-a' }), {
        wrapper: createWrapper(),
      })

      expect(mockUseGetUserProfile).toHaveBeenCalledWith({
        query: expect.objectContaining({
          queryKey: ['/api/v1/user/profile', 'user-a'],
        }),
      })
    })

    it('should return loading=true initially', () => {
      mockUseGetUserProfile.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchUserProfile,
      })

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.user).toBeNull()
      expect(result.current.error).toBeNull()
    })
  })

  describe('Successful Fetch', () => {
    it('should return user data on successful fetch', () => {
      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.user).toEqual(mockUser)
      expect(result.current.error).toBeNull()
    })

    it('should return user email', () => {
      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.user?.email).toBe('test@example.com')
    })

    it('should return subscription tier', () => {
      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.user?.subscription_tier).toBe('free')
    })
  })

  describe('Error Handling', () => {
    it('should return error on failed fetch', () => {
      const error = new Error('Network error')
      mockUseGetUserProfile.mockReturnValue({
        data: undefined,
        isLoading: false,
        error,
        refetch: mockRefetchUserProfile,
      })

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeTruthy()
      expect(result.current.user).toBeNull()
    })

    it('should wrap errors in Error instances', () => {
      mockUseGetUserProfile.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: 'String error',
        refetch: mockRefetchUserProfile,
      })

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeInstanceOf(Error)
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function', () => {
      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(typeof result.current.refetch).toBe('function')
    })

    it('should call underlying refetch when invoked', () => {
      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      result.current.refetch()

      expect(mockRefetchUserProfile).toHaveBeenCalled()
    })
  })

  describe('User Types', () => {
    it('should handle premium tier user', () => {
      const premiumUser: User = {
        ...mockUser,
        subscription_tier: 'premium',
        subscription_status: 'active',
      }

      mockUseGetUserProfile.mockReturnValue({
        data: premiumUser,
        isLoading: false,
        error: null,
        refetch: mockRefetchUserProfile,
      })

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.user?.subscription_tier).toBe('premium')
    })

    it('should handle user with different region', () => {
      const ewrUser: User = {
        ...mockUser,
        primary_region: 'ewr',
      }

      mockUseGetUserProfile.mockReturnValue({
        data: ewrUser,
        isLoading: false,
        error: null,
        refetch: mockRefetchUserProfile,
      })

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.user?.primary_region).toBe('ewr')
    })

    it('should handle unverified email user', () => {
      const unverifiedUser: User = {
        ...mockUser,
        email_verified: false,
      }

      mockUseGetUserProfile.mockReturnValue({
        data: unverifiedUser,
        isLoading: false,
        error: null,
        refetch: mockRefetchUserProfile,
      })

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      expect(result.current.user?.email_verified).toBe(false)
    })
  })
})
