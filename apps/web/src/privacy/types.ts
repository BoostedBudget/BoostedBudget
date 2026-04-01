/**
 * Cookie consent types for GDPR compliance
 */

export interface CookieConsentSettings {
  necessary: boolean;      // Always true (required for app functionality)
  analytics: boolean;      // PostHog analytics consent
  advertising: boolean;    // Advertising consent
}

export interface CookieConsentData {
  consents: CookieConsentSettings;
  timestamp: string;       // ISO 8601 timestamp
  version: string;         // Consent version for tracking changes
}

export const COOKIE_CONSENT_NAME = 'boostedbudget_cookie_consent';
export const COOKIE_CONSENT_VERSION = '2.0';
export const COOKIE_CONSENT_EXPIRES_DAYS = 365; // 1 year
