import { useState, useEffect } from 'react';
import { useCookieState } from '../shared/hooks/useCookieState';
import {
  CookieConsentSettings,
  CookieConsentData,
  COOKIE_CONSENT_NAME,
  COOKIE_CONSENT_VERSION,
  COOKIE_CONSENT_EXPIRES_DAYS,
} from './types';

interface CookieConsentProps {
  onConsentChange?: (consents: CookieConsentSettings) => void;
}

const DEFAULT_CONSENTS: CookieConsentSettings = {
  necessary: true,
  analytics: false,
  advertising: false,
};

const calculateExpiryDate = (): Date => {
  const expires = new Date();
  expires.setDate(expires.getDate() + COOKIE_CONSENT_EXPIRES_DAYS);
  return expires;
};

export const CookieConsent: React.FC<CookieConsentProps> = ({ onConsentChange }) => {
  const [consentData, setConsentData] = useCookieState<CookieConsentData>(COOKIE_CONSENT_NAME);
  const [showBanner, setShowBanner] = useState(false);
  const [showDetails, setShowDetails] = useState(false);
  const [customConsents, setCustomConsents] = useState<CookieConsentSettings>(DEFAULT_CONSENTS);

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

    setConsentData(newConsentData, {
      path: '/',
      expires: calculateExpiryDate(),
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
      advertising: false,
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

  const btnPrimary = 'px-6 py-3 rounded-md text-base font-medium cursor-pointer border-none bg-primary text-primary-foreground hover:bg-primary/90 transition-all focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2'
  const btnSecondary = 'px-6 py-3 rounded-md text-base font-medium cursor-pointer border-none bg-muted text-foreground hover:bg-accent transition-all focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2'
  const btnLink = 'px-4 py-3 rounded-md text-base font-medium cursor-pointer border-none bg-transparent text-primary underline hover:no-underline focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2'

  return (
    <div
      className="cookie-consent-banner fixed bottom-0 left-0 right-0 z-[9999] bg-black/50 backdrop-blur-sm animate-in fade-in"
      role="dialog"
      aria-label="Cookie Consent"
    >
      <div className="bg-card border-t-[3px] border-primary shadow-[0_-4px_12px_rgba(0,0,0,0.15)] p-6 max-w-[800px] mx-auto animate-in slide-in-from-bottom">
        <div>
          <h2 className="m-0 mb-2 text-2xl text-foreground flex items-center gap-2">
            🍪 Cookie Preferences
          </h2>
          <p className="m-0 mb-6 text-muted-foreground leading-relaxed text-[0.95rem]">
            We use cookies to enhance your experience and analyze site usage.
            You can customize your preferences below.
          </p>
        </div>

        {!showDetails ? (
          // Simple view - Quick Accept/Reject
          <div className="flex gap-4 flex-wrap justify-center my-6 mb-4 max-sm:flex-col">
            <button
              className={btnPrimary}
              onClick={handleAcceptAll}
              aria-label="Accept all cookies"
            >
              Accept All
            </button>
            <button
              className={btnSecondary}
              onClick={handleNecessaryOnly}
              aria-label="Accept only necessary cookies"
            >
              Necessary Only
            </button>
            <button
              className={btnLink}
              onClick={() => setShowDetails(true)}
              aria-label="Customize cookie preferences"
            >
              Customize
            </button>
          </div>
        ) : (
          // Detailed view - Granular controls
          <div className="mt-4">
            <div className="my-4 flex flex-col gap-6">
              <div className="p-4 bg-muted rounded-lg border border-border">
                <div className="flex justify-between items-center mb-2 max-sm:flex-col max-sm:items-start max-sm:gap-2">
                  <label htmlFor="consent-necessary" className="flex items-center gap-2 cursor-pointer flex-1">
                    <strong className="text-foreground">Necessary Cookies</strong>
                    <span className="bg-success text-success-foreground px-2 py-1 rounded text-xs font-bold uppercase">Required</span>
                  </label>
                  <input
                    type="checkbox"
                    id="consent-necessary"
                    checked={true}
                    disabled={true}
                    className="w-5 h-5 cursor-not-allowed opacity-60 accent-primary"
                    aria-label="Necessary cookies (required)"
                  />
                </div>
                <p className="m-0 text-sm text-muted-foreground leading-relaxed">
                  Essential for the website to function. These cookies enable core
                  functionality such as security, authentication, and account management.
                </p>
              </div>

              <div className="p-4 bg-muted rounded-lg border border-border">
                <div className="flex justify-between items-center mb-2 max-sm:flex-col max-sm:items-start max-sm:gap-2">
                  <label htmlFor="consent-analytics" className="flex items-center gap-2 cursor-pointer flex-1">
                    <strong className="text-foreground">Analytics Cookies</strong>
                  </label>
                  <input
                    type="checkbox"
                    id="consent-analytics"
                    checked={customConsents.analytics}
                    onChange={() => handleToggleConsent('analytics')}
                    className="w-5 h-5 cursor-pointer accent-primary"
                    aria-label="Analytics cookies"
                  />
                </div>
                <p className="m-0 text-sm text-muted-foreground leading-relaxed">
                  Help us understand how visitors interact with our website by collecting
                  and reporting information anonymously via PostHog.
                </p>
              </div>

            </div>

            <div className="flex gap-4 flex-wrap justify-center my-6 mb-4 max-sm:flex-col">
              <button
                className={btnPrimary}
                onClick={handleSavePreferences}
                aria-label="Save cookie preferences"
              >
                Save Preferences
              </button>
              <button
                className={btnLink}
                onClick={() => setShowDetails(false)}
                aria-label="Go back to simple view"
              >
                Back
              </button>
            </div>
          </div>
        )}

        <p className="mt-4 mb-0 text-[0.85rem] text-muted-foreground text-center pt-4 border-t border-border">
          By using BoostedBudget, you agree to our use of cookies.{' '}
          <a href="/legal/privacy-policy" target="_blank" rel="noopener noreferrer" className="text-primary underline hover:no-underline">
            Privacy Policy
          </a>{' '}
          |{' '}
          <a href="/legal/cookie-policy" target="_blank" rel="noopener noreferrer" className="text-primary underline hover:no-underline">
            Cookie Policy
          </a>
        </p>
      </div>
    </div>
  );
};

export default CookieConsent;
