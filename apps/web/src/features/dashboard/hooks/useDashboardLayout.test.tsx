/**
 * useDashboardLayout Hook Tests
 * Issue #354: Customizable Dashboard Widget System
 * Issue #472: Tests for isFromApi fallback behavior
 * Issue #575: Migrated to bun:test with centralized API mocks
 *
 * Tests the dashboard layout management hook including loading,
 * saving, debounce behavior, widget operations, and error handling.
 * NOTE: Runs with API-level mocks (dashboard-api.ts) for real hook testing
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { setSystemTime } from 'bun:test'
import { renderHook, act, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useDashboardLayout } from './useDashboardLayout'
import { DEFAULT_LAYOUT } from '../widgets/registry'
import type { DashboardLayout, DashboardPreferencesResponse, WidgetConfig } from '../types'
import {
  mockUseGetDashboardPreferences,
  mockUseSaveDashboardPreferences,
  mockUseResetDashboardPreferences,
  mockSaveDashboardPreferencesMutateAsync,
  mockResetDashboardPreferencesMutateAsync,
  mockRefetchDashboardPreferences,
  mockUseGetDefaultLayout,
  mockRefetchDefaultLayout,
} from '@/test/mocks/dashboard-api'
import { mockCreateWidgetConfig } from '@/test/mocks/registry'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useDashboardLayout', () => {
  const mockLayout: DashboardLayout = {
    widgets: [
      { i: 'cash-flow-chart-1', type: 'cash-flow-chart', x: 0, y: 0, w: 12, h: 5, minW: 4, minH: 4 },
      { i: 'balance-history-1', type: 'balance-history', x: 0, y: 5, w: 12, h: 4, minW: 4, minH: 4 },
    ],
    version: 4,
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
    minH: 4,
  }

  let originalTime: number

  beforeEach(() => {
    originalTime = Date.now()
    setSystemTime(new Date(originalTime))

    // Clear all mocks
    mockUseGetDashboardPreferences.mockClear()
    mockUseSaveDashboardPreferences.mockClear()
    mockUseResetDashboardPreferences.mockClear()
    mockSaveDashboardPreferencesMutateAsync.mockClear()
    mockResetDashboardPreferencesMutateAsync.mockClear()
    mockRefetchDashboardPreferences.mockClear()
    mockUseGetDefaultLayout.mockClear()
    mockRefetchDefaultLayout.mockClear()
    mockCreateWidgetConfig.mockClear()

    // Configure useGetDefaultLayout mock to return default layout (used by useDefaultDashboardLayout)
    mockUseGetDefaultLayout.mockReturnValue({
      data: DEFAULT_LAYOUT,
      isLoading: false,
      error: null,
      refetch: mockRefetchDefaultLayout,
      isSuccess: true,
    })

    // Default mocks for generated hooks
    mockUseGetDashboardPreferences.mockReturnValue({
      data: mockPreferencesResponse,
      isLoading: false,
      error: null,
      refetch: mockRefetchDashboardPreferences,
    })

    mockUseSaveDashboardPreferences.mockReturnValue({
      mutateAsync: mockSaveDashboardPreferencesMutateAsync,
      isPending: false,
    })

    mockUseResetDashboardPreferences.mockReturnValue({
      mutateAsync: mockResetDashboardPreferencesMutateAsync,
      isPending: false,
    })

    mockSaveDashboardPreferencesMutateAsync.mockResolvedValue({})
    mockResetDashboardPreferencesMutateAsync.mockResolvedValue({})
  })

  afterEach(() => {
    setSystemTime()
  })

  function configurePreferencesQuery(config: {
    data?: DashboardPreferencesResponse | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetDashboardPreferences.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchDashboardPreferences,
    })
  }

  function configureDefaultLayoutQuery(config: {
    data?: DashboardLayout | null
    isLoading?: boolean
    error?: Error | null
    isSuccess?: boolean
  }) {
    mockUseGetDefaultLayout.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchDefaultLayout,
      isSuccess: config.isSuccess ?? false,
    })
  }

  describe('Initial State', () => {
    it('should return loading=true when query is loading', async () => {
      configurePreferencesQuery({ isLoading: true })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
    })

    it('should return error=null when query is loading', async () => {
      configurePreferencesQuery({ isLoading: true })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeNull()
    })

    it('should return saving=false initially', async () => {
      configurePreferencesQuery({ isLoading: true })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.saving).toBe(false)
    })
  })

  describe('Fetching Layout', () => {
    it('should set layout from query data', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.layout).toEqual(mockLayout)
      expect(result.current.error).toBeNull()
    })

    it('should handle query errors', async () => {
      configurePreferencesQuery({ error: new Error('Network error') })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.error).toBe('Failed to load dashboard preferences')
      expect(result.current.layout).toEqual(DEFAULT_LAYOUT)
    })
  })

  describe('Saving Layout', () => {
    it('should debounce save calls', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })
      mockSaveDashboardPreferencesMutateAsync.mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Make multiple updates rapidly
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 5 })
      })
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 6 })
      })
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 7 })
      })

      expect(mockSaveDashboardPreferencesMutateAsync).not.toHaveBeenCalled()

      // Advance time past debounce period (500ms)
      setSystemTime(new Date(originalTime + 600))

      await waitFor(() => {
        expect(mockSaveDashboardPreferencesMutateAsync).toHaveBeenCalledTimes(1)
      })
    })

    it('should set saving=true during save', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })

      let resolvePromise: (value: unknown) => void
      const savePromise = new Promise((resolve) => {
        resolvePromise = resolve
      })
      mockSaveDashboardPreferencesMutateAsync.mockReturnValue(savePromise)

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 5 })
      })

      setSystemTime(new Date(originalTime + 600))

      await waitFor(() => {
        expect(result.current.saving).toBe(true)
      })

      await act(async () => {
        resolvePromise!(mockPreferencesResponse)
      })

      await waitFor(() => {
        expect(result.current.saving).toBe(false)
      })
    })

    it('should handle save errors', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })
      mockSaveDashboardPreferencesMutateAsync.mockRejectedValue(new Error('Save failed'))

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 5 })
      })

      setSystemTime(new Date(originalTime + 600))

      await waitFor(() => {
        expect(result.current.error).toBe('Failed to save layout')
      })
    })
  })

  describe('Widget Operations', () => {
    it('should add a widget', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })
      mockCreateWidgetConfig.mockReturnValue(mockNewWidget)

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const initialWidgetCount = result.current.layout.widgets.length

      act(() => {
        result.current.addWidget('balance-history')
      })

      expect(result.current.layout.widgets.length).toBe(initialWidgetCount + 1)
      expect(result.current.layout.widgets).toContainEqual(
        expect.objectContaining({ type: 'balance-history' })
      )
    })

    it('should remove a widget', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const widgetToRemove = result.current.layout.widgets[0]
      const initialWidgetCount = result.current.layout.widgets.length

      act(() => {
        result.current.removeWidget(widgetToRemove.i)
      })

      expect(result.current.layout.widgets.length).toBe(initialWidgetCount - 1)
      expect(result.current.layout.widgets).not.toContainEqual(
        expect.objectContaining({ i: widgetToRemove.i })
      )
    })
  })

  describe('Reset to Default', () => {
    it('should call reset mutation', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })
      const resetResponse: DashboardPreferencesResponse = {
        layout: DEFAULT_LAYOUT,
      }
      mockResetDashboardPreferencesMutateAsync.mockResolvedValue(resetResponse)

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.resetToDefault()
      })

      expect(mockResetDashboardPreferencesMutateAsync).toHaveBeenCalled()
      expect(result.current.layout).toEqual(DEFAULT_LAYOUT)
    })

    it('should handle reset errors gracefully', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })
      mockResetDashboardPreferencesMutateAsync.mockRejectedValue(new Error('Reset failed'))

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.resetToDefault()
      })

      expect(result.current.error).toBe('Failed to reset layout')
      expect(result.current.layout).toEqual(DEFAULT_LAYOUT)
    })
  })

  describe('Save Now', () => {
    it('should save immediately without debounce', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse })
      mockSaveDashboardPreferencesMutateAsync.mockResolvedValue(mockPreferencesResponse)

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 5 })
      })

      await act(async () => {
        await result.current.saveNow()
      })

      expect(mockSaveDashboardPreferencesMutateAsync).toHaveBeenCalled()
    })
  })

  describe('Version Migration (Issue #449, #472)', () => {
    it('should reset layout when stored version is outdated and isFromApi is true', async () => {
      const outdatedLayout: DashboardLayout = {
        widgets: mockLayout.widgets,
        version: 2,
      }
      const outdatedResponse: DashboardPreferencesResponse = {
        layout: outdatedLayout,
      }
      configurePreferencesQuery({ data: outdatedResponse })

      const resetResponse: DashboardPreferencesResponse = {
        layout: DEFAULT_LAYOUT,
      }
      mockResetDashboardPreferencesMutateAsync.mockResolvedValue(resetResponse)

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(mockResetDashboardPreferencesMutateAsync).toHaveBeenCalled()
      expect(result.current.layout).toEqual(DEFAULT_LAYOUT)
    })

    it('should NOT reset layout when stored version is outdated but isFromApi is false', async () => {
      // Configure useGetDefaultLayout to simulate fallback mode (isFromApi = false)
      configureDefaultLayoutQuery({
        data: null,
        isLoading: false,
        error: new Error('API not available'),
        isSuccess: false,
      })

      const outdatedLayout: DashboardLayout = {
        widgets: mockLayout.widgets,
        version: 2,
      }
      const outdatedResponse: DashboardPreferencesResponse = {
        layout: outdatedLayout,
      }
      configurePreferencesQuery({ data: outdatedResponse })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(mockResetDashboardPreferencesMutateAsync).not.toHaveBeenCalled()
      expect(result.current.layout).toEqual(outdatedLayout)
    })
  })

  describe('Loading State Dependencies', () => {
    it('should wait for default layout to load before processing', async () => {
      configureDefaultLayoutQuery({
        data: undefined,
        isLoading: true,
        error: null,
        isSuccess: false,
      })

      configurePreferencesQuery({ data: mockPreferencesResponse })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
    })
  })

  // Issue #1297: updateWidgetSettings

  describe('updateWidgetSettings', () => {
    const layoutWithSettings: DashboardLayout = {
      widgets: [
        {
          i: 'portfolio-holdings-1',
          type: 'portfolio-holdings',
          x: 0,
          y: 0,
          w: 6,
          h: 4,
          settings: { topN: 5, sortBy: 'value' },
        },
        {
          i: 'asset-allocation-1',
          type: 'asset-allocation',
          x: 6,
          y: 0,
          w: 4,
          h: 4,
        },
      ],
      version: 4,
    }

    it('should expose updateWidgetSettings on the result', async () => {
      configurePreferencesQuery({
        data: { layout: layoutWithSettings },
      })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(typeof result.current.updateWidgetSettings).toBe('function')
    })

    it('should merge new settings into the target widget', async () => {
      configurePreferencesQuery({
        data: { layout: layoutWithSettings },
      })
      mockSaveDashboardPreferencesMutateAsync.mockResolvedValue({})

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateWidgetSettings('portfolio-holdings-1', {
          topN: 10,
          sortBy: 'day_change',
        })
      })

      const updatedWidget = result.current.layout.widgets.find(
        (w) => w.i === 'portfolio-holdings-1'
      )
      expect(updatedWidget?.settings).toEqual({ topN: 10, sortBy: 'day_change' })
    })

    it('should not modify other widgets when updating one', async () => {
      configurePreferencesQuery({
        data: { layout: layoutWithSettings },
      })
      mockSaveDashboardPreferencesMutateAsync.mockResolvedValue({})

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateWidgetSettings('portfolio-holdings-1', { topN: 20 })
      })

      const allocationWidget = result.current.layout.widgets.find(
        (w) => w.i === 'asset-allocation-1'
      )
      expect(allocationWidget?.settings).toBeUndefined()
    })

    it('should be a no-op when settings are unchanged', async () => {
      configurePreferencesQuery({
        data: {
          layout: {
            ...layoutWithSettings,
            widgets: [
              {
                ...layoutWithSettings.widgets[0],
                settings: { topN: 5, sortBy: 'value' },
              },
              layoutWithSettings.widgets[1],
            ],
          },
        },
      })
      mockSaveDashboardPreferencesMutateAsync.mockResolvedValue({})

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        // Same settings — should not trigger save
        result.current.updateWidgetSettings('portfolio-holdings-1', {
          topN: 5,
          sortBy: 'value',
        })
      })

      // No debounced save triggered
      setSystemTime(new Date(originalTime + 600))

      await new Promise((resolve) => setTimeout(resolve, 50))

      expect(mockSaveDashboardPreferencesMutateAsync).not.toHaveBeenCalled()
    })

    it('should schedule a debounced save after settings change', async () => {
      configurePreferencesQuery({
        data: { layout: layoutWithSettings },
      })
      mockSaveDashboardPreferencesMutateAsync.mockResolvedValue({})

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateWidgetSettings('portfolio-holdings-1', { topN: 20 })
      })

      expect(mockSaveDashboardPreferencesMutateAsync).not.toHaveBeenCalled()

      setSystemTime(new Date(originalTime + 600))

      await waitFor(() => {
        expect(mockSaveDashboardPreferencesMutateAsync).toHaveBeenCalledTimes(1)
      })
    })

    it('settings and geometry survive interleaved updates (drag-then-settings)', async () => {
      // Simulates: user drags a widget (updateLayout call), then changes settings
      // Both changes should be present in the final save payload
      configurePreferencesQuery({
        data: { layout: layoutWithSettings },
      })
      mockSaveDashboardPreferencesMutateAsync.mockResolvedValue({})

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Simulate drag: updateLayout with new geometry (settings intact from original)
      act(() => {
        result.current.updateLayout({
          ...result.current.layout,
          widgets: result.current.layout.widgets.map((w) =>
            w.i === 'portfolio-holdings-1' ? { ...w, x: 2 } : w
          ),
        })
      })

      // Then settings change
      act(() => {
        result.current.updateWidgetSettings('portfolio-holdings-1', { topN: 20 })
      })

      const target = result.current.layout.widgets.find(
        (w) => w.i === 'portfolio-holdings-1'
      )
      // Geometry change from drag
      expect(target?.x).toBe(2)
      // Settings change preserved
      expect(target?.settings).toEqual({ topN: 20 })
    })

    it('settings survive a geometry-only updateLayout call (settings-then-drag)', async () => {
      // Simulates: user changes settings, then drag fires updateLayout
      // Settings set via updateWidgetSettings must not be lost
      configurePreferencesQuery({
        data: { layout: layoutWithSettings },
      })
      mockSaveDashboardPreferencesMutateAsync.mockResolvedValue({})

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Settings change first
      act(() => {
        result.current.updateWidgetSettings('portfolio-holdings-1', { topN: 20 })
      })

      // Then drag fires updateLayout (must preserve settings already set)
      act(() => {
        result.current.updateLayout({
          ...result.current.layout,
          widgets: result.current.layout.widgets.map((w) =>
            w.i === 'portfolio-holdings-1' ? { ...w, x: 3 } : w
          ),
        })
      })

      const target = result.current.layout.widgets.find(
        (w) => w.i === 'portfolio-holdings-1'
      )
      expect(target?.x).toBe(3)
      expect(target?.settings).toEqual({ topN: 20 })
    })

    it('handles a layout loaded without settings field (legacy back-compat)', async () => {
      const legacyLayout: DashboardLayout = {
        widgets: [
          {
            i: 'portfolio-holdings-1',
            type: 'portfolio-holdings',
            x: 0,
            y: 0,
            w: 6,
            h: 4,
            // No settings field
          },
        ],
        version: 4,
      }
      configurePreferencesQuery({ data: { layout: legacyLayout } })

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Should render without crash; settings undefined is valid
      const w = result.current.layout.widgets[0]
      expect(w.settings).toBeUndefined()

      // Calling updateWidgetSettings on a widget without existing settings must work
      act(() => {
        result.current.updateWidgetSettings('portfolio-holdings-1', { topN: 10 })
      })

      const updated = result.current.layout.widgets.find(
        (w) => w.i === 'portfolio-holdings-1'
      )
      expect(updated?.settings).toEqual({ topN: 10 })
    })
  })
})
