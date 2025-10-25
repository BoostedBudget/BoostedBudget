import { useState, useCallback } from 'react';
import Cookies, { CookieAttributes } from 'js-cookie';

/**
 * Custom React hook for managing cookie state
 *
 * Provides a React-friendly interface for reading and writing cookies using js-cookie.
 * Automatically serializes/deserializes JSON objects and triggers React re-renders
 * when cookie values change.
 *
 * @template T - The type of the cookie value
 * @param cookieName - Name of the cookie to manage
 * @returns Tuple of [value, setValue, removeCookie]
 *
 * @example
 * ```tsx
 * const [userData, setUserData, removeUserData] = useCookieState<UserData>('user_data');
 *
 * // Set cookie with options
 * setUserData({ name: 'John' }, {
 *   expires: 365,
 *   path: '/',
 *   sameSite: 'strict'
 * });
 *
 * // Remove cookie
 * removeUserData();
 * ```
 */
export function useCookieState<T = any>(
  cookieName: string
): [T | undefined, (value: T, options?: CookieAttributes) => void, () => void] {
  /**
   * Read and parse cookie value
   * Attempts to parse as JSON, falls back to raw string if parsing fails
   */
  const readCookie = useCallback((): T | undefined => {
    const value = Cookies.get(cookieName);
    if (value === undefined) {
      return undefined;
    }

    // Try to parse as JSON
    try {
      return JSON.parse(value) as T;
    } catch {
      // If parsing fails, return raw value
      return value as T;
    }
  }, [cookieName]);

  // Initialize state with current cookie value
  const [cookieValue, setCookieValue] = useState<T | undefined>(readCookie);

  /**
   * Update cookie value and trigger re-render
   * Automatically serializes objects to JSON
   */
  const updateCookie = useCallback(
    (value: T, options?: CookieAttributes) => {
      // Serialize value - objects become JSON, primitives become strings
      const serialized = typeof value === 'object' && value !== null
        ? JSON.stringify(value)
        : String(value);

      // Update cookie in browser
      Cookies.set(cookieName, serialized, options);

      // Update React state to trigger re-render
      setCookieValue(value);
    },
    [cookieName]
  );

  /**
   * Remove cookie and clear state
   */
  const removeCookie = useCallback(() => {
    Cookies.remove(cookieName);
    setCookieValue(undefined);
  }, [cookieName]);

  return [cookieValue, updateCookie, removeCookie];
}
