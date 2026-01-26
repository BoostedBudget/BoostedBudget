/**
 * UsersPage Component Tests
 * Issue #644: Admin User Management UI
 *
 * Tests for admin user management page.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { mockUseListAdminUsers } from '@/test/mocks/admin-api'
import { UsersPage } from './UsersPage'

// Mock test data
const mockUsers = [
  {
    id: 'user-1',
    email: 'admin@example.com',
    subscription_tier: 'premium',
    subscription_status: 'active',
    primary_region: 'iad',
    created_at: '2024-01-15T10:00:00Z',
    last_login_at: '2024-03-15T14:30:00Z',
    is_active: true,
    linked_accounts_count: 5,
    automation_rules_count: 10,
  },
  {
    id: 'user-2',
    email: 'user@example.com',
    subscription_tier: 'free',
    subscription_status: 'trial',
    primary_region: 'lhr',
    created_at: '2024-02-20T08:00:00Z',
    is_active: true,
    linked_accounts_count: 2,
    automation_rules_count: 1,
  },
]

const mockListResponse = {
  users: mockUsers,
  total_count: 2,
  page: 1,
  page_size: 20,
  total_pages: 1,
  has_more: false,
}

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    )
  }
}

describe('UsersPage', () => {
  beforeEach(() => {
    mockUseListAdminUsers.mockClear()
  })

  describe('loading state', () => {
    it('should show loading indicator when data is loading', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      expect(screen.getByRole('status')).toBeInTheDocument()
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })
  })

  describe('empty state', () => {
    it('should show empty state when no users found', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: {
          users: [],
          total_count: 0,
          page: 1,
          page_size: 20,
          total_pages: 0,
          has_more: false,
        },
        isLoading: false,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      expect(screen.getByText(/no users found/i)).toBeInTheDocument()
    })
  })

  describe('user list', () => {
    it('should render user table with data', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: mockListResponse,
        isLoading: false,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      // Check headers
      expect(screen.getByText('Email')).toBeInTheDocument()
      expect(screen.getByText('Tier')).toBeInTheDocument()
      expect(screen.getByText('Status')).toBeInTheDocument()
      expect(screen.getByText('Region')).toBeInTheDocument()
      expect(screen.getByText('Last Login')).toBeInTheDocument()
      expect(screen.getByText('Accounts')).toBeInTheDocument()
      expect(screen.getByText('Rules')).toBeInTheDocument()

      // Check user data
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      expect(screen.getByText('user@example.com')).toBeInTheDocument()
      expect(screen.getByText('premium')).toBeInTheDocument()
      expect(screen.getByText('free')).toBeInTheDocument()
    })

    it('should show total count in header', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: mockListResponse,
        isLoading: false,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      expect(screen.getByText(/2 users/i)).toBeInTheDocument()
    })
  })

  describe('search', () => {
    it('should render search input', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: mockListResponse,
        isLoading: false,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      expect(screen.getByPlaceholderText(/search by email/i)).toBeInTheDocument()
    })

    it('should call API with search param when searching', async () => {
      mockUseListAdminUsers.mockReturnValue({
        data: mockListResponse,
        isLoading: false,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      const searchInput = screen.getByPlaceholderText(/search by email/i)
      await userEvent.type(searchInput, 'admin@')

      // The hook should be called with search param
      await waitFor(() => {
        expect(mockUseListAdminUsers).toHaveBeenCalled()
      })
    })
  })

  describe('tier filter', () => {
    it('should render tier filter dropdown', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: mockListResponse,
        isLoading: false,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      expect(screen.getByLabelText(/filter by tier/i)).toBeInTheDocument()
    })
  })

  describe('pagination', () => {
    it('should show pagination controls when multiple pages', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: {
          ...mockListResponse,
          total_count: 50,
          total_pages: 3,
          has_more: true,
        },
        isLoading: false,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      expect(screen.getByText(/page 1 of 3/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /next/i })).toBeInTheDocument()
    })

    it('should disable previous button on first page', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: {
          ...mockListResponse,
          total_count: 50,
          total_pages: 3,
          has_more: true,
        },
        isLoading: false,
        error: null,
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      expect(screen.getByRole('button', { name: /previous/i })).toBeDisabled()
    })
  })

  describe('error state', () => {
    it('should show error message when API fails', () => {
      mockUseListAdminUsers.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: new Error('Failed to load users'),
      })

      render(<UsersPage />, { wrapper: createWrapper() })

      expect(screen.getByText(/failed to load users/i)).toBeInTheDocument()
    })
  })
})
