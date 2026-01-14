/**
 * useDefaultDashboardLayout Hook
 *
 * React hook for fetching default dashboard layout configuration with caching.
 * Uses generated React Query hooks from OpenAPI spec.
 *
 * Issue #449: Centralize default dashboard layout configuration
 * Issue #472: Added isFromApi flag to prevent resets when using fallback
 * Issue #564: Migrated to generated React Query hooks
 *
 * The default layout is fetched from the backend API which loads it from
 * the centralized YAML config file (config/default_dashboard_layout.yaml).
 * This ensures both frontend and backend use the same source of truth.
 */

import { useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useGetDefaultLayout,
  getGetDefaultLayoutQueryKey,
} from '@boostedbudget/api-client/endpoints/dashboard/dashboard'
import { DEFAULT_LAYOUT } from '@/features/dashboard/widgets/registry'
import type { DashboardLayout } from '../types'

// Cache TTL: 1 hour (React Query handles caching natively)
const CACHE_TTL_MS = 60 * 60 * 1000

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
 * Clear the cache
 * @internal Exported for testing purposes
 */
export function clearDefaultLayoutCache(): void {
  // This function is kept for backwards compatibility but React Query
  // manages cache invalidation. Use refetch() or queryClient.invalidateQueries()
  // to force a refresh.
}

/**
 * Hook for fetching default dashboard layout with caching
 *
 * Features:
 * - 1-hour cache to reduce API calls (via React Query staleTime)
 * - Falls back to hardcoded DEFAULT_LAYOUT if API fails
 * - Automatic retry with exponential backoff
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
  const queryClient = useQueryClient()

  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
    isSuccess,
  } = useGetDefaultLayout({
    query: {
      staleTime: CACHE_TTL_MS,
      gcTime: CACHE_TTL_MS * 2, // Keep in cache for 2 hours
      retry: 2,
      retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, 10000),
    },
  })

  // Convert error to Error instance
  const error = queryError
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch default dashboard layout')
    : null

  // Fall back to DEFAULT_LAYOUT if API fails or no data
  // Convert generated type to local type (handle nullable widgets and string->WidgetType)
  // The generated API type uses `type: string` but local type uses strict WidgetType union
  const defaultLayout: DashboardLayout = data
    ? { version: data.version, widgets: (data.widgets ?? []) as DashboardLayout['widgets'] }
    : DEFAULT_LAYOUT

  // Issue #472: True if we got data from API, false if using fallback
  const isFromApi = isSuccess && data !== undefined

  // Refetch function that invalidates the cache
  const refetch = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: getGetDefaultLayoutQueryKey(),
    })
    queryRefetch()
  }, [queryClient, queryRefetch])

  return {
    defaultLayout,
    loading: isLoading,
    error,
    refetch,
    isFromApi,
  }
}
