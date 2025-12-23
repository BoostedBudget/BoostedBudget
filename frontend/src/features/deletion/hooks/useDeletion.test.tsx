/**
 * useDeletion Hook Tests
 *
 * Tests the React hook for account deletion operations.
 * Uses MSW for API mocking with React Query.
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { describe, it, expect, afterEach, afterAll, beforeAll } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'
import React from 'react'
import { useDeletion } from './useDeletion'
import type { RequestDeletionResponse, CancelDeletionResponse } from '../types'

// Mock response data
const mockRequestResponse: RequestDeletionResponse = {
  message: 'Account deletion scheduled',
  scheduled_at: '2025-11-26T00:00:00Z',
  cancellation_token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token',
}

const mockCancelResponse: CancelDeletionResponse = {
  message: 'Account deletion canceled successfully',
}

// MSW server setup
const server = setupServer(
  http.post('*/api/v1/user/delete', () => {
    return HttpResponse.json(mockRequestResponse)
  }),
  http.get('*/api/v1/user/delete/cancel/:token', () => {
    return HttpResponse.json(mockCancelResponse)
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
      mutations: {
        retry: false,
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

describe('useDeletion', () => {
  beforeAll(() => {
    server.listen({ onUnhandledRequest: 'error' })
  })

  afterEach(() => {
    server.resetHandlers()
  })

  afterAll(() => {
    server.close()
  })

  describe('Request Deletion Tests', () => {
    it('should return initial state', () => {
      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isRequesting).toBe(false)
      expect(result.current.requestError).toBeNull()
      expect(result.current.isCanceling).toBe(false)
      expect(result.current.cancelError).toBeNull()
    })

    it('should successfully request deletion and return response', async () => {
      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: RequestDeletionResponse | null = null
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      expect(response).toEqual(mockRequestResponse)
      expect(result.current.requestError).toBeNull()
    })

    it('should set isRequesting=true during request', async () => {
      // Use a delayed response to capture loading state
      server.use(
        http.post('*/api/v1/user/delete', async () => {
          await new Promise((resolve) => setTimeout(resolve, 100))
          return HttpResponse.json(mockRequestResponse)
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      // Start the request (don't await)
      act(() => {
        result.current.requestAccountDeletion()
      })

      // Should be requesting immediately after act()
      await waitFor(() => {
        expect(result.current.isRequesting).toBe(true)
      }, { timeout: 200 })

      // Wait for completion
      await waitFor(() => {
        expect(result.current.isRequesting).toBe(false)
      }, { timeout: 2000 })
    }, 15000)

    it('should set isRequesting=false after completion', async () => {
      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      expect(result.current.isRequesting).toBe(false)
    })

    it('should handle errors and set requestError', async () => {
      server.use(
        http.post('*/api/v1/user/delete', () => {
          return HttpResponse.json(
            { message: 'Failed to request account deletion' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: RequestDeletionResponse | null = null
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      expect(response).toBeNull()
      expect(result.current.requestError).toBeInstanceOf(Error)
      expect(result.current.isRequesting).toBe(false)
    })

    it('should return null on error', async () => {
      server.use(
        http.post('*/api/v1/user/delete', () => {
          return HttpResponse.json(
            { message: 'Server error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: RequestDeletionResponse | null = null
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      expect(response).toBeNull()
    })

    it('should handle multiple consecutive requests', async () => {
      let requestCount = 0
      server.use(
        http.post('*/api/v1/user/delete', () => {
          requestCount++
          return HttpResponse.json(mockRequestResponse)
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      expect(requestCount).toBe(2)
      expect(result.current.requestError).toBeNull()
    })

    it('should convert API errors to Error instances', async () => {
      server.use(
        http.post('*/api/v1/user/delete', () => {
          return HttpResponse.json(
            { message: 'Server error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      // Error should be properly wrapped as an Error instance
      expect(result.current.requestError).toBeInstanceOf(Error)
      expect(result.current.requestError?.message).toBeTruthy()
    })
  })

  describe('Cancel Deletion Tests', () => {
    const testToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token'

    it('should successfully cancel deletion with valid token', async () => {
      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: CancelDeletionResponse | null = null
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      expect(response).toEqual(mockCancelResponse)
      expect(result.current.cancelError).toBeNull()
    })

    it('should set isCanceling=true during cancel', async () => {
      server.use(
        http.get('*/api/v1/user/delete/cancel/:token', async () => {
          await new Promise((resolve) => setTimeout(resolve, 100))
          return HttpResponse.json(mockCancelResponse)
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.cancelAccountDeletion(testToken)
      })

      // Should be canceling immediately
      await waitFor(() => {
        expect(result.current.isCanceling).toBe(true)
      }, { timeout: 200 })

      // Wait for completion
      await waitFor(() => {
        expect(result.current.isCanceling).toBe(false)
      }, { timeout: 2000 })
    })

    it('should set isCanceling=false after completion', async () => {
      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.cancelAccountDeletion(testToken)
      })

      expect(result.current.isCanceling).toBe(false)
    })

    it('should handle invalid token errors', async () => {
      server.use(
        http.get('*/api/v1/user/delete/cancel/:token', () => {
          return HttpResponse.json(
            { message: 'Invalid or expired token' },
            { status: 400 }
          )
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: CancelDeletionResponse | null = null
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      expect(response).toBeNull()
      expect(result.current.cancelError).toBeInstanceOf(Error)
    })

    it('should handle expired token errors', async () => {
      server.use(
        http.get('*/api/v1/user/delete/cancel/:token', () => {
          return HttpResponse.json(
            { message: 'Token has expired' },
            { status: 400 }
          )
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.cancelAccountDeletion(testToken)
      })

      expect(result.current.cancelError).toBeInstanceOf(Error)
      expect(result.current.isCanceling).toBe(false)
    })

    it('should return null on error', async () => {
      server.use(
        http.get('*/api/v1/user/delete/cancel/:token', () => {
          return HttpResponse.json(
            { message: 'Network error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: CancelDeletionResponse | null = null
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      expect(response).toBeNull()
    })

    it('should convert API errors to Error instances', async () => {
      server.use(
        http.get('*/api/v1/user/delete/cancel/:token', () => {
          return HttpResponse.json(
            { message: 'Server error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.cancelAccountDeletion(testToken)
      })

      // Error should be properly wrapped as an Error instance
      expect(result.current.cancelError).toBeInstanceOf(Error)
      expect(result.current.cancelError?.message).toBeTruthy()
    })
  })

  describe('State Management Tests', () => {
    it('should maintain separate state for request and cancel', async () => {
      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      // Request completed, cancel not affected
      expect(result.current.isRequesting).toBe(false)
      expect(result.current.isCanceling).toBe(false)
      expect(result.current.requestError).toBeNull()
      expect(result.current.cancelError).toBeNull()
    })

    it('should allow simultaneous request and cancel states', async () => {
      // Both operations take time
      server.use(
        http.post('*/api/v1/user/delete', async () => {
          await new Promise((resolve) => setTimeout(resolve, 200))
          return HttpResponse.json(mockRequestResponse)
        }),
        http.get('*/api/v1/user/delete/cancel/:token', async () => {
          await new Promise((resolve) => setTimeout(resolve, 200))
          return HttpResponse.json(mockCancelResponse)
        })
      )

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      // Start both operations
      act(() => {
        result.current.requestAccountDeletion()
        result.current.cancelAccountDeletion('token')
      })

      // Both should be in progress simultaneously
      await waitFor(() => {
        expect(result.current.isRequesting).toBe(true)
        expect(result.current.isCanceling).toBe(true)
      }, { timeout: 200 })

      // Wait for both to complete
      await waitFor(() => {
        expect(result.current.isRequesting).toBe(false)
        expect(result.current.isCanceling).toBe(false)
      }, { timeout: 2000 })
    })
  })
})
