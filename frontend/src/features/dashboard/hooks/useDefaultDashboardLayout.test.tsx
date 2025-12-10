/**
 * useDefaultDashboardLayout Hook Tests
 *
 * Tests the React hook for fetching default dashboard layout with 1hr caching.
 * Issue #449: Centralize default dashboard layout configuration
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useDefaultDashboardLayout, clearDefaultLayoutCache } from './useDefaultDashboardLayout'
import * as dashboardApi from '../api/dashboardApi'
import type { DashboardLayout } from '../types'

// Mock the API module
vi.mock('../api/dashboardApi')

// Mock the registry to avoid circular dependency issues
vi.mock('../widgets/registry', () => ({
  DEFAULT_LAYOUT: {
    widgets: [
      {
        i: 'fallback-widget-1',
        type: 'cash-flow-chart',
        x: 0,
        y: 0,
        w: 12,
        h: 5,
        minW: 4,
        minH: 3,
      },
    ],
    version: 1,
  },
}))

describe('useDefaultDashboardLayout', () => {
  const mockLayoutResponse: DashboardLayout = {
    widgets: [
      {
        i: 'cash-flow-chart-1',
        type: 'cash-flow-chart',
        x: 0,
        y: 0,
        w: 12,
        h: 5,
        minW: 4,
        minH: 3,
      },
      {
        i: 'balance-history-1',
        type: 'balance-history',
        x: 0,
        y: 5,
        w: 12,
        h: 4,
        minW: 4,
        minH: 3,
      },
    ],
    version: 3,
  }

  beforeEach(() => {
    vi.clearAllMocks()
    // Clear cache (both memory and localStorage) before each test
    clearDefaultLayoutCache()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
  })

  it('should return loading state initially', () => {
    // Arrange
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockImplementation(
      () => new Promise(() => {}) // Never resolves
    )

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout())

    // Assert
    expect(result.current.loading).toBe(true)
    // Should have fallback layout even during loading
    expect(result.current.defaultLayout).toBeDefined()
    expect(result.current.error).toBeNull()
  })

  it('should fetch and return default layout on mount', async () => {
    // Arrange
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockResolvedValue(mockLayoutResponse)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout())

    // Assert - Initial loading state
    expect(result.current.loading).toBe(true)

    // Wait for data to load
    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Data loaded successfully
    expect(result.current.defaultLayout).toEqual(mockLayoutResponse)
    expect(result.current.error).toBeNull()
    expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(1)
  })

  it('should fall back to hardcoded layout on fetch error', async () => {
    // Arrange
    const error = new Error('Failed to fetch layout')
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockRejectedValue(error)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout())

    // Wait for error state
    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Should have error but still have a default layout (fallback)
    expect(result.current.error).toBeDefined()
    expect(result.current.defaultLayout).toBeDefined()
    expect(result.current.defaultLayout.version).toBe(1) // Fallback version
  })

  it('should cache layout in memory for 1 hour', async () => {
    // Arrange
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockResolvedValue(mockLayoutResponse)

    // Act - First render
    const { result: result1, unmount: unmount1 } = renderHook(() => useDefaultDashboardLayout())

    await waitFor(() => {
      expect(result1.current.loading).toBe(false)
    })

    expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(1)

    // Unmount - cache remains in memory
    unmount1()

    // Re-render immediately (still within cache window)
    const { result: result2 } = renderHook(() => useDefaultDashboardLayout())

    await waitFor(() => {
      expect(result2.current.loading).toBe(false)
    })

    // Assert - Should use cached data, not fetch again
    expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(1) // Still only 1 call
    expect(result2.current.defaultLayout).toEqual(mockLayoutResponse)
  })

  it('should refetch after 1 hour cache expires', async () => {
    // Arrange
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockResolvedValue(mockLayoutResponse)

    // Simulate expired cache by setting old timestamp
    const twoHoursAgo = Date.now() - (2 * 60 * 60 * 1000)
    localStorage.setItem('default_dashboard_layout_cache_timestamp', String(twoHoursAgo))

    // Act - Render with expired cache
    const { result } = renderHook(() => useDefaultDashboardLayout())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Should fetch fresh data (cache expired)
    expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(1)
    expect(result.current.defaultLayout).toEqual(mockLayoutResponse)
  })

  it('should provide refetch function to force cache invalidation', async () => {
    // Arrange
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockResolvedValue(mockLayoutResponse)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(1)

    // Act - Call refetch
    result.current.refetch()

    await waitFor(() => {
      expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(2)
    })

    // Assert - Should have fetched again
    expect(result.current.defaultLayout).toEqual(mockLayoutResponse)
  })

  it('should store cache data in localStorage', async () => {
    // Arrange
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockResolvedValue(mockLayoutResponse)

    // Act
    renderHook(() => useDefaultDashboardLayout())

    await waitFor(() => {
      expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(1)
    })

    // Assert - Check timestamp
    const cachedTimestamp = localStorage.getItem('default_dashboard_layout_cache_timestamp')
    expect(cachedTimestamp).toBeTruthy()

    const timestamp = Number(cachedTimestamp)
    expect(timestamp).toBeGreaterThan(0)
    expect(timestamp).toBeLessThanOrEqual(Date.now())

    // Assert - Check data
    const cachedData = localStorage.getItem('default_dashboard_layout_cache_data')
    expect(cachedData).toBeTruthy()
    expect(JSON.parse(cachedData!)).toEqual(mockLayoutResponse)
  })

  it('should return version 3 with 2 widgets from API', async () => {
    // Arrange
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockResolvedValue(mockLayoutResponse)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Issue #449: Verify expected layout from API
    expect(result.current.defaultLayout.version).toBe(3)
    expect(result.current.defaultLayout.widgets).toHaveLength(2)
    expect(result.current.defaultLayout.widgets[0].type).toBe('cash-flow-chart')
    expect(result.current.defaultLayout.widgets[1].type).toBe('balance-history')
  })

  it('should load from localStorage cache when memory cache is empty', async () => {
    // Arrange
    vi.mocked(dashboardApi.fetchDefaultDashboardLayout).mockResolvedValue(mockLayoutResponse)

    // First render - populate cache
    const { unmount } = renderHook(() => useDefaultDashboardLayout())

    await waitFor(() => {
      expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(1)
    })

    unmount()

    // Clear memory cache but keep localStorage
    clearDefaultLayoutCache()
    // Restore localStorage data (simulating browser persistence)
    localStorage.setItem('default_dashboard_layout_cache_timestamp', String(Date.now()))
    localStorage.setItem('default_dashboard_layout_cache_data', JSON.stringify(mockLayoutResponse))

    // Act - New render should use localStorage cache
    const { result } = renderHook(() => useDefaultDashboardLayout())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Should not make another API call (using localStorage cache)
    expect(dashboardApi.fetchDefaultDashboardLayout).toHaveBeenCalledTimes(1)
    expect(result.current.defaultLayout).toEqual(mockLayoutResponse)
  })
})
