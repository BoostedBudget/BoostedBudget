/**
 * TransactionsPage Analytics Tests
 *
 * Issue #1200: Multi-Level Category Hierarchy
 *
 * Dedicated test file for category filter analytics events.
 * Uses mock.module for useTrackEvent (safe here — this is a focused,
 * single-purpose file, not a broad test file per CLAUDE.md guidance).
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'

// Mock useTrackEvent before any component imports so the spy is installed
// process-globally for this file. Acceptable: this is a focused analytics test
// file, not a broad component test file.
const mockTrackEvent = mock(() => {})
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

// Imports must come AFTER mock.module() calls in bun:test
import { TransactionsPage } from './TransactionsPage'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import {
  mockUseInfiniteTransactions,
  mockFetchNextPage,
  mockGetTransactions,
} from '@/test/mocks/transactions-api'
import {
  mockUseGetUserTags,
} from '@/test/mocks/tags-api'
import {
  mockUseGetUserCategories,
  mockUseGetPlaidPrimaryCategories,
} from '@/test/mocks/categories-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import {
  mockUseUpdateTransactionTags,
  mockUpdateTransactionTagsMutateAsync,
  mockUseUpdateTransactionCategory,
  mockUpdateTransactionCategoryMutateAsync,
  mockUseGetTransactions,
  mockRefetchTransactions,
} from '@/test/mocks/transactions-api'
import {
  mockUseCreateTag,
  mockCreateTagMutateAsync,
} from '@/test/mocks/tags-api'
import {
  mockUseCreateCategory,
  mockCreateCategoryMutateAsync,
  mockUseGetPlaidCategories,
} from '@/test/mocks/categories-api'
import type { Transaction } from './types'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>{children}</BrowserRouter>
      </QueryClientProvider>
    )
  }
}

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
    pfc_primary: 'FOOD_AND_DRINK',
    pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
    pending: false,
    created_at: '2025-10-25T10:00:00Z',
    updated_at: '2025-10-25T10:00:00Z',
  },
]

function configureQueries() {
  mockUseInfiniteTransactions.mockReturnValue({
    transactions: mockTransactions,
    fetchNextPage: mockFetchNextPage,
    hasNextPage: false,
    isFetchingNextPage: false,
    isLoading: false,
    isError: false,
    updateTransaction: mock(() => Promise.resolve()),
  })

  mockUseGetTransactions.mockReturnValue({
    data: { transactions: mockTransactions, total_count: 1, limit: 50, offset: 0, has_more: false },
    isLoading: false,
    error: null,
    refetch: mockRefetchTransactions,
  })

  mockGetTransactions.mockResolvedValue({
    transactions: mockTransactions,
    next_cursor: undefined,
    limit: 50,
  })

  mockUseGetUserTags.mockReturnValue({ data: [], isLoading: false, error: null })
  mockUseGetUserCategories.mockReturnValue({ data: [], isLoading: false, error: null })
  mockUseGetPlaidCategories.mockReturnValue({ data: [], isLoading: false, error: null })
  mockUseGetPlaidPrimaryCategories.mockReturnValue({ data: [], isLoading: false, error: null })
  mockUseGetAccounts.mockReturnValue({ data: { accounts: [] }, isLoading: false, error: null })

  mockUseCreateTag.mockReturnValue({ mutateAsync: mockCreateTagMutateAsync, isPending: false })
  mockUseCreateCategory.mockReturnValue({ mutateAsync: mockCreateCategoryMutateAsync, isPending: false })
  mockUseUpdateTransactionTags.mockReturnValue({ mutateAsync: mockUpdateTransactionTagsMutateAsync, isPending: false })
  mockUseUpdateTransactionCategory.mockReturnValue({ mutateAsync: mockUpdateTransactionCategoryMutateAsync, isPending: false })
}

describe('Category filter analytics', () => {
  beforeEach(() => {
    ;[
      mockUseGetTransactions,
      mockRefetchTransactions,
      mockUpdateTransactionTagsMutateAsync,
      mockUpdateTransactionCategoryMutateAsync,
      mockUseUpdateTransactionTags,
      mockUseUpdateTransactionCategory,
      mockUseGetUserTags,
      mockUseGetUserCategories,
      mockUseGetPlaidCategories,
      mockUseGetPlaidPrimaryCategories,
      mockUseGetAccounts,
      mockUseCreateTag,
      mockUseCreateCategory,
      mockUseInfiniteTransactions,
      mockFetchNextPage,
      mockGetTransactions,
    ].forEach((m) => m.mockClear())

    mockTrackEvent.mockClear()
    configureQueries()
  })

  it('fires CATEGORY_FILTER_APPLIED when pfc_primaries filter is applied', async () => {
    const user = userEvent.setup()
    const Wrapper = createWrapper()
    render(<TransactionsPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText(/transactions loaded/i)).toBeInTheDocument()
    })

    // Open the category filter popover via the trigger button in the column header
    const filterButton = screen.getByRole('button', { name: /filter by category/i })
    await user.click(filterButton)

    // Wait for the popover tree to appear
    await waitFor(() => {
      expect(screen.getByRole('tree')).toBeInTheDocument()
    })

    // Click a primary category treeitem to apply the filter
    const primaryTreeitem = screen.getAllByRole('treeitem')[0]
    await user.click(primaryTreeitem)

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.CATEGORY_FILTER_APPLIED)
    })
  })

  it('fires CATEGORY_FILTER_APPLIED when pfc_detaileds filter is applied', async () => {
    const user = userEvent.setup()
    const Wrapper = createWrapper()
    render(<TransactionsPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText(/transactions loaded/i)).toBeInTheDocument()
    })

    // Open the category filter popover
    const filterButton = screen.getByRole('button', { name: /filter by category/i })
    await user.click(filterButton)

    // Wait for the tree to render
    await waitFor(() => {
      expect(screen.getByRole('tree')).toBeInTheDocument()
    })

    // Expand the first primary to reveal its detailed children
    const expandButton = screen.getAllByRole('button', { name: /expand/i })[0]
    await user.click(expandButton)

    // After expanding, more treeitems appear — the children are detailed items
    await waitFor(() => {
      expect(screen.getAllByRole('treeitem').length).toBeGreaterThan(1)
    })

    // Click the second treeitem (a detailed child of the expanded primary)
    const detailedTreeitem = screen.getAllByRole('treeitem')[1]
    await user.click(detailedTreeitem)

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.CATEGORY_FILTER_APPLIED)
    })
  })

  it('fires CATEGORY_FILTER_CLEARED when category filter is cleared', async () => {
    const user = userEvent.setup()
    const Wrapper = createWrapper()
    render(<TransactionsPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText(/transactions loaded/i)).toBeInTheDocument()
    })

    // Open the category filter popover and select a primary category first
    const filterButton = screen.getByRole('button', { name: /filter by category/i })
    await user.click(filterButton)

    await waitFor(() => {
      expect(screen.getByRole('tree')).toBeInTheDocument()
    })

    const primaryTreeitem = screen.getAllByRole('treeitem')[0]
    await user.click(primaryTreeitem)

    // Click the Clear button to clear all category filters
    const clearButton = screen.getByRole('button', { name: /clear category filter/i })
    await user.click(clearButton)

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.CATEGORY_FILTER_CLEARED)
    })
  })
})
