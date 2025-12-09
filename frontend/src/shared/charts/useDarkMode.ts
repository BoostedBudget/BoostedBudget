/**
 * Dark Mode Detection Hook
 * Issue #445: Recharts to Plotly Migration
 *
 * Detects system dark mode preference using CSS media query.
 * Automatically updates when user changes their system preference.
 */

import { useState, useEffect } from 'react';

/**
 * Hook to detect system dark mode preference
 * @returns true if dark mode is enabled, false otherwise
 */
export function useDarkMode(): boolean {
  const [isDark, setIsDark] = useState<boolean>(() => {
    // Check if we're in a browser environment
    if (typeof window === 'undefined') {
      return false;
    }
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  });

  useEffect(() => {
    // Skip effect if not in browser
    if (typeof window === 'undefined') {
      return;
    }

    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');

    // Handler for preference changes
    const handleChange = (event: MediaQueryListEvent) => {
      setIsDark(event.matches);
    };

    // Add listener for changes
    mediaQuery.addEventListener('change', handleChange);

    // Cleanup listener on unmount
    return () => {
      mediaQuery.removeEventListener('change', handleChange);
    };
  }, []);

  return isDark;
}
