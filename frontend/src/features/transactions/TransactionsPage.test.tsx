/**
 * TransactionsPage Component Tests
 *
 * Tests the transactions page container component.
 * Issue #564: Migrated to mock generated React Query hooks
 * Issue #575: Migrated to bun:test with centralized mocks
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'
import { TransactionsPage } from './TransactionsPage'
import type { Transaction } from './types'
import type { UserTag } from '../tags/types'
import type { CustomCategory } from '../categories/types'

// Import centralized mocks
import {
  mockUseGetTransactions,
  mockRefetchTransactions,
  mockUpdateTransactionTagsMutateAsync,
  mockUpdateTransactionCategoryMutateAsync,
} from '@/test/mocks/transactions-api'
import {
  mockUseGetUserTags,
  mockCreateTagMutateAsync,
  mockUseCreateTag,
} from '@/test/mocks/tags-api'
import {
  mockUseGetUserCategories,
  mockUseGetPlaidCategories,
  mockCreateCategoryMutateAsync,
  mockUseCreateCategory,
} from '@/test/mocks/categories-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })
}

function createWrapper() {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>{children}</BrowserRouter>
      </QueryClientProvider>
    )
  }
}

describe('TransactionsPage', () => {
  const mockTransactions: Transaction[] = [
    {
      id: 'txn-1',
      user_id: 'user-123',
      account_id: 'account-456',
      linked_item_id: 'item-789',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-abc',
      amount: -25.50,
      date: '2025-10-25',
      name: 'Starbucks',
      merchant_name: 'Starbucks Coffee',
      category: '["Food and Drink", "Coffee"]',
      pending: false,
      created_at: '2025-10-25T10:00:00Z',
      updated_at: '2025-10-25T10:00:00Z',
    },
    {
      id: 'txn-2',
      user_id: 'user-123',
      account_id: 'account-456',
      linked_item_id: 'item-789',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-def',
      amount: 1000.00,
      date: '2025-10-24',
      name: 'Salary',
      merchant_name: null,
      category: '["Income", "Payroll"]',
      pending: false,
      created_at: '2025-10-24T10:00:00Z',
      updated_at: '2025-10-24T10:00:00Z',
    },
  ]

  const mockTags: UserTag[] = [
    {
      id: 'tag-1',
      user_id: 'user-123',
      name: 'Food',
      color: '#FF5733',
      usage_count: 5,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'tag-2',
      user_id: 'user-123',
      name: 'Shopping',
      color: '#33FF57',
      usage_count: 3,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ]

  const mockCategories: CustomCategory[] = [
    {
      id: 'cat-1',
      user_id: 'user-123',
      name: 'Food & Dining',
      color: '#FF5733',
      usage_count: 10,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'cat-2',
      user_id: 'user-123',
      name: 'Shopping',
      color: '#33FF57',
      usage_count: 5,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ]

  function configureQueries(overrides?: {
    transactions?: Transaction[]
    isLoading?: boolean
    error?: Error | null
    hasMore?: boolean
    totalCount?: number
  }) {
    const transactionsData = overrides?.isLoading
      ? undefined
      : {
          transactions: overrides?.transactions ?? mockTransactions,
          total_count: overrides?.totalCount ?? 25,
          limit: 50,
          offset: 0,
          has_more: overrides?.hasMore ?? false,
        }

    mockUseGetTransactions.mockReturnValue({
      data: transactionsData,
      isLoading: overrides?.isLoading ?? false,
      error: overrides?.error ?? null,
      refetch: mockRefetchTransactions,
    })

    mockUseGetUserTags.mockReturnValue({
      data: mockTags,
      isLoading: false,
      error: null,
    })

    mockUseGetUserCategories.mockReturnValue({
      data: mockCategories,
      isLoading: false,
      error: null,
    })

    mockUseGetPlaidCategories.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
    })

    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [] },
      isLoading: false,
      error: null,
    })

    mockUseCreateTag.mockReturnValue({
      mutateAsync: mockCreateTagMutateAsync,
      isPending: false,
    })

    mockUseCreateCategory.mockReturnValue({
      mutateAsync: mockCreateCategoryMutateAsync,
      isPending: false,
    })
  }

  beforeEach(() => {
    // Clear all mocks
    mockUseGetTransactions.mockClear()
    mockRefetchTransactions.mockClear()
    mockUpdateTransactionTagsMutateAsync.mockClear()
    mockUpdateTransactionCategoryMutateAsync.mockClear()
    mockUseGetUserTags.mockClear()
    mockUseGetUserCategories.mockClear()
    mockUseGetPlaidCategories.mockClear()
    mockUseGetAccounts.mockClear()
    mockUseCreateTag.mockClear()
    mockUseCreateCategory.mockClear()

    configureQueries()
  })

  describe('Initial Loading', () => {
    it('should show loading state initially', async () => {
      configureQueries({ isLoading: true, transactions: [] })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should load and display transactions on mount', async () => {
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByText('Salary')).toBeInTheDocument()
    })
  })

  describe('Error Handling', () => {
    it('should display error message when fetch fails', async () => {
      configureQueries({ error: new Error('Network error'), transactions: [] })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
      })
    })

    it('should have retry button on error', async () => {
      configureQueries({ error: new Error('Network error'), transactions: [] })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })
    })

    it('should retry loading when retry button clicked', async () => {
      const user = userEvent.setup()
      const localRefetch = mock(() => Promise.resolve({}))
      configureQueries({ error: new Error('Network error'), transactions: [] })
      mockUseGetTransactions.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: new Error('Network error'),
        refetch: localRefetch,
      })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
      })

      const retryButton = screen.getByRole('button', { name: /retry/i })
      await user.click(retryButton)

      expect(localRefetch).toHaveBeenCalled()
    })
  })

  describe('Empty State', () => {
    it('should show empty state when no transactions', async () => {
      configureQueries({ transactions: [], totalCount: 0 })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText(/no transactions/i)).toBeInTheDocument()
      })
    })
  })

  describe('Transaction Display', () => {
    it('should display all transactions in a list', async () => {
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByText('Salary')).toBeInTheDocument()
    })

    it('should show total count', async () => {
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText(/25.*transaction/i)).toBeInTheDocument()
      })
    })
  })

  describe('Pagination', () => {
    it('should not show next button when has_more is false', async () => {
      configureQueries({ hasMore: false })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.queryByRole('button', { name: /next/i })).not.toBeInTheDocument()
    })

    it('should show next button when has_more is true', async () => {
      configureQueries({ hasMore: true })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByRole('button', { name: /next/i })).toBeInTheDocument()
    })

    it('should not show previous button on first page', async () => {
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.queryByRole('button', { name: /previous/i })).not.toBeInTheDocument()
    })

    it('should load next page when next button clicked', async () => {
      const user = userEvent.setup()
      configureQueries({ hasMore: true })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const nextButton = screen.getByRole('button', { name: /next/i })
      await user.click(nextButton)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /previous/i })).toBeInTheDocument()
      })
    })
  })

  describe('Search', () => {
    it('should have search input', async () => {
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByPlaceholderText(/search/i)).toBeInTheDocument()
    })

    it('should filter transactions when search input changes', async () => {
      const user = userEvent.setup()
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const searchInput = screen.getByPlaceholderText(/search/i)
      await user.type(searchInput, 'starbucks')

      await waitFor(
        () => {
          expect(searchInput).toHaveValue('starbucks')
        },
        { timeout: 1000 }
      )
    })
  })

  describe('Accessibility', () => {
    it('should have proper heading structure', async () => {
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /transactions/i })).toBeInTheDocument()
      })
    })

    it('should have main landmark', async () => {
      const Wrapper = createWrapper()
      const { container } = render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(container.querySelector('main')).toBeInTheDocument()
    })
  })

  describe('Transaction Editing', () => {
    it('should fetch tags and categories on mount', async () => {
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(mockUseGetUserTags).toHaveBeenCalled()
        expect(mockUseGetUserCategories).toHaveBeenCalled()
      })
    })

    it('should open editor when clicking edit button', async () => {
      const user = userEvent.setup()
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      expect(editButtons.length).toBeGreaterThan(0)
      await user.click(editButtons[0])

      await waitFor(() => {
        expect(screen.getByText('Edit Transaction')).toBeInTheDocument()
      })
    })

    it('should display transaction details in editor', async () => {
      const user = userEvent.setup()
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      await user.click(editButtons[0])

      await waitFor(() => {
        expect(screen.getByText('Edit Transaction')).toBeInTheDocument()
        expect(screen.getByText('$25.50')).toBeInTheDocument()
      })
    })

    it('should close editor when cancel is clicked', async () => {
      const user = userEvent.setup()
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      await user.click(editButtons[0])

      await waitFor(() => {
        expect(screen.getByText('Edit Transaction')).toBeInTheDocument()
      })

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      await user.click(cancelButton)

      await waitFor(() => {
        expect(screen.queryByText('Edit Transaction')).not.toBeInTheDocument()
      })
    })

    it('should handle tag/category fetch errors gracefully', async () => {
      mockUseGetUserTags.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: new Error('Failed to fetch tags'),
      })

      mockUseGetUserCategories.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: new Error('Failed to fetch categories'),
      })

      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })
    })

    it('should render table with edit buttons for each transaction', async () => {
      const Wrapper = createWrapper()
      render(<TransactionsPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByRole('table')).toBeInTheDocument()
      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      expect(editButtons.length).toBeGreaterThan(0)
    })
  })
})
