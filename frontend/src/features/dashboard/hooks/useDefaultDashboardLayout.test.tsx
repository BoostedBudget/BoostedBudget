/**
 * useDefaultDashboardLayout Hook Tests
 *
 * Tests the React hook for fetching default dashboard layout with 1hr caching.
 * Issue #449: Centralize default dashboard layout configuration
 * Issue #564: Migrated to generated React Query hooks
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useDefaultDashboardLayout } from './useDefaultDashboardLayout'
import type { DashboardLayout } from '../types'

// Mock the generated hook
const mockRefetch = vi.fn()
vi.mock('@/api/generated/endpoints/dashboard/dashboard', () => ({
  useGetDefaultLayout: vi.fn(),
  getGetDefaultLayoutQueryKey: vi.fn(() => ['dashboard', 'default-layout']),
}))

// Import the mock after vi.mock
import { useGetDefaultLayout } from '@/api/generated/endpoints/dashboard/dashboard'

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

// Helper to create wrapper with QueryClient
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

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
    mockRefetch.mockClear()
  })

  // Helper to configure the mock for loading state
  function configureLoading() {
    vi.mocked(useGetDefaultLayout).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockRefetch,
      isSuccess: false,
    } as unknown as ReturnType<typeof useGetDefaultLayout>)
  }

  // Helper to configure the mock for success state
  function configureSuccess(data: DashboardLayout) {
    vi.mocked(useGetDefaultLayout).mockReturnValue({
      data,
      isLoading: false,
      error: null,
      refetch: mockRefetch,
      isSuccess: true,
    } as unknown as ReturnType<typeof useGetDefaultLayout>)
  }

  // Helper to configure the mock for error state
  function configureError(error: Error) {
    vi.mocked(useGetDefaultLayout).mockReturnValue({
      data: undefined,
      isLoading: false,
      error,
      refetch: mockRefetch,
      isSuccess: false,
    } as unknown as ReturnType<typeof useGetDefaultLayout>)
  }

  it('should return loading state initially', () => {
    // Arrange
    configureLoading()

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert
    expect(result.current.loading).toBe(true)
    // Should have fallback layout even during loading
    expect(result.current.defaultLayout).toBeDefined()
    expect(result.current.error).toBeNull()
  })

  it('should return default layout when loaded successfully', () => {
    // Arrange
    configureSuccess(mockLayoutResponse)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - Data loaded successfully
    expect(result.current.loading).toBe(false)
    expect(result.current.defaultLayout).toEqual(mockLayoutResponse)
    expect(result.current.error).toBeNull()
    expect(result.current.isFromApi).toBe(true)
  })

  it('should fall back to hardcoded layout on fetch error', () => {
    // Arrange
    const error = new Error('Failed to fetch layout')
    configureError(error)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - Should have error but still have a default layout (fallback)
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeDefined()
    expect(result.current.error?.message).toBe('Failed to fetch layout')
    expect(result.current.defaultLayout).toBeDefined()
    expect(result.current.defaultLayout.version).toBe(1) // Fallback version
    expect(result.current.isFromApi).toBe(false)
  })

  it('should provide refetch function', () => {
    // Arrange
    configureSuccess(mockLayoutResponse)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - refetch function is available
    expect(typeof result.current.refetch).toBe('function')
  })

  it('should call query refetch when refetch is called', async () => {
    // Arrange
    configureSuccess(mockLayoutResponse)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Call refetch
    result.current.refetch()

    // Assert
    await waitFor(() => {
      expect(mockRefetch).toHaveBeenCalled()
    })
  })

  it('should return version 3 with 2 widgets from API', () => {
    // Arrange
    configureSuccess(mockLayoutResponse)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - Issue #449: Verify expected layout from API
    expect(result.current.defaultLayout.version).toBe(3)
    expect(result.current.defaultLayout.widgets).toHaveLength(2)
    expect(result.current.defaultLayout.widgets[0].type).toBe('cash-flow-chart')
    expect(result.current.defaultLayout.widgets[1].type).toBe('balance-history')
  })

  it('should set isFromApi to true when data comes from API', () => {
    // Arrange
    configureSuccess(mockLayoutResponse)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - Issue #472: isFromApi should be true
    expect(result.current.isFromApi).toBe(true)
  })

  it('should set isFromApi to false when using fallback', () => {
    // Arrange
    configureError(new Error('Network error'))

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - Issue #472: isFromApi should be false when using fallback
    expect(result.current.isFromApi).toBe(false)
  })

  it('should set isFromApi to false during loading', () => {
    // Arrange
    configureLoading()

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - isFromApi should be false while loading
    expect(result.current.isFromApi).toBe(false)
  })

  it('should use staleTime for caching (configured in hook)', () => {
    // Arrange
    configureSuccess(mockLayoutResponse)

    // Act
    renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - Verify useGetDefaultLayout was called with query options
    expect(useGetDefaultLayout).toHaveBeenCalledWith(
      expect.objectContaining({
        query: expect.objectContaining({
          staleTime: 60 * 60 * 1000, // 1 hour
          gcTime: 60 * 60 * 1000 * 2, // 2 hours
          retry: 2,
        }),
      })
    )
  })

  it('should convert non-Error objects to Error', () => {
    // Arrange - Pass a non-Error object as error
    vi.mocked(useGetDefaultLayout).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: 'String error' as unknown as Error,
      refetch: mockRefetch,
      isSuccess: false,
    } as unknown as ReturnType<typeof useGetDefaultLayout>)

    // Act
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })

    // Assert - Should convert to Error instance
    expect(result.current.error).toBeInstanceOf(Error)
    expect(result.current.error?.message).toBe('Failed to fetch default dashboard layout')
  })
})
