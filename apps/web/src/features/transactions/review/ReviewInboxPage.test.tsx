import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import type { ReactNode } from 'react'
import { ReviewInboxPage } from './ReviewInboxPage'
import type { Transaction } from '../types'
import {
  mockGetTransactions,
  mockUseGetTransactionReviewSummary,
  mockSetTransactionReviewedMutateAsync,
  mockUseSetTransactionReviewed,
  mockBulkSetTransactionReviewedMutateAsync,
  mockUseBulkSetTransactionReviewed,
  mockBulkUpdateTransactionCategoryMutateAsync,
  mockUseBulkUpdateTransactionCategory,
} from '@/test/mocks/transactions-api'
import { mockPosthogCapture } from '@/test/mocks/posthog'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

const mockTx: Transaction = {
  id: 'tx-1',
  user_id: 'user-1',
  account_id: 'acct-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: 25.00,
  date: '2026-05-01',
  name: 'Starbucks',
  merchant_name: 'Starbucks Coffee',
  pending: false,
  reviewed: false,
  category_source: 'plaid',
  created_at: '2026-05-01T10:00:00Z',
  updated_at: '2026-05-01T10:00:00Z',
}

const mockTx2: Transaction = {
  id: 'tx-2',
  user_id: 'user-1',
  account_id: 'acct-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: 100.00,
  date: '2026-05-02',
  name: 'Amazon',
  merchant_name: 'Amazon',
  pending: false,
  reviewed: false,
  category_source: 'plaid',
  created_at: '2026-05-02T10:00:00Z',
  updated_at: '2026-05-02T10:00:00Z',
}

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <BrowserRouter>{children}</BrowserRouter>
      </QueryClientProvider>
    )
  }
}

function configureBase(transactions: Transaction[] = []) {
  // Configure raw fetcher used by useInfiniteTransactions (async)
  mockGetTransactions.mockResolvedValue({
    transactions,
    next_cursor: undefined,
    limit: 30,
  })
  // Configure review summary (synchronous mock)
  mockUseGetTransactionReviewSummary.mockReturnValue({
    data: { unreviewed_count: transactions.length, reviewed_by_rule_count: 0, reviewed_by_manual_count: 0, reviewed_by_plaid_count: 0 },
    isLoading: false,
    isError: false,
  })
  // Configure mutation hooks (synchronous)
  mockUseSetTransactionReviewed.mockReturnValue({
    mutateAsync: mockSetTransactionReviewedMutateAsync,
    isPending: false,
  })
  mockUseBulkSetTransactionReviewed.mockReturnValue({
    mutateAsync: mockBulkSetTransactionReviewedMutateAsync,
    isPending: false,
  })
  mockUseBulkUpdateTransactionCategory.mockReturnValue({
    mutateAsync: mockBulkUpdateTransactionCategoryMutateAsync,
    isPending: false,
  })
}

describe('ReviewInboxPage', () => {
  beforeEach(() => {
    mockGetTransactions.mockClear()
    mockUseGetTransactionReviewSummary.mockClear()
    mockSetTransactionReviewedMutateAsync.mockClear()
    mockBulkSetTransactionReviewedMutateAsync.mockClear()
    mockBulkUpdateTransactionCategoryMutateAsync.mockClear()
    mockPosthogCapture.mockClear()
  })

  it('fires REVIEW_INBOX_VIEWED analytics on mount', () => {
    configureBase()
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    expect(mockPosthogCapture).toHaveBeenCalledWith(ANALYTICS_EVENTS.REVIEW_INBOX_VIEWED, undefined)
  })

  it('shows "All caught up!" empty state when unreviewed preset has 0 transactions', async () => {
    configureBase([])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByText('All caught up!')).toBeInTheDocument()
    })
  })

  it('shows generic empty state when non-default preset has 0 transactions', async () => {
    configureBase([])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    // Switch to "Reviewed" preset
    fireEvent.click(screen.getByText('Reviewed'))
    await waitFor(() => {
      expect(screen.getByText('No transactions found')).toBeInTheDocument()
    })
  })

  it('renders transaction rows in the desktop table', async () => {
    configureBase([mockTx, mockTx2])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      expect(screen.getByText('Amazon')).toBeInTheDocument()
    })
  })

  it('shows unreviewed count in header from review summary', async () => {
    configureBase([mockTx, mockTx2])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByText('2 unreviewed transactions')).toBeInTheDocument()
    })
  })

  it('ReviewBulkActionBar is hidden when no rows are selected', async () => {
    configureBase([mockTx])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())
    // Bulk bar only shows when selectedCount > 0
    expect(screen.queryByText('1 selected')).toBeNull()
  })

  it('ReviewBulkActionBar appears after selecting a row', async () => {
    configureBase([mockTx])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())
    // Select the row checkbox (select-all is first, row checkbox is second)
    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[1])
    expect(screen.getByText('1 selected')).toBeInTheDocument()
  })

  it('handleBulkMarkReviewed calls bulkSetReviewed with selected ids', async () => {
    configureBase([mockTx, mockTx2])
    mockBulkSetTransactionReviewedMutateAsync.mockResolvedValue({})
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())

    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[1]) // select tx-1
    fireEvent.click(checkboxes[2]) // select tx-2

    fireEvent.click(screen.getByLabelText('Mark selected as reviewed'))

    await waitFor(() => {
      expect(mockBulkSetTransactionReviewedMutateAsync).toHaveBeenCalledWith({
        data: expect.objectContaining({ reviewed: true }),
      })
    })
  })

  it('handleBulkMarkUnreviewed calls bulkSetReviewed with reviewed=false', async () => {
    configureBase([mockTx])
    mockBulkSetTransactionReviewedMutateAsync.mockResolvedValue({})
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())

    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[1])

    fireEvent.click(screen.getByLabelText('Mark selected as unreviewed'))

    await waitFor(() => {
      expect(mockBulkSetTransactionReviewedMutateAsync).toHaveBeenCalledWith({
        data: expect.objectContaining({ reviewed: false }),
      })
    })
  })

  it('select-all checkbox selects all loaded transactions', async () => {
    configureBase([mockTx, mockTx2])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('checkbox', { name: /select all/i }))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })
})
