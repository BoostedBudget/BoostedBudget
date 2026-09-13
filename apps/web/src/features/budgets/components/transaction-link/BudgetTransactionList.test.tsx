import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router'
import type { Transaction } from '../../../transactions/types'
import { mockUseGetUserCategories } from '@/test/mocks/categories-api'

type HookState = ReturnType<typeof import('../../hooks/useBudgetTransactions').useBudgetTransactions>
const createMockHookState = (overrides?: Partial<HookState>): HookState => ({
  transactions: [],
  totalCount: 0,
  totalAmount: 0,
  loading: false,
  loadingMore: false,
  error: null,
  hasMore: false,
  loadMore: mock(() => Promise.resolve()),
  refetch: mock(() => Promise.resolve()),
  ...overrides,
})

const mockUseBudgetTransactions = mock(() => createMockHookState())

mock.module('../../hooks/useBudgetTransactions', () => ({
  useBudgetTransactions: mockUseBudgetTransactions,
}))

import { BudgetTransactionList } from './BudgetTransactionList'

const mockTransaction: Transaction = {
  id: 'tx-1',
  user_id: 'user-1',
  account_id: 'account-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: -50.0,
  date: '2024-01-15T00:00:00Z',
  name: 'Coffee Shop',
  merchant_name: 'Starbucks',
  pending: false,
  created_at: '2024-01-15T00:00:00Z',
  updated_at: '2024-01-15T00:00:00Z',
}

const mockTransaction2: Transaction = {
  ...mockTransaction,
  id: 'tx-2',
  amount: -25.0,
  name: 'Lunch',
  merchant_name: 'Chipotle',
}

const renderWithRouter = (component: React.ReactElement) =>
  render(<BrowserRouter>{component}</BrowserRouter>)

describe('BudgetTransactionList', () => {
  beforeEach(() => {
    mockUseBudgetTransactions.mockClear()
  })

  it('preserves malformed currency markers throughout the masked expense breakdown', () => {
    mockUseBudgetTransactions.mockReturnValue(createMockHookState({
      transactions: [{ ...mockTransaction, amount: 50, budget_expense_amount: 30 }], totalCount: 1, totalAmount: 30,
    }))
    renderWithRouter(<BudgetTransactionList budgetId="budget-1" categoryId="category-1" currencyCode="U$D" />)
    expect(screen.queryByText('Total: U$D 30.00')).toHaveAttribute('data-ph-mask')
    expect(screen.getByText('Expense counted: U$D 30.00')).toHaveAttribute('data-ph-mask')
    expect(screen.getByText('Allocated to savings: U$D 20.00')).toHaveAttribute('data-ph-mask')
    expect(screen.getByText('-U$D 50.00').closest('[data-ph-mask]')).not.toBeNull()
  })

  describe('loading state', () => {
    it('should show loading spinner when loading', () => {
      mockUseBudgetTransactions.mockReturnValue(createMockHookState({ loading: true }))

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      expect(screen.getByRole('status')).toBeInTheDocument()
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })
  })

  describe('empty state', () => {
    it('should show empty message when no transactions', () => {
      mockUseBudgetTransactions.mockReturnValue(createMockHookState())

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      expect(screen.getByText(/no transactions/i)).toBeInTheDocument()
    })
  })

  describe('error state', () => {
    it('should show error message when error occurs', () => {
      const mockRefetch = mock(() => Promise.resolve())
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({ error: new Error('Failed to load'), refetch: mockRefetch })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
    })

    it('should call refetch when retry button clicked', () => {
      const mockRefetch = mock(() => Promise.resolve())
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({ error: new Error('Failed to load'), refetch: mockRefetch })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      fireEvent.click(screen.getByRole('button', { name: /retry/i }))
      expect(mockRefetch).toHaveBeenCalledTimes(1)
    })
  })

  describe('transaction list', () => {
    it('passes loaded custom ancestry to the production transaction cards', () => {
      mockUseGetUserCategories.mockReturnValue({ data: [
        { id: 'parent', name: 'Family phones', parent_pfc_primary: 'RENT_AND_UTILITIES' },
        { id: 'child', name: 'Kids phones', parent_id: 'parent' },
      ], isLoading: false, error: null } as unknown as ReturnType<typeof mockUseGetUserCategories>)
      mockUseBudgetTransactions.mockReturnValue(createMockHookState({
        transactions: [{ ...mockTransaction, category_override_kind: 'custom', custom_category_override_id: 'child', custom_category_override_display: 'Kids phones' }],
        totalCount: 1,
      }))
      renderWithRouter(<BudgetTransactionList budgetId="budget-1" categoryId="category-1" />)
      expect(screen.getByText('Rent & Utilities › Family phones › Kids phones').closest('[data-ph-mask]')).not.toBeNull()
    })

    it('should render transaction list with count and total', () => {
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({
          transactions: [mockTransaction, mockTransaction2],
          totalCount: 2,
          totalAmount: 75.0,
        })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      expect(screen.getByText(/transactions \(2\)/i)).toBeInTheDocument()
      expect(screen.getByText(/\$75\.00/)).toBeInTheDocument()
      expect(screen.getByText('Starbucks')).toBeInTheDocument()
      expect(screen.getByText('Chipotle')).toBeInTheDocument()
    })

    it('should render list with proper role', () => {
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({ transactions: [mockTransaction], totalCount: 1, totalAmount: 50.0 })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      expect(screen.getByRole('list')).toBeInTheDocument()
    })
  })

  describe('pagination', () => {
    it('passes the report snapshot version to the transaction hook', () => {
      mockUseBudgetTransactions.mockReturnValue(createMockHookState())
      renderWithRouter(
        <BudgetTransactionList
          budgetId="budget-1"
          categoryId="category-1"
          snapshotVersion="2026-09-12T12:00:00Z"
        />
      )
      expect(mockUseBudgetTransactions).toHaveBeenCalledWith('budget-1', 'category-1', {
        snapshotVersion: '2026-09-12T12:00:00Z',
      })
    })
    it('distinguishes gross spending, savings and counted expense in the report currency', () => {
      mockUseBudgetTransactions.mockReturnValue(createMockHookState({ transactions: [{ ...mockTransaction, amount: 100, budget_expense_amount: 70 }], totalCount: 1, totalAmount: 70 }))
      renderWithRouter(<BudgetTransactionList budgetId="budget-1" categoryId="category-1" currencyCode="CAD" />)
      expect(screen.getByText('-CA$100.00')).toBeInTheDocument()
      expect(screen.getByText('Expense counted: CA$70.00')).toBeInTheDocument()
      expect(screen.getByText('Allocated to savings: CA$30.00')).toBeInTheDocument()
      expect(screen.getByText('Total: CA$70.00')).toHaveAttribute('data-ph-mask')
    })
    it('should show Load More button when hasMore', () => {
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({
          transactions: [mockTransaction],
          totalCount: 10,
          totalAmount: 500.0,
          hasMore: true,
        })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      expect(screen.getByRole('button', { name: /load more/i })).toBeInTheDocument()
    })

    it('should not show Load More when no more', () => {
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({
          transactions: [mockTransaction],
          totalCount: 1,
          totalAmount: 50.0,
        })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      expect(screen.queryByRole('button', { name: /load more/i })).not.toBeInTheDocument()
    })

    it('should call loadMore when button clicked', () => {
      const mockLoadMore = mock(() => Promise.resolve())
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({
          transactions: [mockTransaction],
          totalCount: 10,
          totalAmount: 500.0,
          hasMore: true,
          loadMore: mockLoadMore,
        })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      fireEvent.click(screen.getByRole('button', { name: /load more/i }))
      expect(mockLoadMore).toHaveBeenCalledTimes(1)
    })

    it('should show loading on button when loadingMore', () => {
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({
          transactions: [mockTransaction],
          totalCount: 10,
          totalAmount: 500.0,
          hasMore: true,
          loadingMore: true,
        })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      const loadMoreButton = screen.getByRole('button', { name: /loading/i })
      expect(loadMoreButton).toBeInTheDocument()
      expect(loadMoreButton).toBeDisabled()
    })
  })

  describe('hook integration', () => {
    it('should pass budgetId and categoryId to hook', () => {
      mockUseBudgetTransactions.mockReturnValue(createMockHookState())

      renderWithRouter(
        <BudgetTransactionList budgetId="my-budget" categoryId="my-category" />
      )

      expect(mockUseBudgetTransactions).toHaveBeenCalledWith('my-budget', 'my-category', {
        snapshotVersion: undefined,
      })
    })
  })

  describe('accessibility', () => {
    it('should have accessible heading', () => {
      mockUseBudgetTransactions.mockReturnValue(
        createMockHookState({ transactions: [mockTransaction], totalCount: 1, totalAmount: 50.0 })
      )

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      )

      expect(screen.getByRole('heading', { level: 3 })).toBeInTheDocument()
    })
  })
})
