import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { DashboardLayout } from '../types'

const mockRefetch = mock(() => {})

const mockUseGetDefaultLayout = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockRefetch,
  isSuccess: false,
}))

mock.module('@/api/generated/endpoints/dashboard/dashboard', () => ({
  useGetDefaultLayout: mockUseGetDefaultLayout,
  getGetDefaultLayoutQueryKey: mock(() => ['dashboard', 'default-layout']),
}))

mock.module('../widgets/registry', () => ({
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

import { useDefaultDashboardLayout } from './useDefaultDashboardLayout'

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

  const configureMock = (data: unknown = undefined, isLoading = false, error: unknown = null, isSuccess = false) => {
    mockUseGetDefaultLayout.mockReturnValue({
      data,
      isLoading,
      error,
      refetch: mockRefetch,
      isSuccess,
    })
  }

  beforeEach(() => {
    mockUseGetDefaultLayout.mockClear()
    mockRefetch.mockClear()
  })

  it('should return loading state initially', () => {
    configureMock(undefined, true)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(result.current.loading).toBe(true)
    expect(result.current.defaultLayout).toBeDefined()
    expect(result.current.error).toBeNull()
  })

  it('should return default layout when loaded successfully', () => {
    configureMock(mockLayoutResponse, false, null, true)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(result.current.loading).toBe(false)
    expect(result.current.defaultLayout).toEqual(mockLayoutResponse)
    expect(result.current.error).toBeNull()
    expect(result.current.isFromApi).toBe(true)
  })

  it('should fall back to hardcoded layout on fetch error', () => {
    const error = new Error('Failed to fetch layout')
    configureMock(undefined, false, error)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeDefined()
    expect(result.current.error?.message).toBe('Failed to fetch layout')
    expect(result.current.defaultLayout).toBeDefined()
    expect(result.current.defaultLayout.version).toBe(1)
    expect(result.current.isFromApi).toBe(false)
  })

  it('should provide refetch function', () => {
    configureMock(mockLayoutResponse, false, null, true)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(typeof result.current.refetch).toBe('function')
  })

  it('should call query refetch when refetch is called', async () => {
    configureMock(mockLayoutResponse, false, null, true)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    result.current.refetch()
    await waitFor(() => {
      expect(mockRefetch).toHaveBeenCalled()
    })
  })

  it('should return version 3 with 2 widgets from API', () => {
    configureMock(mockLayoutResponse, false, null, true)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(result.current.defaultLayout.version).toBe(3)
    expect(result.current.defaultLayout.widgets).toHaveLength(2)
    expect(result.current.defaultLayout.widgets[0].type).toBe('cash-flow-chart')
    expect(result.current.defaultLayout.widgets[1].type).toBe('balance-history')
  })

  it('should set isFromApi to true when data comes from API', () => {
    configureMock(mockLayoutResponse, false, null, true)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(result.current.isFromApi).toBe(true)
  })

  it('should set isFromApi to false when using fallback', () => {
    configureMock(undefined, false, new Error('Network error'))
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(result.current.isFromApi).toBe(false)
  })

  it('should set isFromApi to false during loading', () => {
    configureMock(undefined, true)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(result.current.isFromApi).toBe(false)
  })

  it('should use staleTime for caching (configured in hook)', () => {
    configureMock(mockLayoutResponse, false, null, true)
    renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(mockUseGetDefaultLayout).toHaveBeenCalledWith(
      expect.objectContaining({
        query: expect.objectContaining({
          staleTime: 60 * 60 * 1000,
          gcTime: 60 * 60 * 1000 * 2,
          retry: 2,
        }),
      })
    )
  })

  it('should convert non-Error objects to Error', () => {
    configureMock(undefined, false, 'String error' as unknown as Error)
    const { result } = renderHook(() => useDefaultDashboardLayout(), {
      wrapper: createWrapper(),
    })
    expect(result.current.error).toBeInstanceOf(Error)
    expect(result.current.error?.message).toBe('Failed to fetch default dashboard layout')
  })
})
