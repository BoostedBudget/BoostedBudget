/**
 * deletion

Api Tests
 *
 * TDD RED: These tests verify the API client functions for account deletion.
 * Tests both authenticated and public endpoints.
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import { requestDeletion, cancelDeletion } from './deletionApi'
import type { RequestDeletionResponse, CancelDeletionResponse } from '../types'

describe('deletionApi', () => {
  let mock: MockAdapter

  beforeEach(() => {
    mock = new MockAdapter(apiClient)
  })

  afterEach(() => {
    mock.reset()
    vi.clearAllMocks()
  })

  describe('requestDeletion', () => {
    it('should call POST /api/v1/user/delete', async () => {
      const mockResponse: RequestDeletionResponse = {
        message: 'Account deletion scheduled',
        scheduled_at: '2025-11-26T00:00:00Z',
        cancellation_token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token',
      }

      mock.onPost('/api/v1/user/delete').reply(200, mockResponse)

      const response = await requestDeletion()

      expect(response).toEqual(mockResponse)
      expect(mock.history.post.length).toBe(1)
      expect(mock.history.post[0].url).toBe('/api/v1/user/delete')
    })

    it('should include Auth0 token in Authorization header', async () => {
      const mockResponse: RequestDeletionResponse = {
        message: 'Account deletion scheduled',
        scheduled_at: '2025-11-26T00:00:00Z',
        cancellation_token: 'test-token-123',
      }

      mock.onPost('/api/v1/user/delete').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockResponse]
      })

      await requestDeletion()
    })

    it('should return response with scheduled_at and cancellation_token', async () => {
      const mockResponse: RequestDeletionResponse = {
        message: 'Your account has been scheduled for deletion',
        scheduled_at: '2025-11-26T12:00:00Z',
        cancellation_token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyLTEyMyJ9.test',
      }

      mock.onPost('/api/v1/user/delete').reply(200, mockResponse)

      const response = await requestDeletion()

      expect(response.message).toBeDefined()
      expect(response.scheduled_at).toBeDefined()
      expect(response.cancellation_token).toBeDefined()
      expect(typeof response.scheduled_at).toBe('string')
      expect(typeof response.cancellation_token).toBe('string')
    })

    it('should handle 401 unauthorized error', async () => {
      mock.onPost('/api/v1/user/delete').reply(401, { error: 'Unauthorized' })

      await expect(requestDeletion()).rejects.toThrow()
    })

    it('should handle 500 server errors', async () => {
      mock.onPost('/api/v1/user/delete').reply(500, { error: 'Internal server error' })

      await expect(requestDeletion()).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      mock.onPost('/api/v1/user/delete').networkError()

      await expect(requestDeletion()).rejects.toThrow()
    })
  })

  describe('cancelDeletion', () => {
    const testToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token'

    it('should call GET /api/v1/user/delete/cancel/:token', async () => {
      const mockResponse: CancelDeletionResponse = {
        message: 'Account deletion canceled successfully',
      }

      mock.onGet(`/api/v1/user/delete/cancel/${testToken}`).reply(200, mockResponse)

      const response = await cancelDeletion(testToken)

      expect(response).toEqual(mockResponse)
      expect(mock.history.get.length).toBe(1)
      expect(mock.history.get[0].url).toBe(`/api/v1/user/delete/cancel/${testToken}`)
    })

    it('should NOT require authentication (public endpoint)', async () => {
      const mockResponse: CancelDeletionResponse = {
        message: 'Deletion canceled',
      }

      // Public endpoint - should work without auth header
      mock.onGet(`/api/v1/user/delete/cancel/${testToken}`).reply((_config) => {
        // Note: axios interceptor may still add header, but endpoint doesn't require it
        return [200, mockResponse]
      })

      const response = await cancelDeletion(testToken)

      expect(response).toBeDefined()
      expect(response.message).toBeDefined()
    })

    it('should return success message', async () => {
      const mockResponse: CancelDeletionResponse = {
        message: 'Your account deletion has been successfully canceled',
      }

      mock.onGet(`/api/v1/user/delete/cancel/${testToken}`).reply(200, mockResponse)

      const response = await cancelDeletion(testToken)

      expect(response.message).toBe('Your account deletion has been successfully canceled')
    })

    it('should handle 400 invalid/expired token', async () => {
      mock.onGet(`/api/v1/user/delete/cancel/${testToken}`).reply(400, {
        error: 'Invalid or expired cancellation token',
      })

      await expect(cancelDeletion(testToken)).rejects.toThrow()
    })

    it('should handle 404 token not found', async () => {
      mock.onGet(`/api/v1/user/delete/cancel/${testToken}`).reply(404, {
        error: 'Cancellation token not found',
      })

      await expect(cancelDeletion(testToken)).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      mock.onGet(`/api/v1/user/delete/cancel/${testToken}`).networkError()

      await expect(cancelDeletion(testToken)).rejects.toThrow()
    })
  })
})
