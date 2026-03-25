/**
 * useIsAdmin Hook Tests
 * Issue #642: Admin feature foundation
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { mockUseCheckRole, mockRefetchCheckRole } from '@/test/mocks/admin-api'
import { mockUseAuth, createMockClerkAuth } from '@/test/mocks/clerk'
import { useIsAdmin } from './useIsAdmin'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useIsAdmin', () => {
  beforeEach(() => {
    mockUseCheckRole.mockClear()
    mockRefetchCheckRole.mockClear()
    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true }))
  })

  it('should return loading state initially', () => {
    mockUseCheckRole.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockRefetchCheckRole,
    })

    const { result } = renderHook(() => useIsAdmin(), { wrapper: createWrapper() })

    expect(result.current.loading).toBe(true)
    expect(result.current.isAdmin).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('should return isAdmin true when user is admin', async () => {
    mockUseCheckRole.mockReturnValue({
      data: { is_admin: true },
      isLoading: false,
      error: null,
      refetch: mockRefetchCheckRole,
    })

    const { result } = renderHook(() => useIsAdmin(), { wrapper: createWrapper() })

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(result.current.isAdmin).toBe(true)
    expect(result.current.error).toBeNull()
  })

  it('should return isAdmin false when user is not admin', async () => {
    mockUseCheckRole.mockReturnValue({
      data: { is_admin: false },
      isLoading: false,
      error: null,
      refetch: mockRefetchCheckRole,
    })

    const { result } = renderHook(() => useIsAdmin(), { wrapper: createWrapper() })

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(result.current.isAdmin).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('should return error when API call fails', async () => {
    const apiError = new Error('API Error')
    mockUseCheckRole.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: apiError,
      refetch: mockRefetchCheckRole,
    })

    const { result } = renderHook(() => useIsAdmin(), { wrapper: createWrapper() })

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(result.current.isAdmin).toBe(false)
    expect(result.current.error).toBeInstanceOf(Error)
    expect(result.current.error?.message).toBe('Failed to check admin role')
  })

  it('should provide refetch function', () => {
    mockUseCheckRole.mockReturnValue({
      data: { is_admin: true },
      isLoading: false,
      error: null,
      refetch: mockRefetchCheckRole,
    })

    const { result } = renderHook(() => useIsAdmin(), { wrapper: createWrapper() })

    expect(result.current.refetch).toBeDefined()
    expect(typeof result.current.refetch).toBe('function')
  })

  it('should default isAdmin to false when data is undefined', () => {
    mockUseCheckRole.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockRefetchCheckRole,
    })

    const { result } = renderHook(() => useIsAdmin(), { wrapper: createWrapper() })

    expect(result.current.isAdmin).toBe(false)
  })
})
