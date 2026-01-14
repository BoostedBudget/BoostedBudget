/**
 * Google AdSense integration with GDPR compliance
 * Only loads AdSense scripts when user has given advertising consent
 */

let adsenseInitialized = false;

export interface AdSenseConfig {
  clientId: string;  // AdSense publisher ID (ca-pub-...)
  testMode?: boolean; // For development testing
}

/**
 * Initialize Google AdSense
 * Should only be called after user has given advertising consent
 */
export const initializeAdSense = (config: AdSenseConfig): void => {
  // Prevent double initialization
  if (adsenseInitialized) {
    console.warn('[AdSense] Already initialized');
    return;
  }

  // Don't initialize in test mode if not explicitly enabled
  if (config.testMode && !import.meta.env.DEV) {
    console.warn('[AdSense] Test mode only available in development');
    return;
  }

  // Validate client ID
  if (!config.clientId || !config.clientId.startsWith('ca-pub-')) {
    console.error('[AdSense] Invalid client ID. Must start with "ca-pub-"');
    return;
  }

  console.log('[AdSense] Initializing with client ID:', config.clientId);

  // Create and inject AdSense script
  const script = document.createElement('script');
  script.src = `https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js?client=${config.clientId}`;
  script.async = true;
  script.crossOrigin = 'anonymous';

  script.onload = () => {
    console.log('[AdSense] Script loaded successfully');
    adsenseInitialized = true;
  };

  script.onerror = () => {
    console.error('[AdSense] Failed to load script');
  };

  document.head.appendChild(script);
};

/**
 * Remove AdSense scripts from the page
 * Used when user revokes advertising consent
 */
export const removeAdSense = (): void => {
  if (!adsenseInitialized) {
    return;
  }

  console.log('[AdSense] Removing AdSense scripts');

  // Remove AdSense scripts
  const scripts = document.querySelectorAll('script[src*="googlesyndication.com"]');
  scripts.forEach((script) => script.remove());

  // Remove AdSense iframes
  const iframes = document.querySelectorAll('iframe[id^="google_ads_iframe"]');
  iframes.forEach((iframe) => iframe.remove());

  // Clear AdSense global object
  if (window.adsbygoogle) {
    delete window.adsbygoogle;
  }

  adsenseInitialized = false;
};

/**
 * Check if AdSense is currently initialized
 */
export const isAdSenseInitialized = (): boolean => {
  return adsenseInitialized;
};

// Extend Window interface for TypeScript
declare global {
  interface Window {
    adsbygoogle?: unknown[];
  }
}
