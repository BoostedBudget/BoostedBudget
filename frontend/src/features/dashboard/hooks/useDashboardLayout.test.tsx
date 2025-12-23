/**
 * useDashboardLayout Hook Tests
 * Issue #354: Customizable Dashboard Widget System
 * Issue #472: Tests for isFromApi fallback behavior
 * Issue #564: Migrated to mock generated React Query hooks
 *
 * Tests the dashboard layout management hook including loading,
 * saving, debounce behavior, widget operations, and error handling.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useDashboardLayout } from './useDashboardLayout';
import * as registry from '../widgets/registry';
import * as useDefaultDashboardLayoutModule from './useDefaultDashboardLayout';
import type { DashboardLayout, DashboardPreferencesResponse, WidgetConfig } from '../types';

// Use vi.hoisted for mock functions
const {
  mockPreferencesRefetch,
  mockSaveMutateAsync,
  mockResetMutateAsync,
} = vi.hoisted(() => ({
  mockPreferencesRefetch: vi.fn().mockResolvedValue({}),
  mockSaveMutateAsync: vi.fn().mockResolvedValue({}),
  mockResetMutateAsync: vi.fn().mockResolvedValue({}),
}));

// Mock the generated hooks
vi.mock('@/api/generated/endpoints/dashboard/dashboard', () => ({
  useGetDashboardPreferences: vi.fn(),
  useSaveDashboardPreferences: vi.fn(() => ({
    mutateAsync: mockSaveMutateAsync,
    isPending: false,
  })),
  useResetDashboardPreferences: vi.fn(() => ({
    mutateAsync: mockResetMutateAsync,
    isPending: false,
  })),
}));

// Import the mocked hooks
import {
  useGetDashboardPreferences,
  useSaveDashboardPreferences,
  useResetDashboardPreferences,
} from '@/api/generated/endpoints/dashboard/dashboard';

// Mock the registry module partially
vi.mock('../widgets/registry', async () => {
  const actual = await vi.importActual('../widgets/registry');
  return {
    ...actual,
    createWidgetConfig: vi.fn(),
  };
});

// Mock useDefaultDashboardLayout
vi.mock('./useDefaultDashboardLayout', async () => {
  const actual = await vi.importActual('./useDefaultDashboardLayout');
  return {
    ...actual,
    useDefaultDashboardLayout: vi.fn(),
  };
});

// Helper to create wrapper with QueryClient
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

describe('useDashboardLayout', () => {
  // Use version 4 (current DEFAULT_LAYOUT version) to avoid triggering auto-reset
  const mockLayout: DashboardLayout = {
    widgets: [
      { i: 'cash-flow-chart-1', type: 'cash-flow-chart', x: 0, y: 0, w: 12, h: 5, minW: 4, minH: 4 },
      { i: 'balance-history-1', type: 'balance-history', x: 0, y: 5, w: 12, h: 4, minW: 4, minH: 4 },
    ],
    version: 4,
  };

  const mockPreferencesResponse: DashboardPreferencesResponse = {
    layout: mockLayout,
  };

  const mockNewWidget: WidgetConfig = {
    i: 'new-widget-123',
    type: 'balance-history',
    x: 0,
    y: 0,
    w: 6,
    h: 4,
    minW: 4,
    minH: 4,
  };

  beforeEach(() => {
    vi.clearAllMocks();
    // Use fake timers but allow setImmediate to work for waitFor()
    vi.useFakeTimers({ shouldAdvanceTime: true });

    // Default mock for useDefaultDashboardLayout
    vi.mocked(useDefaultDashboardLayoutModule.useDefaultDashboardLayout).mockReturnValue({
      defaultLayout: registry.DEFAULT_LAYOUT,
      loading: false,
      error: null,
      refetch: vi.fn(),
      isFromApi: true,
    });

    // Default mocks for generated hooks
    vi.mocked(useGetDashboardPreferences).mockReturnValue({
      data: mockPreferencesResponse,
      isLoading: false,
      error: null,
      refetch: mockPreferencesRefetch,
    } as unknown as ReturnType<typeof useGetDashboardPreferences>);

    vi.mocked(useSaveDashboardPreferences).mockReturnValue({
      mutateAsync: mockSaveMutateAsync,
      isPending: false,
    } as unknown as ReturnType<typeof useSaveDashboardPreferences>);

    vi.mocked(useResetDashboardPreferences).mockReturnValue({
      mutateAsync: mockResetMutateAsync,
      isPending: false,
    } as unknown as ReturnType<typeof useResetDashboardPreferences>);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  // Helper to configure query state
  function configurePreferencesQuery(config: {
    data?: DashboardPreferencesResponse | null;
    isLoading?: boolean;
    error?: Error | null;
  }) {
    vi.mocked(useGetDashboardPreferences).mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockPreferencesRefetch,
    } as unknown as ReturnType<typeof useGetDashboardPreferences>);
  }

  describe('Initial State', () => {
    it('should return loading=true when query is loading', async () => {
      configurePreferencesQuery({ isLoading: true });

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(true);
    });

    it('should return error=null when query is loading', async () => {
      configurePreferencesQuery({ isLoading: true });

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      expect(result.current.error).toBeNull();
    });

    it('should return saving=false initially', async () => {
      configurePreferencesQuery({ isLoading: true });

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      expect(result.current.saving).toBe(false);
    });
  });

  describe('Fetching Layout', () => {
    it('should set layout from query data', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.layout).toEqual(mockLayout);
      expect(result.current.error).toBeNull();
    });

    it('should handle query errors', async () => {
      configurePreferencesQuery({ error: new Error('Network error') });

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.error).toBe('Failed to load dashboard preferences');
      // Should fall back to default layout
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT);
    });
  });

  describe('Saving Layout', () => {
    it('should debounce save calls', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });
      mockSaveMutateAsync.mockResolvedValue(mockPreferencesResponse);

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Make multiple updates rapidly
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 5 });
      });
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 6 });
      });
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 7 });
      });

      // Should not have saved yet
      expect(mockSaveMutateAsync).not.toHaveBeenCalled();

      // Advance timers past debounce period (500ms)
      await act(async () => {
        vi.advanceTimersByTime(600);
      });

      // Should only save once with the last value
      await waitFor(() => {
        expect(mockSaveMutateAsync).toHaveBeenCalledTimes(1);
      });
    });

    it('should set saving=true during save', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });
      // Create a promise that doesn't resolve immediately
      let resolvePromise: (value: unknown) => void;
      const savePromise = new Promise((resolve) => {
        resolvePromise = resolve;
      });
      mockSaveMutateAsync.mockReturnValue(savePromise);

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Trigger update
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 5 });
      });

      // Advance past debounce
      await act(async () => {
        vi.advanceTimersByTime(600);
      });

      // Should be saving
      expect(result.current.saving).toBe(true);

      // Resolve the save
      await act(async () => {
        resolvePromise!(mockPreferencesResponse);
      });

      await waitFor(() => {
        expect(result.current.saving).toBe(false);
      });
    });

    it('should handle save errors', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });
      mockSaveMutateAsync.mockRejectedValue(new Error('Save failed'));

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Trigger update
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 5 });
      });

      // Advance past debounce
      await act(async () => {
        vi.advanceTimersByTime(600);
      });

      await waitFor(() => {
        expect(result.current.error).toBe('Failed to save layout');
      });
    });
  });

  describe('Widget Operations', () => {
    it('should add a widget', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });
      vi.mocked(registry.createWidgetConfig).mockReturnValue(mockNewWidget);

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      const initialWidgetCount = result.current.layout.widgets.length;

      act(() => {
        result.current.addWidget('balance-history');
      });

      expect(result.current.layout.widgets.length).toBe(initialWidgetCount + 1);
      expect(result.current.layout.widgets).toContainEqual(
        expect.objectContaining({ type: 'balance-history' })
      );
    });

    it('should remove a widget', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      const widgetToRemove = result.current.layout.widgets[0];
      const initialWidgetCount = result.current.layout.widgets.length;

      act(() => {
        result.current.removeWidget(widgetToRemove.i);
      });

      expect(result.current.layout.widgets.length).toBe(initialWidgetCount - 1);
      expect(result.current.layout.widgets).not.toContainEqual(
        expect.objectContaining({ i: widgetToRemove.i })
      );
    });
  });

  describe('Reset to Default', () => {
    it('should call reset mutation', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });
      const resetResponse: DashboardPreferencesResponse = {
        layout: registry.DEFAULT_LAYOUT,
      };
      mockResetMutateAsync.mockResolvedValue(resetResponse);

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.resetToDefault();
      });

      expect(mockResetMutateAsync).toHaveBeenCalled();
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT);
    });

    it('should handle reset errors gracefully', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });
      mockResetMutateAsync.mockRejectedValue(new Error('Reset failed'));

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.resetToDefault();
      });

      expect(result.current.error).toBe('Failed to reset layout');
      // Should still update to default layout locally
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT);
    });
  });

  describe('Save Now', () => {
    it('should save immediately without debounce', async () => {
      configurePreferencesQuery({ data: mockPreferencesResponse });
      mockSaveMutateAsync.mockResolvedValue(mockPreferencesResponse);

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Trigger an update (which would normally debounce)
      act(() => {
        result.current.updateLayout({ ...mockLayout, version: 5 });
      });

      // Call saveNow immediately
      await act(async () => {
        await result.current.saveNow();
      });

      // Should have saved without waiting for debounce
      expect(mockSaveMutateAsync).toHaveBeenCalled();
    });
  });

  describe('Version Migration (Issue #449, #472)', () => {
    it('should reset layout when stored version is outdated and isFromApi is true', async () => {
      const outdatedLayout: DashboardLayout = {
        widgets: mockLayout.widgets,
        version: 2, // Outdated version
      };
      const outdatedResponse: DashboardPreferencesResponse = {
        layout: outdatedLayout,
      };
      configurePreferencesQuery({ data: outdatedResponse });

      // Default layout has version 4
      const resetResponse: DashboardPreferencesResponse = {
        layout: registry.DEFAULT_LAYOUT,
      };
      mockResetMutateAsync.mockResolvedValue(resetResponse);

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Should have triggered reset mutation
      expect(mockResetMutateAsync).toHaveBeenCalled();
      expect(result.current.layout).toEqual(registry.DEFAULT_LAYOUT);
    });

    it('should NOT reset layout when stored version is outdated but isFromApi is false', async () => {
      // Simulate fallback mode (isFromApi = false)
      vi.mocked(useDefaultDashboardLayoutModule.useDefaultDashboardLayout).mockReturnValue({
        defaultLayout: registry.DEFAULT_LAYOUT,
        loading: false,
        error: null,
        refetch: vi.fn(),
        isFromApi: false, // Using fallback
      });

      const outdatedLayout: DashboardLayout = {
        widgets: mockLayout.widgets,
        version: 2, // Outdated version
      };
      const outdatedResponse: DashboardPreferencesResponse = {
        layout: outdatedLayout,
      };
      configurePreferencesQuery({ data: outdatedResponse });

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Should NOT have triggered reset
      expect(mockResetMutateAsync).not.toHaveBeenCalled();
      // Should use the user's outdated layout
      expect(result.current.layout).toEqual(outdatedLayout);
    });
  });

  describe('Loading State Dependencies', () => {
    it('should wait for default layout to load before processing', async () => {
      // Default layout is still loading
      vi.mocked(useDefaultDashboardLayoutModule.useDefaultDashboardLayout).mockReturnValue({
        defaultLayout: registry.DEFAULT_LAYOUT,
        loading: true, // Still loading
        error: null,
        refetch: vi.fn(),
        isFromApi: false,
      });

      configurePreferencesQuery({ data: mockPreferencesResponse });

      const { result } = renderHook(() => useDashboardLayout(), {
        wrapper: createWrapper(),
      });

      // Should still be in loading state
      expect(result.current.loading).toBe(true);
    });
  });
});
