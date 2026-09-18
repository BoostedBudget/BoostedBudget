import type { CookieConsentSettings } from './types'
import type { PrivacySettings, UpdatePrivacySettingsRequest } from '../features/profile/types'

/**
 * Convert cookie consent settings to backend privacy settings update request
 * Maps only GDPR consent fields (necessary, analytics)
 */
export function cookieToPrivacySettings(cookie: CookieConsentSettings): UpdatePrivacySettingsRequest {
  return {
    gdpr_necessary_consent: cookie.necessary,
    gdpr_analytics_consent: cookie.analytics,
  }
}

/**
 * Convert backend privacy settings to cookie consent format
 * Extracts only GDPR consent fields, ignoring email preferences and data sharing
 */
export function privacySettingsToCookie(settings: PrivacySettings): CookieConsentSettings {
  return {
    necessary: settings.gdpr_necessary_consent,
    analytics: settings.gdpr_analytics_consent,
    advertising: false,
  }
}
