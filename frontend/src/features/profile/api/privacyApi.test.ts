import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import { fetchPrivacySettings, updatePrivacySettings } from './privacyApi'
import type { PrivacySettings, UpdatePrivacySettingsRequest } from '../types'

describe('privacyApi', () => {
  let mock: MockAdapter

  beforeEach(() => {
    mock = new MockAdapter(apiClient)
  })

  afterEach(() => {
    mock.reset()
    vi.clearAllMocks()
  })

  describe('fetchPrivacySettings', () => {
    it('should call GET /v1/user/privacy', async () => {
      const mockSettings: PrivacySettings = {
        id: '123e4567-e89b-12d3-a456-426614174000',
        user_id: 'user-uuid-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: true,
        gdpr_advertising_consent: false,
        gdpr_consent_timestamp: '2025-01-15T10:00:00Z',
        marketing_emails_enabled: false,
        product_updates_enabled: true,
        security_alerts_enabled: true,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T10:00:00Z',
      }

      mock.onGet('/v1/user/privacy').reply(200, mockSettings)

      const settings = await fetchPrivacySettings()

      expect(settings).toEqual(mockSettings)
      expect(mock.history.get.length).toBe(1)
      expect(mock.history.get[0].url).toBe('/v1/user/privacy')
    })

    it('should include Auth0 token in Authorization header', async () => {
      const mockSettings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: false,
        gdpr_advertising_consent: false,
        gdpr_consent_timestamp: '2025-01-01T00:00:00Z',
        marketing_emails_enabled: false,
        product_updates_enabled: true,
        security_alerts_enabled: true,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      }

      mock.onGet('/v1/user/privacy').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockSettings]
      })

      await fetchPrivacySettings()
    })

    it('should handle 401 unauthorized error', async () => {
      mock.onGet('/v1/user/privacy').reply(401, { error: 'Unauthorized' })

      await expect(fetchPrivacySettings()).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      mock.onGet('/v1/user/privacy').networkError()

      await expect(fetchPrivacySettings()).rejects.toThrow()
    })

    it('should handle 500 server errors', async () => {
      mock.onGet('/v1/user/privacy').reply(500, { error: 'Internal server error' })

      await expect(fetchPrivacySettings()).rejects.toThrow()
    })
  })

  describe('updatePrivacySettings', () => {
    it('should call PUT /v1/user/privacy with updated data', async () => {
      const updateRequest: UpdatePrivacySettingsRequest = {
        gdpr_analytics_consent: true,
        marketing_emails_enabled: true,
      }

      const updatedSettings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: true,
        gdpr_advertising_consent: false,
        gdpr_consent_timestamp: '2025-01-15T12:00:00Z',
        marketing_emails_enabled: true,
        product_updates_enabled: true,
        security_alerts_enabled: true,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T12:00:00Z',
      }

      mock.onPut('/v1/user/privacy', updateRequest).reply(200, updatedSettings)

      const settings = await updatePrivacySettings(updateRequest)

      expect(settings).toEqual(updatedSettings)
      expect(mock.history.put.length).toBe(1)
      expect(mock.history.put[0].url).toBe('/v1/user/privacy')
      expect(JSON.parse(mock.history.put[0].data)).toEqual(updateRequest)
    })

    it('should update only analytics consent', async () => {
      const updateRequest: UpdatePrivacySettingsRequest = {
        gdpr_analytics_consent: true,
      }

      const updatedSettings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: true,
        gdpr_advertising_consent: false,
        gdpr_consent_timestamp: '2025-01-15T12:00:00Z',
        marketing_emails_enabled: false,
        product_updates_enabled: true,
        security_alerts_enabled: true,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T12:00:00Z',
      }

      mock.onPut('/v1/user/privacy').reply(200, updatedSettings)

      const settings = await updatePrivacySettings(updateRequest)

      expect(settings.gdpr_analytics_consent).toBe(true)
    })

    it('should update advertising consent', async () => {
      const updateRequest: UpdatePrivacySettingsRequest = {
        gdpr_advertising_consent: true,
      }

      const updatedSettings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: false,
        gdpr_advertising_consent: true,
        gdpr_consent_timestamp: '2025-01-15T12:00:00Z',
        marketing_emails_enabled: false,
        product_updates_enabled: true,
        security_alerts_enabled: true,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T12:00:00Z',
      }

      mock.onPut('/v1/user/privacy').reply(200, updatedSettings)

      const settings = await updatePrivacySettings(updateRequest)

      expect(settings.gdpr_advertising_consent).toBe(true)
    })

    it('should handle GDPR validation errors (400)', async () => {
      const updateRequest: UpdatePrivacySettingsRequest = {
        gdpr_necessary_consent: false, // Attempt to disable - should fail
      }

      mock.onPut('/v1/user/privacy').reply(400, { error: 'gdpr_necessary_consent cannot be disabled - required for app functionality' })

      await expect(updatePrivacySettings(updateRequest)).rejects.toThrow()
    })

    it('should handle unauthorized errors (401)', async () => {
      mock.onPut('/v1/user/privacy').reply(401, { error: 'Unauthorized' })

      await expect(updatePrivacySettings({})).rejects.toThrow()
    })

    it('should handle server errors (500)', async () => {
      mock.onPut('/v1/user/privacy').reply(500, { error: 'Failed to update privacy settings' })

      await expect(updatePrivacySettings({})).rejects.toThrow()
    })

    it('should update email preferences', async () => {
      const updateRequest: UpdatePrivacySettingsRequest = {
        marketing_emails_enabled: true,
        product_updates_enabled: false,
      }

      const updatedSettings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: false,
        gdpr_advertising_consent: false,
        gdpr_consent_timestamp: '2025-01-15T12:00:00Z',
        marketing_emails_enabled: true,
        product_updates_enabled: false,
        security_alerts_enabled: true,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T12:00:00Z',
      }

      mock.onPut('/v1/user/privacy').reply(200, updatedSettings)

      const settings = await updatePrivacySettings(updateRequest)

      expect(settings.marketing_emails_enabled).toBe(true)
      expect(settings.product_updates_enabled).toBe(false)
    })
  })
})
