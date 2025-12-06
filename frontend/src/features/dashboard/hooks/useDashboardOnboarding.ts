/**
 * useDashboardOnboarding Hook
 *
 * Detects first-time visitors to show discoverability hints.
 * Uses localStorage to persist dismissal.
 *
 * Issue #412: Dashboard widget UX improvements
 */

import { useState, useCallback } from 'react';

const STORAGE_KEY = 'dashboard-onboarding-seen';

/**
 * Check if user has seen the onboarding hint.
 * Handles SSR by checking for window availability.
 */
function getInitialShowHint(): boolean {
  if (typeof window === 'undefined') {
    return false;
  }
  const seen = localStorage.getItem(STORAGE_KEY);
  return !seen;
}

export function useDashboardOnboarding() {
  const [showHint, setShowHint] = useState(getInitialShowHint);

  const dismissHint = useCallback(() => {
    localStorage.setItem(STORAGE_KEY, 'true');
    setShowHint(false);
  }, []);

  return { showHint, dismissHint };
}
