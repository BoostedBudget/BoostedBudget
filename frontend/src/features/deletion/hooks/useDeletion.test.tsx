/**
 * useDeletion Hook Tests
 *
 * Tests the React hook for account deletion operations.
 * Uses centralized API mocks (Issue #575).
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { useDeletion } from './useDeletion'
import {
  mockUseRequestDeletion,
  mockRequestDeletionMutateAsync,
  mockCancelDeletion,
} from '@/test/mocks/user-api'

// Create a fresh QueryClient for each test
function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
}

function createWrapper() {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('useDeletion', () => {
  beforeEach(() => {
    mockUseRequestDeletion.mockClear()
    mockRequestDeletionMutateAsync.mockClear()
    mockCancelDeletion.mockClear()

    // Default successful responses
    mockRequestDeletionMutateAsync.mockResolvedValue({
      message: 'Account deletion scheduled',
      scheduled_at: '2025-11-26T00:00:00Z',
      cancellation_token: 'test-token',
    })

    mockUseRequestDeletion.mockReturnValue({
      mutateAsync: mockRequestDeletionMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })

    mockCancelDeletion.mockResolvedValue({
      message: 'Account deletion canceled successfully',
    })
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

      let response: Awaited<ReturnType<typeof result.current.requestAccountDeletion>>
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      expect(response!).toEqual({
        message: 'Account deletion scheduled',
        scheduled_at: '2025-11-26T00:00:00Z',
        cancellation_token: 'test-token',
      })
      expect(result.current.requestError).toBeNull()
    })

    it('should set isRequesting=true during request', async () => {
      // Simulate pending state
      mockUseRequestDeletion.mockReturnValue({
        mutateAsync: mockRequestDeletionMutateAsync,
        isPending: true,
        isError: false,
        error: null,
      })

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isRequesting).toBe(true)
    })

    it('should handle errors and set requestError', async () => {
      const error = new Error('Failed to request account deletion')
      mockRequestDeletionMutateAsync.mockRejectedValue(error)
      mockUseRequestDeletion.mockReturnValue({
        mutateAsync: mockRequestDeletionMutateAsync,
        isPending: false,
        isError: true,
        error,
      })

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: Awaited<ReturnType<typeof result.current.requestAccountDeletion>>
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      expect(response!).toBeNull()
      expect(result.current.requestError).toBeInstanceOf(Error)
      expect(result.current.isRequesting).toBe(false)
    })

    it('should return null on error', async () => {
      mockRequestDeletionMutateAsync.mockRejectedValue(new Error('Server error'))

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: Awaited<ReturnType<typeof result.current.requestAccountDeletion>>
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      expect(response!).toBeNull()
    })

    it('should convert API errors to Error instances', async () => {
      const error = new Error('Server error')
      mockUseRequestDeletion.mockReturnValue({
        mutateAsync: mockRequestDeletionMutateAsync,
        isPending: false,
        isError: true,
        error,
      })

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      expect(result.current.requestError).toBeInstanceOf(Error)
      expect(result.current.requestError?.message).toBeTruthy()
    })
  })

  describe('Cancel Deletion Tests', () => {
    const testToken = 'test-cancellation-token'

    it('should successfully cancel deletion with valid token', async () => {
      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: Awaited<ReturnType<typeof result.current.cancelAccountDeletion>>
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      expect(response!).toEqual({
        message: 'Account deletion canceled successfully',
      })
      expect(result.current.cancelError).toBeNull()
    })

    it('should handle invalid token errors', async () => {
      mockCancelDeletion.mockRejectedValue(new Error('Invalid or expired token'))

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: Awaited<ReturnType<typeof result.current.cancelAccountDeletion>>
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      expect(response!).toBeNull()

      // Wait for the error state to update
      await waitFor(() => {
        expect(result.current.cancelError).toBeInstanceOf(Error)
      })
    })

    it('should return null on error', async () => {
      mockCancelDeletion.mockRejectedValue(new Error('Network error'))

      const { result } = renderHook(() => useDeletion(), {
        wrapper: createWrapper(),
      })

      let response: Awaited<ReturnType<typeof result.current.cancelAccountDeletion>>
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      expect(response!).toBeNull()
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

      expect(result.current.isRequesting).toBe(false)
      expect(result.current.isCanceling).toBe(false)
      expect(result.current.requestError).toBeNull()
      expect(result.current.cancelError).toBeNull()
    })
  })
})
