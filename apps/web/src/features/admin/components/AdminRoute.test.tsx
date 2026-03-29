/**
 * AdminRoute Component Tests
 * Issue #642: Admin feature foundation
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { mockUseCheckRole } from '@/test/mocks/admin-api'
import { mockUseAuth, createMockClerkAuth } from '@/test/mocks/clerk'
import { ADMIN_MODE_STORAGE_KEY } from '../constants'
import { AdminRoute } from './AdminRoute'

function createWrapper(initialEntries: string[] = ['/admin']) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={initialEntries}>
          <Routes>
            <Route
              path="/admin"
              element={<AdminRoute>{children}</AdminRoute>}
            />
            <Route path="/dashboard" element={<div>Dashboard Page</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    )
  }
}

describe('AdminRoute', () => {
  beforeEach(() => {
    mockUseCheckRole.mockClear()
    localStorage.clear()
    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true }))
  })

  it('should show loading skeleton while checking admin status', () => {
    mockUseCheckRole.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: () => Promise.resolve(),
    })

    render(<div>Admin Content</div>, { wrapper: createWrapper() })

    expect(screen.getByTestId('admin-loading')).toBeInTheDocument()
    expect(screen.queryByText('Admin Content')).not.toBeInTheDocument()
  })

  it('should show error state when API fails', async () => {
    mockUseCheckRole.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('API Error'),
      refetch: () => Promise.resolve(),
    })

    render(<div>Admin Content</div>, { wrapper: createWrapper() })

    await waitFor(() => {
      expect(screen.getByTestId('admin-error')).toBeInTheDocument()
    })
    expect(screen.queryByText('Admin Content')).not.toBeInTheDocument()
  })

  it('should redirect to dashboard when user is not admin', async () => {
    mockUseCheckRole.mockReturnValue({
      data: { is_admin: false },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    localStorage.setItem(ADMIN_MODE_STORAGE_KEY, 'true')

    render(<div>Admin Content</div>, { wrapper: createWrapper() })

    await waitFor(() => {
      expect(screen.getByText('Dashboard Page')).toBeInTheDocument()
    })
    expect(screen.queryByText('Admin Content')).not.toBeInTheDocument()
  })

  it('should redirect when admin but admin mode is disabled', async () => {
    mockUseCheckRole.mockReturnValue({
      data: { is_admin: true },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    // Admin mode disabled (not in localStorage)
    localStorage.removeItem(ADMIN_MODE_STORAGE_KEY)

    render(<div>Admin Content</div>, { wrapper: createWrapper() })

    await waitFor(() => {
      expect(screen.getByText('Dashboard Page')).toBeInTheDocument()
    })
    expect(screen.queryByText('Admin Content')).not.toBeInTheDocument()
  })

  it('should render children when admin and admin mode is enabled', async () => {
    mockUseCheckRole.mockReturnValue({
      data: { is_admin: true },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    localStorage.setItem(ADMIN_MODE_STORAGE_KEY, 'true')

    render(<div>Admin Content</div>, { wrapper: createWrapper() })

    await waitFor(() => {
      expect(screen.getByText('Admin Content')).toBeInTheDocument()
    })
    expect(screen.queryByText('Dashboard Page')).not.toBeInTheDocument()
  })
})
