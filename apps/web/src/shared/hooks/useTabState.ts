import { useSearchParams } from 'react-router-dom'

/**
 * Custom hook for managing tab state via URL query parameters
 *
 * Provides persistent tab state that:
 * - Survives page refreshes
 * - Enables shareable/bookmarkable URLs
 * - Supports browser back/forward navigation
 * - Type-safe with TypeScript generics
 *
 * @template T - The string literal union type for valid tab values
 * @param queryParamKey - The URL query parameter key (e.g., 'tab', 'view')
 * @param defaultTab - The default tab value when no URL param is present
 * @param validTabs - Optional array of valid tab values for runtime validation
 * @returns A tuple [currentTab, setTab] matching React.useState API
 *
 * @example
 * ```tsx
 * // Type-safe tab state with validation
 * const [activeTab, setActiveTab] = useTabState(
 *   'tab',
 *   'profile',
 *   ['profile', 'privacy', 'settings'] as const
 * )
 *
 * // URL will be /page?tab=privacy
 * setActiveTab('privacy')
 * ```
 *
 * @example
 * ```tsx
 * // Simple tab state without validation
 * const [view, setView] = useTabState('view', 'grid')
 * setView('list') // URL will be /page?view=list
 * ```
 */
export function useTabState<T extends string>(
  queryParamKey: string,
  defaultTab: T,
  validTabs?: readonly T[]
): [T, (tab: T) => void] {
  const [searchParams, setSearchParams] = useSearchParams()

  // Read tab from URL query param
  const tabFromUrl = searchParams.get(queryParamKey)

  // Determine current tab value
  let currentTab: T = defaultTab

  if (tabFromUrl && tabFromUrl !== '') {
    if (validTabs) {
      // Validate against allowed tabs if provided
      if (validTabs.includes(tabFromUrl as T)) {
        currentTab = tabFromUrl as T
      }
      // else: invalid tab, use default
    } else {
      // No validation, accept any value from URL
      currentTab = tabFromUrl as T
    }
  }
  // else: no URL param or empty string, use default

  /**
   * Update tab state and sync with URL query parameter
   * Preserves other query parameters
   */
  const setTab = (newTab: T) => {
    setSearchParams(
      (prevParams) => {
        // Create new URLSearchParams to preserve other params
        const newParams = new URLSearchParams(prevParams)
        newParams.set(queryParamKey, newTab)
        return newParams
      },
      { replace: false } // Don't replace history, allow back/forward navigation
    )
  }

  return [currentTab, setTab]
}
