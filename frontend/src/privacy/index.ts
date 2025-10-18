/**
 * Privacy and GDPR compliance module
 * Exports cookie consent components and AdSense integration
 */

export { CookieConsent, default as default } from './CookieConsent';
export { initializeAdSense, removeAdSense, isAdSenseInitialized } from './adsense';
export type { AdSenseConfig } from './adsense';
export type { CookieConsentSettings, CookieConsentData } from './types';
export {
  COOKIE_CONSENT_NAME,
  COOKIE_CONSENT_VERSION,
  COOKIE_CONSENT_EXPIRES_DAYS,
} from './types';
