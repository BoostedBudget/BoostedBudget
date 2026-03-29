/**
 * Privacy and GDPR compliance module
 */

export { CookieConsent, default as default } from './CookieConsent';
export type { CookieConsentSettings, CookieConsentData } from './types';
export {
  COOKIE_CONSENT_NAME,
  COOKIE_CONSENT_VERSION,
  COOKIE_CONSENT_EXPIRES_DAYS,
} from './types';
export { cookieToPrivacySettings, privacySettingsToCookie } from './utils';
