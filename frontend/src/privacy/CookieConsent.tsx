import { useState, useEffect } from 'react';
import { useCookieState } from '../shared/hooks/useCookieState';
import {
  CookieConsentSettings,
  CookieConsentData,
  COOKIE_CONSENT_NAME,
  COOKIE_CONSENT_VERSION,
  COOKIE_CONSENT_EXPIRES_DAYS,
} from './types';
import './CookieConsent.css';

interface CookieConsentProps {
  onConsentChange?: (consents: CookieConsentSettings) => void;
}

export const CookieConsent: React.FC<CookieConsentProps> = ({ onConsentChange }) => {
  const [consentData, setConsentData] = useCookieState<CookieConsentData>(COOKIE_CONSENT_NAME);
  const [showBanner, setShowBanner] = useState(false);
  const [showDetails, setShowDetails] = useState(false);
  const [customConsents, setCustomConsents] = useState<CookieConsentSettings>({
    necessary: true,
    analytics: false,
    advertising: false,
  });

  useEffect(() => {
    // Check if user has already provided consent
    if (!consentData || consentData.version !== COOKIE_CONSENT_VERSION) {
      // Show banner if no consent or version mismatch
      setShowBanner(true);
    } else {
      // Apply existing consents
      onConsentChange?.(consentData.consents);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const saveConsent = (consents: CookieConsentSettings) => {
    const newConsentData: CookieConsentData = {
      consents,
      timestamp: new Date().toISOString(),
      version: COOKIE_CONSENT_VERSION,
    };

    // Set cookie to expire in 1 year
    const expires = new Date();
    expires.setDate(expires.getDate() + COOKIE_CONSENT_EXPIRES_DAYS);

    setConsentData(newConsentData, {
      path: '/',
      expires,
      sameSite: 'strict',
      secure: window.location.protocol === 'https:',
    });

    setShowBanner(false);
    onConsentChange?.(consents);
  };

  const handleAcceptAll = () => {
    saveConsent({
      necessary: true,
      analytics: true,
      advertising: true,
    });
  };

  const handleNecessaryOnly = () => {
    saveConsent({
      necessary: true,
      analytics: false,
      advertising: false,
    });
  };

  const handleSavePreferences = () => {
    saveConsent(customConsents);
  };

  const handleToggleConsent = (key: keyof CookieConsentSettings) => {
    if (key === 'necessary') return; // Necessary cookies cannot be disabled

    setCustomConsents((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
  };

  if (!showBanner) {
    return null;
  }

  return (
    <div className="cookie-consent-overlay" role="dialog" aria-label="Cookie Consent">
      <div className="cookie-consent-banner">
        <div className="cookie-consent-header">
          <h2>🍪 Cookie Preferences</h2>
          <p className="cookie-consent-description">
            We use cookies to enhance your experience, analyze site usage, and display
            personalized ads for free tier users. You can customize your preferences below.
          </p>
        </div>

        {!showDetails ? (
          // Simple view - Quick Accept/Reject
          <div className="cookie-consent-actions">
            <button
              className="btn btn-primary"
              onClick={handleAcceptAll}
              aria-label="Accept all cookies"
            >
              Accept All
            </button>
            <button
              className="btn btn-secondary"
              onClick={handleNecessaryOnly}
              aria-label="Accept only necessary cookies"
            >
              Necessary Only
            </button>
            <button
              className="btn btn-link"
              onClick={() => setShowDetails(true)}
              aria-label="Customize cookie preferences"
            >
              Customize
            </button>
          </div>
        ) : (
          // Detailed view - Granular controls
          <div className="cookie-consent-details">
            <div className="cookie-consent-options">
              <div className="cookie-option">
                <div className="cookie-option-header">
                  <label htmlFor="consent-necessary">
                    <strong>Necessary Cookies</strong>
                    <span className="required-badge">Required</span>
                  </label>
                  <input
                    type="checkbox"
                    id="consent-necessary"
                    checked={true}
                    disabled={true}
                    aria-label="Necessary cookies (required)"
                  />
                </div>
                <p className="cookie-option-description">
                  Essential for the website to function. These cookies enable core
                  functionality such as security, authentication, and account management.
                </p>
              </div>

              <div className="cookie-option">
                <div className="cookie-option-header">
                  <label htmlFor="consent-analytics">
                    <strong>Analytics Cookies</strong>
                  </label>
                  <input
                    type="checkbox"
                    id="consent-analytics"
                    checked={customConsents.analytics}
                    onChange={() => handleToggleConsent('analytics')}
                    aria-label="Analytics cookies"
                  />
                </div>
                <p className="cookie-option-description">
                  Help us understand how visitors interact with our website by collecting
                  and reporting information anonymously via Google Analytics.
                </p>
              </div>

              <div className="cookie-option">
                <div className="cookie-option-header">
                  <label htmlFor="consent-advertising">
                    <strong>Advertising Cookies</strong>
                  </label>
                  <input
                    type="checkbox"
                    id="consent-advertising"
                    checked={customConsents.advertising}
                    onChange={() => handleToggleConsent('advertising')}
                    aria-label="Advertising cookies"
                  />
                </div>
                <p className="cookie-option-description">
                  Used to display personalized ads for free tier users via Google AdSense.
                  Premium users see no ads regardless of this setting.
                </p>
              </div>
            </div>

            <div className="cookie-consent-actions">
              <button
                className="btn btn-primary"
                onClick={handleSavePreferences}
                aria-label="Save cookie preferences"
              >
                Save Preferences
              </button>
              <button
                className="btn btn-link"
                onClick={() => setShowDetails(false)}
                aria-label="Go back to simple view"
              >
                Back
              </button>
            </div>
          </div>
        )}

        <p className="cookie-consent-footer">
          By using BoostedBudget, you agree to our use of cookies.{' '}
          <a href="/legal/privacy-policy" target="_blank" rel="noopener noreferrer">
            Privacy Policy
          </a>{' '}
          |{' '}
          <a href="/legal/cookie-policy" target="_blank" rel="noopener noreferrer">
            Cookie Policy
          </a>
        </p>
      </div>
    </div>
  );
};

export default CookieConsent;
