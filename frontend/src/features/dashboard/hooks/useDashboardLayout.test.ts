/**
 * useDashboardLayout Hook Tests
 *
 * Tests the dashboard layout management hook including loading,
 * saving, debounce behavior, widget operations, and error handling.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useDashboardLayout } from './useDashboardLayout'
import * as preferencesApi from '../api/preferencesApi'
import * as registry from '../widgets/registry'
import type { DashboardLayout, DashboardPreferencesResponse, WidgetConfig } from '../types'

// Mock the API module
vi.mock('../api/preferencesApi')

// Mock the registry module partially
vi.mock('../widgets/registry', async () => {
  const actual = await vi.importActual('../widgets/registry')
  return {
    ...actual,
    createWidgetConfig: vi.fn(),
  }
})

describe('useDashboardLayout', () => {
  // Use version 3 (current DEFAULT_LAYOUT version) to avoid triggering auto-reset
  const mockLayout: DashboardLayout = {
    widgets: [
      { i: 'cash-flow-chart-1', type: 'cash-flow-chart', x: 0, y: 0, w: 12, h: 5, minW: 4, minH: 3 },
      { i: 'balance-history-1', type: 'balance-history', x: 0, y: 5, w: 12, h: 4, minW: 4, minH: 3 },
    ],
    version: 3,
  }

  const mockPreferencesResponse: DashboardPreferencesResponse = {
    layout: mockLayout,
  }

  const mockNewWidget: WidgetConfig = {
    i: 'new-widget-123',
    type: 'balance-history',
    x: 0,
    y: 0,
    w: 6,
    h: 4,
    minW: 4,
    minH: 3,
  }

  beforeEach(() => {
    vi.clearAllMocks()
    // Use fake timers but allow setImmediate to work for waitFor()
    vi.useFakeTimers({ shouldAdvanceTime: true })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  describe('Initial State', () => {
    it('should return loading=true initially', async () => {
      // Never resolve to keep loading state
      vi.mocked(preferencesApi.getDashboardPreferences).mockImplementation(
        () => new Promise(() => {})
      )

      const { result } = renderHook(() => useDashboardLayout())

      expect(result.current.loading).toBe(true)
    })

    it('should return error=null initially', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockImplementation(
        () => new Promise(() => {})
      )

      const { result } = renderHook(() => useDashboardLayout())

      expect(result.current.error).toBeNull()
    })

    it('should return saving=false initially', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockImplementation(
        () => new Promise(() => {})
      )

      const { result } = renderHook(() => useDashboardLayout())

      expect(result.current.saving).toBe(false)
    })
  })

  describe('Load Layout', () => {
    it('should fetch preferences on mount', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(preferencesApi.getDashboardPreferences).toHaveBeenCalledTimes(1)
      })
    })

    it('should set layout from API response', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.layout).toEqual(mockLayout)
      })
    })

    it('should set loading=false after fetch', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })
    })

    it('should handle 404 as new user - use default layout without error', async () => {
      const error404 = new Error('Request failed with status 404')
      vi.mocked(preferencesApi.getDashboardPreferences).mockRejectedValue(error404)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Should use default layout
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT)
      // Should NOT set error for 404
      expect(result.current.error).toBeNull()
    })

    it('should handle non-404 errors with error message', async () => {
      const error500 = new Error('Server error 500')
      vi.mocked(preferencesApi.getDashboardPreferences).mockRejectedValue(error500)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Should use default layout
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT)
      // Should set error for non-404
      expect(result.current.error).toBe('Failed to load dashboard preferences')
    })
  })

  describe('Version-Based Auto-Reset', () => {
    it('should auto-reset when stored version is older than DEFAULT_LAYOUT', async () => {
      // Stored layout has version 1 (outdated)
      const outdatedLayout: DashboardPreferencesResponse = {
        layout: {
          widgets: [
            { i: 'old-widget', type: 'balance-overview', x: 0, y: 0, w: 4, h: 2, minW: 3, minH: 2 },
          ],
          version: 1, // Older than DEFAULT_LAYOUT.version (2)
        },
      }

      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(outdatedLayout)
      vi.mocked(preferencesApi.resetDashboardPreferences).mockResolvedValue({
        layout: registry.DEFAULT_LAYOUT,
      })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Should have called reset due to version mismatch
      expect(preferencesApi.resetDashboardPreferences).toHaveBeenCalledTimes(1)
      // Layout should be the new default
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT)
    })

    it('should NOT auto-reset when stored version matches DEFAULT_LAYOUT', async () => {
      // Stored layout has same version as current
      const currentVersionLayout: DashboardPreferencesResponse = {
        layout: {
          widgets: [
            { i: 'custom-widget', type: 'cash-flow-chart', x: 0, y: 0, w: 8, h: 4, minW: 4, minH: 3 },
          ],
          version: registry.DEFAULT_LAYOUT.version, // Same as current
        },
      }

      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(currentVersionLayout)
      vi.mocked(preferencesApi.resetDashboardPreferences).mockResolvedValue({
        layout: registry.DEFAULT_LAYOUT,
      })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Should NOT have called reset
      expect(preferencesApi.resetDashboardPreferences).not.toHaveBeenCalled()
      // Layout should be the user's custom layout (not reset to default)
      expect(result.current.layout.widgets[0].w).toBe(8)
    })

    it('should fall back to DEFAULT_LAYOUT if reset fails during migration', async () => {
      const outdatedLayout: DashboardPreferencesResponse = {
        layout: {
          widgets: [],
          version: 1,
        },
      }

      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(outdatedLayout)
      vi.mocked(preferencesApi.resetDashboardPreferences).mockRejectedValue(new Error('Reset failed'))

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Should fall back to DEFAULT_LAYOUT
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT)
      // Should not set error (graceful fallback)
      expect(result.current.error).toBeNull()
    })
  })

  describe('Debounced Save', () => {
    it('should schedule save after layout change', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const newLayout: DashboardLayout = { ...mockLayout, version: 2 }

      act(() => {
        result.current.updateLayout(newLayout)
      })

      // Should not save immediately
      expect(preferencesApi.saveDashboardPreferences).not.toHaveBeenCalled()

      // Fast forward debounce time
      await act(async () => {
        vi.advanceTimersByTime(1000)
      })

      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledWith(newLayout)
    })

    it('should collapse multiple rapid changes into one save', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Make multiple updates in quick succession
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 2 })
      })

      act(() => {
        vi.advanceTimersByTime(500) // Half the debounce time
      })

      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 3 })
      })

      act(() => {
        vi.advanceTimersByTime(500) // Another half
      })

      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 4 })
      })

      // Fast forward past debounce time
      await act(async () => {
        vi.advanceTimersByTime(1000)
      })

      // Should only save once with the last layout
      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledTimes(1)
      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledWith(
        expect.objectContaining({ version: 4 })
      )
    })

    it('should save after SAVE_DEBOUNCE_MS (1000ms)', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 2 })
      })

      // Not saved at 999ms
      act(() => {
        vi.advanceTimersByTime(999)
      })
      expect(preferencesApi.saveDashboardPreferences).not.toHaveBeenCalled()

      // Saved at 1000ms
      await act(async () => {
        vi.advanceTimersByTime(1)
      })
      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledTimes(1)
    })

    it('should cancel pending save on new update', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // First update
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 2 })
      })

      // Wait 900ms
      act(() => {
        vi.advanceTimersByTime(900)
      })

      // Second update (cancels first)
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 3 })
      })

      // Wait another 900ms (only 900ms since second update)
      act(() => {
        vi.advanceTimersByTime(900)
      })

      // First save should NOT have fired
      expect(preferencesApi.saveDashboardPreferences).not.toHaveBeenCalled()

      // Wait the remaining 100ms
      await act(async () => {
        vi.advanceTimersByTime(100)
      })

      // Now it should save with version 3
      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledTimes(1)
      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledWith(
        expect.objectContaining({ version: 3 })
      )
    })

    it('should cleanup timeout on unmount', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result, unmount } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Schedule a save
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 2 })
      })

      // Unmount before debounce completes
      unmount()

      // Fast forward
      await act(async () => {
        vi.advanceTimersByTime(2000)
      })

      // Save should NOT have been called
      expect(preferencesApi.saveDashboardPreferences).not.toHaveBeenCalled()
    })
  })

  describe('updateLayout', () => {
    it('should update layout state immediately (optimistic)', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const newLayout: DashboardLayout = {
        widgets: [...mockLayout.widgets, mockNewWidget],
        version: 2,
      }

      act(() => {
        result.current.updateLayout(newLayout)
      })

      // Layout should be updated immediately
      expect(result.current.layout).toEqual(newLayout)
    })

    it('should schedule save with new layout', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const newLayout: DashboardLayout = { widgets: [], version: 2 }

      act(() => {
        result.current.updateLayout(newLayout)
      })

      await act(async () => {
        vi.advanceTimersByTime(1000)
      })

      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledWith(newLayout)
    })
  })

  describe('addWidget', () => {
    it('should add widget with correct config', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(registry.createWidgetConfig).mockReturnValue({ ...mockNewWidget })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.addWidget('balance-history')
      })

      expect(result.current.layout.widgets).toHaveLength(3)
      expect(result.current.layout.widgets[2].type).toBe('balance-history')
    })

    it('should position at bottom (y = maxY) when y is Infinity', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(registry.createWidgetConfig).mockReturnValue({ ...mockNewWidget, y: Infinity })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Current widgets: y=0,h=5 and y=5,h=4 -> maxY should be 9
      act(() => {
        result.current.addWidget('balance-history')
      })

      const addedWidget = result.current.layout.widgets[2]
      expect(addedWidget.y).toBe(9) // maxY = max(0+5, 5+4) = 9
    })

    it('should use default size from definition', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(registry.createWidgetConfig).mockReturnValue({
        i: 'test-widget',
        type: 'balance-history',
        x: 0,
        y: 0,
        w: 6,
        h: 4,
        minW: 4,
        minH: 3,
      })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.addWidget('balance-history')
      })

      expect(registry.createWidgetConfig).toHaveBeenCalledWith('balance-history', 0, Infinity)
    })

    it('should generate unique ID', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(registry.createWidgetConfig)
        .mockReturnValueOnce({ ...mockNewWidget, i: 'unique-id-1' })
        .mockReturnValueOnce({ ...mockNewWidget, i: 'unique-id-2' })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.addWidget('balance-history')
      })

      act(() => {
        result.current.addWidget('balance-history')
      })

      const ids = result.current.layout.widgets.map((w) => w.i)
      const uniqueIds = new Set(ids)
      expect(uniqueIds.size).toBe(ids.length)
    })

    it('should handle empty layout (y = 0)', async () => {
      const emptyLayout: DashboardPreferencesResponse = {
        layout: { widgets: [], version: 2 },
      }
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(emptyLayout)
      vi.mocked(registry.createWidgetConfig).mockReturnValue({ ...mockNewWidget, y: Infinity })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.addWidget('balance-history')
      })

      const addedWidget = result.current.layout.widgets[0]
      expect(addedWidget.y).toBe(0) // No existing widgets, maxY = 0
    })

    it('should not add widget if createWidgetConfig returns null', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(registry.createWidgetConfig).mockReturnValue(null)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const initialLength = result.current.layout.widgets.length

      act(() => {
        result.current.addWidget('unknown-type' as 'balance-overview')
      })

      expect(result.current.layout.widgets.length).toBe(initialLength)
    })
  })

  describe('removeWidget', () => {
    it('should remove widget by ID', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.layout.widgets).toHaveLength(2)

      act(() => {
        result.current.removeWidget('cash-flow-chart-1')
      })

      expect(result.current.layout.widgets).toHaveLength(1)
      expect(result.current.layout.widgets[0].i).toBe('balance-history-1')
    })

    it('should update layout immediately', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.removeWidget('balance-history-1')
      })

      // Immediate update
      expect(result.current.layout.widgets.some((w) => w.i === 'balance-history-1')).toBe(false)
    })

    it('should schedule save after remove', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.removeWidget('cash-flow-chart-1')
      })

      await act(async () => {
        vi.advanceTimersByTime(1000)
      })

      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledWith(
        expect.objectContaining({
          widgets: expect.not.arrayContaining([expect.objectContaining({ i: 'cash-flow-chart-1' })]),
        })
      )
    })
  })

  describe('resetToDefault', () => {
    it('should clear pending saves', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.resetDashboardPreferences).mockResolvedValue({
        layout: registry.DEFAULT_LAYOUT,
      })
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Schedule a save
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 99 })
      })

      // Reset before debounce completes
      await act(async () => {
        await result.current.resetToDefault()
      })

      // Fast forward past debounce
      await act(async () => {
        vi.advanceTimersByTime(2000)
      })

      // Only reset API should have been called, not save
      expect(preferencesApi.saveDashboardPreferences).not.toHaveBeenCalled()
      expect(preferencesApi.resetDashboardPreferences).toHaveBeenCalledTimes(1)
    })

    it('should call reset API endpoint', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.resetDashboardPreferences).mockResolvedValue({
        layout: registry.DEFAULT_LAYOUT,
      })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.resetToDefault()
      })

      expect(preferencesApi.resetDashboardPreferences).toHaveBeenCalledTimes(1)
    })

    it('should update layout to DEFAULT_LAYOUT', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.resetDashboardPreferences).mockResolvedValue({
        layout: registry.DEFAULT_LAYOUT,
      })

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.resetToDefault()
      })

      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT)
    })

    it('should handle reset errors gracefully', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.resetDashboardPreferences).mockRejectedValue(new Error('Reset failed'))

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.resetToDefault()
      })

      // Should still update local state to default
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT)
      // Should set error
      expect(result.current.error).toBe('Failed to reset layout')
    })

    it('should set saving state during reset', async () => {
      let resolveFn: () => void
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.resetDashboardPreferences).mockImplementation(
        () =>
          new Promise((resolve) => {
            resolveFn = () => resolve({ layout: registry.DEFAULT_LAYOUT })
          })
      )

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      let resetPromise: Promise<void>
      act(() => {
        resetPromise = result.current.resetToDefault()
      })

      // Should be saving
      expect(result.current.saving).toBe(true)

      await act(async () => {
        resolveFn()
        await resetPromise
      })

      // Should no longer be saving
      expect(result.current.saving).toBe(false)
    })
  })

  describe('saveNow', () => {
    it('should clear debounce timeout', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Schedule a debounced save
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 2 })
      })

      // Force immediate save
      await act(async () => {
        await result.current.saveNow()
      })

      // Should have saved once
      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledTimes(1)

      // Fast forward past normal debounce time
      await act(async () => {
        vi.advanceTimersByTime(2000)
      })

      // Should still only have saved once (debounce was cleared)
      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledTimes(1)
    })

    it('should save immediately', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 2 })
      })

      // No time passed, force save
      await act(async () => {
        await result.current.saveNow()
      })

      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledTimes(1)
    })

    it('should save pending layout if exists', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const pendingLayout = { ...mockLayout, version: 5 }

      act(() => {
        result.current.updateLayout(pendingLayout)
      })

      await act(async () => {
        await result.current.saveNow()
      })

      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledWith(pendingLayout)
    })

    it('should save current layout if no pending', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // No update, just save current
      await act(async () => {
        await result.current.saveNow()
      })

      expect(preferencesApi.saveDashboardPreferences).toHaveBeenCalledWith(mockLayout)
    })
  })

  describe('Error Handling', () => {
    it('should set error on save failure', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences).mockRejectedValue(new Error('Save failed'))

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 2 })
      })

      await act(async () => {
        vi.advanceTimersByTime(1000)
      })

      expect(result.current.error).toBe('Failed to save layout')
    })

    it('should clear error on successful save', async () => {
      vi.mocked(preferencesApi.getDashboardPreferences).mockResolvedValue(mockPreferencesResponse)
      vi.mocked(preferencesApi.saveDashboardPreferences)
        .mockRejectedValueOnce(new Error('Save failed'))
        .mockResolvedValueOnce(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // First save - fails
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 2 })
      })

      await act(async () => {
        vi.advanceTimersByTime(1000)
      })

      expect(result.current.error).toBe('Failed to save layout')

      // Second save - succeeds
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 3 })
      })

      await act(async () => {
        vi.advanceTimersByTime(1000)
      })

      expect(result.current.error).toBeNull()
    })
  })
})
