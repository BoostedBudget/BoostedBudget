/**
 * useProfile Hook Tests
 *
 * Tests the React hook with React Query caching strategy.
 * Uses MSW for API mocking.
 */

import { describe, it, expect, afterEach, afterAll, beforeAll } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'
import React from 'react'
import { useProfile } from './useProfile'
import type { User } from '@/api/generated/models'

// Mock user data matching generated User type
const mockUser: User = {
  id: 'user-123',
  clerk_user_id: 'auth0|123',
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

// MSW server setup
const server = setupServer(
  http.get('*/api/v1/user/profile', () => {
    return HttpResponse.json(mockUser)
  })
)

// Create a fresh QueryClient for each test
function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0,
      },
    },
  })
}

// Wrapper component for tests
function createWrapper() {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    )
  }
}

describe('useProfile', () => {
  beforeAll(() => {
    server.listen({ onUnhandledRequest: 'error' })
  })

  afterEach(() => {
    server.resetHandlers()
  })

  afterAll(() => {
    server.close()
  })

  describe('Initial state', () => {
    it('should return loading state initially', async () => {
      // Use a delayed response to capture loading state
      server.use(
        http.get('*/api/v1/user/profile', async () => {
          await new Promise((resolve) => setTimeout(resolve, 100))
          return HttpResponse.json(mockUser)
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Assert initial loading state
      expect(result.current.loading).toBe(true)
      expect(result.current.user).toBeNull()
      expect(result.current.error).toBeNull()

      // Wait for completion
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })
    })
  })

  describe('Successful fetch', () => {
    it('should fetch profile on mount', async () => {
      let fetchCount = 0
      server.use(
        http.get('*/api/v1/user/profile', () => {
          fetchCount++
          return HttpResponse.json(mockUser)
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Assert - Initial loading state
      expect(result.current.loading).toBe(true)

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - API called
      expect(fetchCount).toBe(1)
    })

    it('should return user data after successful fetch', async () => {
      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

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
      server.use(
        http.get('*/api/v1/user/profile', () => {
          return HttpResponse.json(
            { message: 'Network error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.error).toBeTruthy()
      expect(result.current.user).toBeNull()
    })

    it('should wrap errors in Error instances', async () => {
      server.use(
        http.get('*/api/v1/user/profile', () => {
          return HttpResponse.json(
            { message: 'Server error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - Should be wrapped in Error
      expect(result.current.error).toBeInstanceOf(Error)
      expect(result.current.user).toBeNull()
    })
  })

  describe('Refetch functionality', () => {
    it('should provide refetch function', async () => {
      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - refetch function exists
      expect(typeof result.current.refetch).toBe('function')
    })

    it('should refetch profile when refetch is called', async () => {
      let fetchCount = 0
      server.use(
        http.get('*/api/v1/user/profile', () => {
          fetchCount++
          return HttpResponse.json(mockUser)
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(fetchCount).toBe(1)

      // Call refetch
      result.current.refetch()

      // Wait for refetch to complete
      await waitFor(() => {
        expect(fetchCount).toBe(2)
      })
    })

    it('should update user data on refetch with new data', async () => {
      let fetchCount = 0
      server.use(
        http.get('*/api/v1/user/profile', () => {
          fetchCount++
          if (fetchCount === 1) {
            return HttpResponse.json(mockUser)
          }
          return HttpResponse.json({ ...mockUser, email: 'updated@example.com' })
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.user?.email).toBe('test@example.com')

      // Call refetch
      result.current.refetch()

      // Wait for refetch to complete with updated data
      await waitFor(() => {
        expect(result.current.user?.email).toBe('updated@example.com')
      })
    })
  })

  describe('User data types', () => {
    it('should handle premium tier user', async () => {
      const premiumUser: User = {
        ...mockUser,
        subscription_tier: 'premium',
        subscription_status: 'active',
      }

      server.use(
        http.get('*/api/v1/user/profile', () => {
          return HttpResponse.json(premiumUser)
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.user?.subscription_tier).toBe('premium')
      expect(result.current.user?.subscription_status).toBe('active')
    })

    it('should handle user with different region', async () => {
      const lhrUser: User = {
        ...mockUser,
        primary_region: 'lhr',
      }

      server.use(
        http.get('*/api/v1/user/profile', () => {
          return HttpResponse.json(lhrUser)
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.user?.primary_region).toBe('lhr')
    })

    it('should handle unverified email user', async () => {
      const unverifiedUser: User = {
        ...mockUser,
        email_verified: false,
      }

      server.use(
        http.get('*/api/v1/user/profile', () => {
          return HttpResponse.json(unverifiedUser)
        })
      )

      const { result } = renderHook(() => useProfile(), {
        wrapper: createWrapper(),
      })

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.user?.email_verified).toBe(false)
    })
  })

  describe('Caching', () => {
    it('should use React Query caching', async () => {
      let fetchCount = 0
      server.use(
        http.get('*/api/v1/user/profile', () => {
          fetchCount++
          return HttpResponse.json(mockUser)
        })
      )

      // Use shared QueryClient for caching test
      const queryClient = createTestQueryClient()
      const wrapper = ({ children }: { children: React.ReactNode }) => (
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      )

      // First render
      const { result: result1, unmount: unmount1 } = renderHook(
        () => useProfile(),
        { wrapper }
      )

      await waitFor(() => {
        expect(result1.current.loading).toBe(false)
      })

      expect(fetchCount).toBe(1)
      unmount1()

      // Second render with same QueryClient - should use cache
      const { result: result2 } = renderHook(
        () => useProfile(),
        { wrapper }
      )

      await waitFor(() => {
        expect(result2.current.loading).toBe(false)
      })

      // Should not fetch again - using cached data
      expect(fetchCount).toBe(1)
      expect(result2.current.user).toEqual(mockUser)
    })
  })
})
