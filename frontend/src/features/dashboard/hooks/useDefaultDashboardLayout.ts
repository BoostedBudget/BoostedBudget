/**
 * useDefaultDashboardLayout Hook
 *
 * React hook for fetching default dashboard layout configuration with 1hr caching.
 * Uses in-memory cache (for single session) and localStorage (for cache persistence).
 *
 * Issue #449: Centralize default dashboard layout configuration
 * Issue #472: Added isFromApi flag to prevent resets when using fallback
 *
 * The default layout is fetched from the backend API which loads it from
 * the centralized YAML config file (config/default_dashboard_layout.yaml).
 * This ensures both frontend and backend use the same source of truth.
 */

import { useState, useEffect, useCallback } from 'react'
import { fetchDefaultDashboardLayout } from '../api/dashboardApi'
import { DEFAULT_LAYOUT } from '../widgets/registry'
import type { DashboardLayout } from '../types'

// Constants
const CACHE_KEY_TIMESTAMP = 'default_dashboard_layout_cache_timestamp'
const CACHE_KEY_DATA = 'default_dashboard_layout_cache_data'
const CACHE_TTL_MS = 60 * 60 * 1000 // 1 hour in milliseconds

// Module-level in-memory cache
let memoryCache: DashboardLayout | null = null

/**
 * Check if cached data is still valid based on timestamp
 */
function isCacheValid(): boolean {
  const cachedTimestamp = localStorage.getItem(CACHE_KEY_TIMESTAMP)
  if (!cachedTimestamp) {
    return false
  }

  const timestamp = Number(cachedTimestamp)
  const now = Date.now()
  const age = now - timestamp

  return age < CACHE_TTL_MS
}

/**
 * Update cache with fresh data
 */
function updateCache(data: DashboardLayout): void {
  memoryCache = data
  localStorage.setItem(CACHE_KEY_TIMESTAMP, String(Date.now()))
  localStorage.setItem(CACHE_KEY_DATA, JSON.stringify(data))
}

/**
 * Load data from localStorage cache
 */
function loadFromLocalStorage(): DashboardLayout | null {
  try {
    const cachedData = localStorage.getItem(CACHE_KEY_DATA)
    if (cachedData && isCacheValid()) {
      return JSON.parse(cachedData)
    }
  } catch {
    // Invalid JSON, ignore and fetch fresh
  }
  return null
}

/**
 * Clear the cache
 * @internal Exported for testing purposes
 */
export function clearDefaultLayoutCache(): void {
  memoryCache = null
  localStorage.removeItem(CACHE_KEY_TIMESTAMP)
  localStorage.removeItem(CACHE_KEY_DATA)
}

export interface UseDefaultDashboardLayoutResult {
  defaultLayout: DashboardLayout
  loading: boolean
  error: Error | null
  refetch: () => void
  /**
   * Issue #472: True if layout came from API (or valid cache), false if using fallback.
   * Used to prevent version-based resets when backend is unavailable.
   */
  isFromApi: boolean
}

/**
 * Hook for fetching default dashboard layout with caching
 *
 * Features:
 * - 1-hour cache to reduce API calls
 * - Falls back to hardcoded DEFAULT_LAYOUT if API fails
 * - Memory + localStorage caching for persistence
 *
 * @returns Object containing default layout, loading state, error, and refetch function
 *
 * @example
 * ```tsx
 * function Dashboard() {
 *   const { defaultLayout, loading, error } = useDefaultDashboardLayout()
 *
 *   if (loading) return <LoadingSpinner />
 *
 *   // Use defaultLayout for initial setup or reset
 *   return <DashboardGrid layout={userLayout ?? defaultLayout} />
 * }
 * ```
 */
export function useDefaultDashboardLayout(): UseDefaultDashboardLayoutResult {
  // Start with hardcoded fallback to avoid undefined states
  const [defaultLayout, setDefaultLayout] = useState<DashboardLayout>(DEFAULT_LAYOUT)
  const [loading, setLoading] = useState<boolean>(true)
  const [error, setError] = useState<Error | null>(null)
  // Issue #472: Track whether layout came from API/cache or fallback
  const [isFromApi, setIsFromApi] = useState<boolean>(false)

  const loadLayout = useCallback(async (forceRefresh = false) => {
    try {
      setLoading(true)
      setError(null)

      // Check memory cache first if not forcing refresh
      if (!forceRefresh && memoryCache && isCacheValid()) {
        setDefaultLayout(memoryCache)
        setIsFromApi(true) // Cache came from successful API call
        setLoading(false)
        return
      }

      // Check localStorage cache
      if (!forceRefresh) {
        const localStorageData = loadFromLocalStorage()
        if (localStorageData) {
          memoryCache = localStorageData
          setDefaultLayout(localStorageData)
          setIsFromApi(true) // Cache came from successful API call
          setLoading(false)
          return
        }
      }

      // Fetch fresh data from API
      const data = await fetchDefaultDashboardLayout()

      // Update cache and state
      updateCache(data)
      setDefaultLayout(data)
      setIsFromApi(true) // Successfully fetched from API
    } catch (err) {
      // Log error but use fallback silently
      console.warn('Failed to fetch default dashboard layout, using fallback:', err)
      setError(err instanceof Error ? err : new Error('Failed to fetch default dashboard layout'))
      // Keep using fallback DEFAULT_LAYOUT - don't set to null
      setIsFromApi(false) // Using fallback, not from API
    } finally {
      setLoading(false)
    }
  }, [])

  const refetch = useCallback(() => {
    clearDefaultLayoutCache()
    loadLayout(true)
  }, [loadLayout])

  // Fetch on mount
  useEffect(() => {
    loadLayout()
  }, [loadLayout])

  return {
    defaultLayout,
    loading,
    error,
    refetch,
    isFromApi,
  }
}
