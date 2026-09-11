import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router'
import type { ReactNode } from 'react'
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
import { mockTrackEvent } from '@/test/mocks/track-event'
import { mockToastSuccess } from '@/test/mocks/sonner'

const REVIEW_ANALYTICS_EVENTS = {
  REVIEW_INBOX_VIEWED: 'review_inbox_viewed',
  REVIEW_ALL_CAUGHT_UP: 'review_all_caught_up',
  REVIEW_PRESET_CHANGED: 'review_preset_changed',
  TRANSACTION_APPROVED: 'transaction_approved',
  TRANSACTION_REVIEWED: 'transaction_reviewed',
  TRANSACTION_SKIPPED: 'transaction_skipped',
  REVIEW_UNDO: 'review_undo',
  REVIEW_BULK_RECATEGORIZED: 'review_bulk_recategorized',
} as const


import { ReviewInboxPage } from './ReviewInboxPage'

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

function setDesktopViewport() {
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: 1024 })
  window.dispatchEvent(new Event('resize'))
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
    setDesktopViewport()
    mockGetTransactions.mockReset()
    mockUseGetTransactionReviewSummary.mockReset()
    mockSetTransactionReviewedMutateAsync.mockReset()
    mockUseSetTransactionReviewed.mockReset()
    mockBulkSetTransactionReviewedMutateAsync.mockReset()
    mockUseBulkSetTransactionReviewed.mockReset()
    mockBulkUpdateTransactionCategoryMutateAsync.mockReset()
    mockUseBulkUpdateTransactionCategory.mockReset()
    mockTrackEvent.mockReset()
    mockToastSuccess.mockReset()
  })

  it('fires REVIEW_INBOX_VIEWED analytics on mount', () => {
    configureBase()
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    expect(mockTrackEvent).toHaveBeenCalledWith(REVIEW_ANALYTICS_EVENTS.REVIEW_INBOX_VIEWED)
  })

  it('shows "All caught up!" empty state and fires REVIEW_ALL_CAUGHT_UP when unreviewed preset has 0 transactions', async () => {
    configureBase([])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByText('All caught up!')).toBeInTheDocument()
    })
    expect(mockTrackEvent).toHaveBeenCalledWith(REVIEW_ANALYTICS_EVENTS.REVIEW_ALL_CAUGHT_UP)
  })

  it('does not show "All caught up!" when transaction fetch errors', async () => {
    mockGetTransactions.mockRejectedValue(new Error('Network error'))
    mockUseGetTransactionReviewSummary.mockReturnValue({
      data: null,
      isLoading: false,
      isError: false,
    })
    mockUseSetTransactionReviewed.mockReturnValue({ mutateAsync: mockSetTransactionReviewedMutateAsync, isPending: false })
    mockUseBulkSetTransactionReviewed.mockReturnValue({ mutateAsync: mockBulkSetTransactionReviewedMutateAsync, isPending: false })
    mockUseBulkUpdateTransactionCategory.mockReturnValue({ mutateAsync: mockBulkUpdateTransactionCategoryMutateAsync, isPending: false })
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    // Wait for the fetch to be attempted (ensures we're past the initial loading state)
    await waitFor(() => expect(mockGetTransactions).toHaveBeenCalled())
    expect(screen.queryByText('All caught up!')).toBeNull()
    expect(mockTrackEvent).not.toHaveBeenCalledWith(REVIEW_ANALYTICS_EVENTS.REVIEW_ALL_CAUGHT_UP)
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

  // #1631: the date column printed tx.date raw, so users saw whatever the wire said -- a bare
  // "2026-05-01T00:00:00Z" before the backend fix. Routed through the shared formatDate, which
  // splits on the T and parses component-wise, so it is correct for both wire shapes. Asserting
  // the formatted string and the absence of the raw one: asserting only the raw value's presence
  // is what let this render ship unformatted.
  it('formats the date column rather than printing the wire value raw', async () => {
    configureBase([mockTx])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByText('May 1, 2026')).toBeInTheDocument()
    })
    expect(screen.queryByText('2026-05-01')).not.toBeInTheDocument()
  })

  it('formats a legacy timestamp-shaped date without shifting the day', async () => {
    configureBase([{ ...mockTx, date: '2026-05-01T00:00:00Z' }])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByText('May 1, 2026')).toBeInTheDocument()
    })
    expect(screen.queryByText('2026-05-01T00:00:00Z')).not.toBeInTheDocument()
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

  it('triage approve shows undo toast; undo action reverts to unreviewed using closure-captured id', async () => {
    configureBase([mockTx])
    mockSetTransactionReviewedMutateAsync.mockResolvedValue({})
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())

    // Open triage view
    fireEvent.click(screen.getByRole('cell', { name: 'Starbucks Coffee' }))
    await waitFor(() => expect(screen.getByRole('button', { name: /approve/i })).toBeInTheDocument())

    // Approve
    fireEvent.click(screen.getByRole('button', { name: /approve/i }))

    // Toast with undo action appears
    await waitFor(() =>
      expect(mockToastSuccess).toHaveBeenCalledWith(
        'Marked as reviewed',
        expect.objectContaining({ action: expect.objectContaining({ label: 'Undo' }) })
      )
    )

    // Extract closure-captured undo action and invoke it
    const [, opts] = mockToastSuccess.mock.calls[0] as [string, { action: { onClick: () => Promise<void> } }]
    await opts.action.onClick()

    // setReviewed called with tx-1 and false (not a stale ref to a different tx)
    expect(mockSetTransactionReviewedMutateAsync).toHaveBeenLastCalledWith({ id: 'tx-1', data: { reviewed: false } })
  })

  it('opens bulk recategorize dialog with correct title after row selection', async () => {
    configureBase([mockTx])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())

    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[1])

    fireEvent.click(screen.getByRole('button', { name: 'Recategorize selected' }))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())
    expect(screen.getByText('Recategorize 1 transaction')).toBeInTheDocument()
  })

  it('closes bulk recategorize dialog on Cancel', async () => {
    configureBase([mockTx])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())

    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[1])
    fireEvent.click(screen.getByRole('button', { name: 'Recategorize selected' }))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('shows effective_merchant_name in the merchant column when override is present', async () => {
    const txWithOverride: Transaction = {
      ...mockTx,
      merchant_name: 'STARBKS #99',
      effective_merchant_name: 'Starbucks',
    }
    configureBase([txWithOverride])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks')).toBeInTheDocument())
    expect(screen.queryByText('STARBKS #99')).not.toBeInTheDocument()
  })
})
