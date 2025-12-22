/**
 * useProfile Hook Tests
 *
 * Tests for the React hook that fetches user profile data.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useProfile } from './useProfile'
import * as profileApi from '../api/profileApi'
import type { User } from '../types'

// Mock the API module
vi.mock('../api/profileApi')

describe('useProfile', () => {
  const mockUser: User = {
    id: 'user-123',
    auth0_user_id: 'auth0|123',
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

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Initial state', () => {
    it('should return loading state initially', () => {
      // Arrange - never resolving promise to keep loading state
      vi.mocked(profileApi.fetchProfile).mockImplementation(
        () => new Promise(() => {})
      )

      // Act
      const { result } = renderHook(() => useProfile())

      // Assert
      expect(result.current.loading).toBe(true)
      expect(result.current.user).toBeNull()
      expect(result.current.error).toBeNull()
    })
  })

  describe('Successful fetch', () => {
    it('should fetch profile on mount', async () => {
      // Arrange
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Assert - Initial loading state
      expect(result.current.loading).toBe(true)

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - API called
      expect(profileApi.fetchProfile).toHaveBeenCalledTimes(1)
    })

    it('should return user data after successful fetch', async () => {
      // Arrange
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.user).toEqual(mockUser)
      expect(result.current.error).toBeNull()
    })
  })

  describe('Error handling', () => {
    it('should return error on failed fetch', async () => {
      // Arrange
      const error = new Error('Network error')
      vi.mocked(profileApi.fetchProfile).mockRejectedValue(error)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.error).toEqual(error)
      expect(result.current.user).toBeNull()
    })

    it('should convert non-Error objects to Error instances', async () => {
      // Arrange - Reject with a string instead of Error
      vi.mocked(profileApi.fetchProfile).mockRejectedValue('Something went wrong')

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - Should be wrapped in Error
      expect(result.current.error).toBeInstanceOf(Error)
      expect(result.current.error?.message).toBe('Failed to fetch user profile')
      expect(result.current.user).toBeNull()
    })
  })

  describe('Refetch functionality', () => {
    it('should provide refetch function', async () => {
      // Arrange
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - refetch function exists
      expect(typeof result.current.refetch).toBe('function')
    })

    it('should refetch profile when refetch is called', async () => {
      // Arrange
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(profileApi.fetchProfile).toHaveBeenCalledTimes(1)

      // Call refetch
      act(() => {
        result.current.refetch()
      })

      // Wait for refetch to complete
      await waitFor(() => {
        expect(profileApi.fetchProfile).toHaveBeenCalledTimes(2)
      })
    })

    it('should update user data on refetch with new data', async () => {
      // Arrange - First call returns initial user
      vi.mocked(profileApi.fetchProfile).mockResolvedValueOnce(mockUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.user?.email).toBe('test@example.com')

      // Arrange - Second call returns updated user
      const updatedUser = { ...mockUser, email: 'updated@example.com' }
      vi.mocked(profileApi.fetchProfile).mockResolvedValueOnce(updatedUser)

      // Call refetch
      act(() => {
        result.current.refetch()
      })

      // Wait for refetch to complete
      await waitFor(() => {
        expect(result.current.user?.email).toBe('updated@example.com')
      })

      // Assert
      expect(result.current.user).toEqual(updatedUser)
    })

    it('should clear previous error on successful refetch', async () => {
      // Arrange - First call fails
      const error = new Error('Network error')
      vi.mocked(profileApi.fetchProfile).mockRejectedValueOnce(error)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.error).toEqual(error)

      // Arrange - Second call succeeds
      vi.mocked(profileApi.fetchProfile).mockResolvedValueOnce(mockUser)

      // Call refetch
      act(() => {
        result.current.refetch()
      })

      // Wait for refetch to complete (wait for user to be set, not just error to be null)
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
        expect(result.current.user).toEqual(mockUser)
      })

      // Assert - error should be cleared
      expect(result.current.error).toBeNull()
    })

    it('should set loading state during refetch', async () => {
      // Arrange
      vi.mocked(profileApi.fetchProfile).mockResolvedValueOnce(mockUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Mock slow refetch
      vi.mocked(profileApi.fetchProfile).mockImplementationOnce(
        () => new Promise((resolve) => setTimeout(() => resolve(mockUser), 100))
      )

      // Call refetch
      act(() => {
        result.current.refetch()
      })

      // Assert - should be loading during refetch
      expect(result.current.loading).toBe(true)

      // Wait for refetch to complete
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })
    })
  })

  describe('User data types', () => {
    it('should handle premium tier user', async () => {
      // Arrange
      const premiumUser: User = {
        ...mockUser,
        subscription_tier: 'premium',
        subscription_status: 'active',
      }
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(premiumUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.user?.subscription_tier).toBe('premium')
      expect(result.current.user?.subscription_status).toBe('active')
    })

    it('should handle user with different region', async () => {
      // Arrange
      const lhrUser: User = {
        ...mockUser,
        primary_region: 'lhr',
      }
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(lhrUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.user?.primary_region).toBe('lhr')
    })

    it('should handle unverified email user', async () => {
      // Arrange
      const unverifiedUser: User = {
        ...mockUser,
        email_verified: false,
      }
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(unverifiedUser)

      // Act
      const { result } = renderHook(() => useProfile())

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.user?.email_verified).toBe(false)
    })
  })
})
