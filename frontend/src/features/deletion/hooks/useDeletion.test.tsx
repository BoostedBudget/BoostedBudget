/**
 * useDeletion Hook Tests
 *
 * TDD RED: These tests verify the React hook for account deletion operations.
 * Tests both requestAccountDeletion and cancelAccountDeletion flows.
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useDeletion } from './useDeletion'
import * as deletionApi from '../api/deletionApi'
import type { RequestDeletionResponse, CancelDeletionResponse } from '../types'

// Mock the API module
vi.mock('../api/deletionApi')

describe('useDeletion', () => {
  const mockRequestResponse: RequestDeletionResponse = {
    message: 'Account deletion scheduled',
    scheduled_at: '2025-11-26T00:00:00Z',
    cancellation_token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token',
  }

  const mockCancelResponse: CancelDeletionResponse = {
    message: 'Account deletion canceled successfully',
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Request Deletion Tests', () => {
    it('should return initial state', () => {
      // Act
      const { result } = renderHook(() => useDeletion())

      // Assert
      expect(result.current.isRequesting).toBe(false)
      expect(result.current.requestError).toBeNull()
      expect(result.current.isCanceling).toBe(false)
      expect(result.current.cancelError).toBeNull()
    })

    it('should successfully request deletion and return response', async () => {
      // Arrange
      vi.mocked(deletionApi.requestDeletion).mockResolvedValue(mockRequestResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      let response: RequestDeletionResponse | null = null
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      // Assert
      expect(response).toEqual(mockRequestResponse)
      expect(result.current.requestError).toBeNull()
      expect(deletionApi.requestDeletion).toHaveBeenCalledTimes(1)
    })

    it('should set isRequesting=true during request', async () => {
      // Arrange - Use longer delay to ensure we can catch the loading state
      vi.mocked(deletionApi.requestDeletion).mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(mockRequestResponse), 500))
      )

      // Act
      const { result } = renderHook(() => useDeletion())

      // Start the request (don't await)
      act(() => {
        result.current.requestAccountDeletion()
      })

      // Assert - Should be requesting immediately after act()
      // Use waitFor with short timeout to handle CI timing variance
      await waitFor(() => {
        expect(result.current.isRequesting).toBe(true)
      }, { timeout: 200 })

      // Wait for completion (increased timeout for CI stability)
      await waitFor(() => {
        expect(result.current.isRequesting).toBe(false)
      }, { timeout: 2000 })
    }, 15000)

    it('should set isRequesting=false after completion', async () => {
      // Arrange
      vi.mocked(deletionApi.requestDeletion).mockResolvedValue(mockRequestResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      // Assert
      expect(result.current.isRequesting).toBe(false)
    })

    it('should handle errors and set requestError', async () => {
      // Arrange
      const error = new Error('Failed to request account deletion')
      vi.mocked(deletionApi.requestDeletion).mockRejectedValue(error)

      // Act
      const { result } = renderHook(() => useDeletion())

      let response: RequestDeletionResponse | null = null
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      // Assert
      expect(response).toBeNull()
      expect(result.current.requestError).toEqual(error)
      expect(result.current.isRequesting).toBe(false)
    })

    it('should return null on error', async () => {
      // Arrange
      vi.mocked(deletionApi.requestDeletion).mockRejectedValue(new Error('Server error'))

      // Act
      const { result } = renderHook(() => useDeletion())

      let response: RequestDeletionResponse | null = null
      await act(async () => {
        response = await result.current.requestAccountDeletion()
      })

      // Assert
      expect(response).toBeNull()
    })

    it('should clear previous errors on new request', async () => {
      // Arrange
      const error = new Error('First error')
      vi.mocked(deletionApi.requestDeletion).mockRejectedValueOnce(error)
        .mockResolvedValueOnce(mockRequestResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      // First request (fails)
      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      expect(result.current.requestError).toEqual(error)

      // Second request (succeeds)
      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      // Assert - Error should be cleared
      expect(result.current.requestError).toBeNull()
    })

    it('should handle multiple consecutive requests', async () => {
      // Arrange
      vi.mocked(deletionApi.requestDeletion).mockResolvedValue(mockRequestResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      // Assert
      expect(deletionApi.requestDeletion).toHaveBeenCalledTimes(2)
      expect(result.current.requestError).toBeNull()
    })

    it('should handle non-Error thrown values', async () => {
      // Arrange
      vi.mocked(deletionApi.requestDeletion).mockRejectedValue('String error')

      // Act
      const { result } = renderHook(() => useDeletion())

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      // Assert
      expect(result.current.requestError).toBeInstanceOf(Error)
      expect(result.current.requestError?.message).toBe('Failed to request account deletion')
    })
  })

  describe('Cancel Deletion Tests', () => {
    const testToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token'

    it('should successfully cancel deletion with valid token', async () => {
      // Arrange
      vi.mocked(deletionApi.cancelDeletion).mockResolvedValue(mockCancelResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      let response: CancelDeletionResponse | null = null
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      // Assert
      expect(response).toEqual(mockCancelResponse)
      expect(result.current.cancelError).toBeNull()
      expect(deletionApi.cancelDeletion).toHaveBeenCalledWith(testToken)
    })

    it('should set isCanceling=true during cancel', async () => {
      // Arrange
      vi.mocked(deletionApi.cancelDeletion).mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(mockCancelResponse), 100))
      )

      // Act
      const { result } = renderHook(() => useDeletion())

      act(() => {
        result.current.cancelAccountDeletion(testToken)
      })

      // Assert - Should be canceling immediately
      expect(result.current.isCanceling).toBe(true)

      // Wait for completion (increased timeout for CI stability)
      await waitFor(() => {
        expect(result.current.isCanceling).toBe(false)
      }, { timeout: 2000 })
    })

    it('should set isCanceling=false after completion', async () => {
      // Arrange
      vi.mocked(deletionApi.cancelDeletion).mockResolvedValue(mockCancelResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      await act(async () => {
        await result.current.cancelAccountDeletion(testToken)
      })

      // Assert
      expect(result.current.isCanceling).toBe(false)
    })

    it('should handle invalid token errors', async () => {
      // Arrange
      const error = new Error('Invalid or expired token')
      vi.mocked(deletionApi.cancelDeletion).mockRejectedValue(error)

      // Act
      const { result } = renderHook(() => useDeletion())

      let response: CancelDeletionResponse | null = null
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      // Assert
      expect(response).toBeNull()
      expect(result.current.cancelError).toEqual(error)
    })

    it('should handle expired token errors', async () => {
      // Arrange
      const error = new Error('Token has expired')
      vi.mocked(deletionApi.cancelDeletion).mockRejectedValue(error)

      // Act
      const { result } = renderHook(() => useDeletion())

      await act(async () => {
        await result.current.cancelAccountDeletion(testToken)
      })

      // Assert
      expect(result.current.cancelError).toEqual(error)
      expect(result.current.isCanceling).toBe(false)
    })

    it('should return null on error', async () => {
      // Arrange
      vi.mocked(deletionApi.cancelDeletion).mockRejectedValue(new Error('Network error'))

      // Act
      const { result } = renderHook(() => useDeletion())

      let response: CancelDeletionResponse | null = null
      await act(async () => {
        response = await result.current.cancelAccountDeletion(testToken)
      })

      // Assert
      expect(response).toBeNull()
    })

    it('should clear previous errors on new cancel', async () => {
      // Arrange
      const error = new Error('First error')
      vi.mocked(deletionApi.cancelDeletion).mockRejectedValueOnce(error)
        .mockResolvedValueOnce(mockCancelResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      // First cancel (fails)
      await act(async () => {
        await result.current.cancelAccountDeletion(testToken)
      })

      expect(result.current.cancelError).toEqual(error)

      // Second cancel (succeeds)
      await act(async () => {
        await result.current.cancelAccountDeletion(testToken)
      })

      // Assert - Error should be cleared
      expect(result.current.cancelError).toBeNull()
    })

    it('should handle non-Error thrown values', async () => {
      // Arrange
      vi.mocked(deletionApi.cancelDeletion).mockRejectedValue('String error')

      // Act
      const { result } = renderHook(() => useDeletion())

      await act(async () => {
        await result.current.cancelAccountDeletion(testToken)
      })

      // Assert
      expect(result.current.cancelError).toBeInstanceOf(Error)
      expect(result.current.cancelError?.message).toBe('Failed to cancel account deletion')
    })
  })

  describe('State Management Tests', () => {
    it('should maintain separate state for request and cancel', async () => {
      // Arrange
      vi.mocked(deletionApi.requestDeletion).mockResolvedValue(mockRequestResponse)
      vi.mocked(deletionApi.cancelDeletion).mockResolvedValue(mockCancelResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      // Assert - Request completed, cancel not affected
      expect(result.current.isRequesting).toBe(false)
      expect(result.current.isCanceling).toBe(false)
      expect(result.current.requestError).toBeNull()
      expect(result.current.cancelError).toBeNull()
    })

    it('should allow simultaneous request and cancel states', async () => {
      // Arrange - Both operations take time
      vi.mocked(deletionApi.requestDeletion).mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(mockRequestResponse), 200))
      )
      vi.mocked(deletionApi.cancelDeletion).mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(mockCancelResponse), 200))
      )

      // Act
      const { result } = renderHook(() => useDeletion())

      // Start both operations
      act(() => {
        result.current.requestAccountDeletion()
        result.current.cancelAccountDeletion('token')
      })

      // Assert - Both should be in progress simultaneously
      expect(result.current.isRequesting).toBe(true)
      expect(result.current.isCanceling).toBe(true)

      // Wait for both to complete (increased timeout for CI stability)
      await waitFor(() => {
        expect(result.current.isRequesting).toBe(false)
        expect(result.current.isCanceling).toBe(false)
      }, { timeout: 2000 })
    })

    it('should reset errors independently', async () => {
      // Arrange
      const requestError = new Error('Request error')
      const cancelError = new Error('Cancel error')

      vi.mocked(deletionApi.requestDeletion)
        .mockRejectedValueOnce(requestError)
        .mockResolvedValueOnce(mockRequestResponse)

      vi.mocked(deletionApi.cancelDeletion)
        .mockRejectedValueOnce(cancelError)
        .mockResolvedValueOnce(mockCancelResponse)

      // Act
      const { result } = renderHook(() => useDeletion())

      // Both fail
      await act(async () => {
        await result.current.requestAccountDeletion()
        await result.current.cancelAccountDeletion('token')
      })

      expect(result.current.requestError).toEqual(requestError)
      expect(result.current.cancelError).toEqual(cancelError)

      // Request succeeds, cancel error should remain
      await act(async () => {
        await result.current.requestAccountDeletion()
      })

      expect(result.current.requestError).toBeNull()
      expect(result.current.cancelError).toEqual(cancelError) // Still set

      // Cancel succeeds
      await act(async () => {
        await result.current.cancelAccountDeletion('token')
      })

      expect(result.current.cancelError).toBeNull()
    })
  })
})
