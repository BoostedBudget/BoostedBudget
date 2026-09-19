import { describe, it, expect } from 'bun:test'
import { cookieToPrivacySettings, privacySettingsToCookie } from './utils'
import type { CookieConsentSettings } from './types'
import type { PrivacySettings } from '../features/profile/types'

describe('privacy utils', () => {
  describe('cookieToPrivacySettings', () => {
    it('should convert cookie consent to privacy settings update request', () => {
      const cookie: CookieConsentSettings = {
        necessary: true,
        analytics: true,
        advertising: false,
      }

      const result = cookieToPrivacySettings(cookie)

      expect(result).toEqual({
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: true,
      })
    })

    it('should handle all consents enabled', () => {
      const cookie: CookieConsentSettings = {
        necessary: true,
        analytics: true,
        advertising: true,
      }

      const result = cookieToPrivacySettings(cookie)

      expect(result.gdpr_necessary_consent).toBe(true)
      expect(result.gdpr_analytics_consent).toBe(true)
    })

    it('should handle only necessary consent', () => {
      const cookie: CookieConsentSettings = {
        necessary: true,
        analytics: false,
        advertising: false,
      }

      const result = cookieToPrivacySettings(cookie)

      expect(result.gdpr_necessary_consent).toBe(true)
      expect(result.gdpr_analytics_consent).toBe(false)
    })
  })

  describe('privacySettingsToCookie', () => {
    it('should convert privacy settings to cookie consent', () => {
      const settings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: true,
        gdpr_consent_timestamp: '2025-01-15T10:00:00Z',
        marketing_emails_enabled: true,
        product_updates_enabled: true,
        security_alerts_enabled: true,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T10:00:00Z',
      }

      const result = privacySettingsToCookie(settings)

      expect(result).toEqual({
        necessary: true,
        analytics: true,
        advertising: false,
      })
    })

    it('should extract only GDPR consent fields', () => {
      const settings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: false,
        gdpr_consent_timestamp: '2025-01-15T10:00:00Z',
        marketing_emails_enabled: false,
        product_updates_enabled: false,
        security_alerts_enabled: false,
        data_sharing_enabled: true, // Should NOT be in cookie
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T10:00:00Z',
      }

      const result = privacySettingsToCookie(settings)

      expect(result).toEqual({
        necessary: true,
        analytics: false,
        advertising: false,
      })
      expect(result).not.toHaveProperty('marketing_emails_enabled')
      expect(result).not.toHaveProperty('data_sharing_enabled')
    })

    it('should always set advertising false (server no longer stores it)', () => {
      const settings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: true,
        gdpr_analytics_consent: true,
        gdpr_consent_timestamp: '2025-01-15T10:00:00Z',
        marketing_emails_enabled: false,
        product_updates_enabled: false,
        security_alerts_enabled: false,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T10:00:00Z',
      }

      const result = privacySettingsToCookie(settings)

      expect(result.necessary).toBe(true)
      expect(result.analytics).toBe(true)
      expect(result.advertising).toBe(false)
    })
  })

  describe('interaction capture category', () => {
    const completeSettings: PrivacySettings = {
      id: '123',
      user_id: 'user-123',
      gdpr_necessary_consent: true,
      gdpr_analytics_consent: true,
      gdpr_interaction_capture_consent: true,
      identified_diagnostics_enabled: false,
      include_in_aggregate_stats: false,
      gdpr_consent_timestamp: '2025-01-15T10:00:00Z',
      marketing_emails_enabled: false,
      product_updates_enabled: false,
      security_alerts_enabled: false,
      data_sharing_enabled: false,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-15T10:00:00Z',
    }

    it('maps the cookie interaction-capture category to the backend field', () => {
      const result = cookieToPrivacySettings({
        necessary: true,
        analytics: true,
        advertising: false,
        interactionCapture: true,
      })

      expect(result.gdpr_interaction_capture_consent).toBe(true)
    })

    it('writes interaction capture back to the cookie and keeps advertising false', () => {
      const result = privacySettingsToCookie(completeSettings)

      expect(result.interactionCapture).toBe(true)
      expect(result.advertising).toBe(false)
    })
  })

  describe('round-trip conversion', () => {
    it('should preserve GDPR values through cookie → backend → cookie conversion', () => {
      const originalCookie: CookieConsentSettings = {
        necessary: true,
        analytics: true,
        advertising: false,
      }

      // Convert to backend format
      const backendUpdate = cookieToPrivacySettings(originalCookie)

      // Simulate full backend privacy settings
      const fullBackendSettings: PrivacySettings = {
        id: '123',
        user_id: 'user-123',
        gdpr_necessary_consent: backendUpdate.gdpr_necessary_consent!,
        gdpr_analytics_consent: backendUpdate.gdpr_analytics_consent!,
        gdpr_consent_timestamp: '2025-01-15T10:00:00Z',
        marketing_emails_enabled: false,
        product_updates_enabled: true,
        security_alerts_enabled: true,
        data_sharing_enabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-15T10:00:00Z',
      }

      // Convert back to cookie format
      const finalCookie = privacySettingsToCookie(fullBackendSettings)

      // Should match original
      expect(finalCookie).toEqual(originalCookie)
    })
  })
})
