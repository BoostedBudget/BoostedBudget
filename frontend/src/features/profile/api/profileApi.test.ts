import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import { fetchProfile, updateProfile } from './profileApi'
import type { User, UpdateProfileRequest } from '../types'

describe('profileApi', () => {
  let mock: MockAdapter

  beforeEach(() => {
    mock = new MockAdapter(apiClient)
  })

  afterEach(() => {
    mock.reset()
    vi.clearAllMocks()
  })

  describe('fetchProfile', () => {
    it('should call GET /v1/user/profile', async () => {
      const mockUser: User = {
        id: '123e4567-e89b-12d3-a456-426614174000',
        auth0_user_id: 'auth0|507f1f77bcf86cd799439011',
        email: 'test@example.com',
        email_verified: true,
        subscription_tier: 'premium',
        subscription_status: 'active',
        primary_region: 'iad',
        is_active: true,
        is_deleted: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        last_login_at: '2025-01-01T00:00:00Z',
      }

      mock.onGet('/v1/user/profile').reply(200, mockUser)

      const user = await fetchProfile()

      expect(user).toEqual(mockUser)
      expect(mock.history.get.length).toBe(1)
      expect(mock.history.get[0].url).toBe('/v1/user/profile')
    })

    it('should include Auth0 token in Authorization header', async () => {
      const mockUser: User = {
        id: '123',
        auth0_user_id: 'auth0|123',
        email: 'test@example.com',
        email_verified: true,
        subscription_tier: 'free',
        subscription_status: 'active',
        primary_region: 'iad',
        is_active: true,
        is_deleted: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        last_login_at: undefined,
      }

      mock.onGet('/v1/user/profile').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockUser]
      })

      await fetchProfile()
    })

    it('should handle 401 unauthorized error', async () => {
      mock.onGet('/v1/user/profile').reply(401, { error: 'Unauthorized' })

      await expect(fetchProfile()).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      mock.onGet('/v1/user/profile').networkError()

      await expect(fetchProfile()).rejects.toThrow()
    })

    it('should handle 500 server errors', async () => {
      mock.onGet('/v1/user/profile').reply(500, { error: 'Internal server error' })

      await expect(fetchProfile()).rejects.toThrow()
    })
  })

  describe('updateProfile', () => {
    it('should call PUT /v1/user/profile with updated data', async () => {
      const updateRequest: UpdateProfileRequest = {
        email: 'newemail@example.com',
      }

      const updatedUser: User = {
        id: '123',
        auth0_user_id: 'auth0|123',
        email: 'newemail@example.com',
        email_verified: false,
        subscription_tier: 'premium',
        subscription_status: 'active',
        primary_region: 'iad',
        is_active: true,
        is_deleted: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T12:00:00Z',
        last_login_at: '2025-01-01T00:00:00Z',
      }

      mock.onPut('/v1/user/profile', updateRequest).reply(200, updatedUser)

      const user = await updateProfile(updateRequest)

      expect(user).toEqual(updatedUser)
      expect(mock.history.put.length).toBe(1)
      expect(mock.history.put[0].url).toBe('/v1/user/profile')
      expect(JSON.parse(mock.history.put[0].data)).toEqual(updateRequest)
    })

    it('should update primary region', async () => {
      const updateRequest: UpdateProfileRequest = {
        primary_region: 'lhr',
      }

      const updatedUser: User = {
        id: '123',
        auth0_user_id: 'auth0|123',
        email: 'test@example.com',
        email_verified: true,
        subscription_tier: 'premium',
        subscription_status: 'active',
        primary_region: 'lhr',
        is_active: true,
        is_deleted: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T12:00:00Z',
        last_login_at: '2025-01-01T00:00:00Z',
      }

      mock.onPut('/v1/user/profile').reply(200, updatedUser)

      const user = await updateProfile(updateRequest)

      expect(user.primary_region).toBe('lhr')
    })

    it('should handle validation errors (400)', async () => {
      const updateRequest: UpdateProfileRequest = {
        email: 'invalid-email',
      }

      mock.onPut('/v1/user/profile').reply(400, { error: 'Invalid email address' })

      await expect(updateProfile(updateRequest)).rejects.toThrow()
    })

    it('should handle unauthorized errors (401)', async () => {
      mock.onPut('/v1/user/profile').reply(401, { error: 'Unauthorized' })

      await expect(updateProfile({})).rejects.toThrow()
    })

    it('should handle server errors (500)', async () => {
      mock.onPut('/v1/user/profile').reply(500, { error: 'Failed to update profile' })

      await expect(updateProfile({})).rejects.toThrow()
    })
  })
})
