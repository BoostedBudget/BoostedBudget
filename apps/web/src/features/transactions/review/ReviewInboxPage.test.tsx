import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react'
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
  mockUpdateTransactionTagsMutateAsync,
  mockUseUpdateTransactionTags,
} from '@/test/mocks/transactions-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { mockToastSuccess } from '@/test/mocks/sonner'
import { mockUseGetPlaidDetailedCategories } from '@/test/mocks/categories-api'
import { mockUseGetAccounts, mockRefetchAccounts } from '@/test/mocks/accounts-api'

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

function createWrapper(qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })) {
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
  it('renders complete masked row context and opens the real editor from triage', async () => {
    configureBase([{ ...mockTx, amount: -25, tags: ['Holiday'], pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_COFFEE' }])
    mockUseGetAccounts.mockReturnValue({ data: { accounts: [{ id: 'acct-1', name: 'Closed checking', status: 'removed' }] }, isLoading: false, isError: false, error: null, refetch: mockRefetchAccounts } as never)
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    const merchant = await screen.findByRole('cell', { name: /Starbucks Coffee/ })
    for (const value of ['Closed checking', 'Food & Drink', 'Coffee', '+$25.00']) {
      expect(screen.getByRole('cell', { name: value })).toHaveAttribute('data-ph-mask')
    }
    expect(within(merchant).getByText('Holiday')).toBeInTheDocument()
    fireEvent.click(merchant)
    expect(screen.getByRole('button', { name: /approve/i })).toBeInTheDocument()
    const edit = screen.getByRole('button', { name: 'Edit details' })
    fireEvent.keyDown(edit, { key: 'Enter' })
    expect(mockSetTransactionReviewedMutateAsync).not.toHaveBeenCalled()
    fireEvent.click(edit)
    expect(screen.getByRole('dialog', { name: 'Edit Transaction' })).toBeInTheDocument()
    expect(mockTrackEvent).toHaveBeenCalledWith('transaction_editor_opened', { source: 'review' })
    fireEvent.keyDown(document, { key: 'e' })
    expect(mockSetTransactionReviewedMutateAsync).not.toHaveBeenCalled()
  })

  it.each([
    ['reorder', [mockTx2, mockTx], 'Starbucks Coffee'],
    ['first removal', [mockTx2], 'Amazon'],
    ['empty', [], null],
  ] as const)('reconciles selected identity on %s', async (_case, next, expected) => {
    configureBase([mockTx, mockTx2])
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    render(<ReviewInboxPage />, { wrapper: createWrapper(qc) })
    fireEvent.click(await screen.findByRole('cell', { name: 'Starbucks Coffee' }))
    mockGetTransactions.mockResolvedValue({ transactions: next, limit: 30 })
    await act(async () => { await qc.invalidateQueries() })
    if (expected) {
      await waitFor(() => expect(screen.getAllByText(expected)).toHaveLength(2))
      if (_case === 'reorder') await waitFor(() => expect(screen.getByText('2 / 2')).toBeInTheDocument())
      expect(screen.getByRole('button', { name: 'Edit details' })).toBeInTheDocument()
    } else await waitFor(() => expect(screen.queryByRole('button', { name: /approve/i })).toBeNull())
  })

  it('holds the editor snapshot through a refetch that removes its row', async () => {
    configureBase([mockTx, mockTx2])
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    render(<ReviewInboxPage />, { wrapper: createWrapper(qc) })
    fireEvent.click(await screen.findByRole('cell', { name: 'Starbucks Coffee' }))
    fireEvent.click(screen.getByRole('button', { name: 'Edit details' }))
    mockGetTransactions.mockResolvedValue({ transactions: [mockTx2], limit: 30 })
    await act(async () => { await qc.invalidateQueries() })
    expect(within(screen.getByRole('dialog')).getByText('Starbucks Coffee')).toBeInTheDocument()
    expect(within(screen.getByRole('dialog')).queryByText('Amazon')).toBeNull()
  })

  it('restores Edit details focus on Escape and retains letter shortcuts without Enter approval', async () => {
    configureBase([mockTx])
    mockSetTransactionReviewedMutateAsync.mockResolvedValue({})
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    fireEvent.click(await screen.findByRole('cell', { name: 'Starbucks Coffee' }))
    const edit = screen.getByRole('button', { name: 'Edit details' })
    edit.focus()
    fireEvent.click(edit)
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(edit))
    fireEvent.keyDown(edit, { key: 'Enter' })
    expect(mockSetTransactionReviewedMutateAsync).not.toHaveBeenCalled()
    fireEvent.keyDown(edit, { key: 'e' })
    await waitFor(() => expect(mockSetTransactionReviewedMutateAsync).toHaveBeenCalledTimes(1))
  })

  it.each(['middle', 'last'])('selects the successor or previous row after %s removal', async position => {
    const third = { ...mockTx, id: 'tx-3', name: 'Third', merchant_name: 'Third' }
    configureBase([mockTx, mockTx2, third])
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<ReviewInboxPage />, { wrapper: createWrapper(qc) })
    fireEvent.click(await screen.findByRole('cell', { name: position === 'middle' ? 'Amazon' : 'Third' }))
    mockGetTransactions.mockResolvedValue({ transactions: position === 'middle' ? [mockTx, third] : [mockTx, mockTx2], limit: 30 })
    await act(async () => { await qc.invalidateQueries() })
    await waitFor(() => expect(screen.getAllByText(position === 'middle' ? 'Third' : 'Amazon')).toHaveLength(2))
  })

  it('saves the original editor ID after pending refetch removal and restores safe focus', async () => {
    configureBase([{ ...mockTx, tags: ['Trip'] }, mockTx2])
    let resolve!: (value: Transaction) => void
    mockUpdateTransactionTagsMutateAsync.mockImplementationOnce(() => new Promise<Transaction>(r => { resolve = r }))
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<ReviewInboxPage />, { wrapper: createWrapper(qc) })
    fireEvent.click(await screen.findByRole('cell', { name: /Starbucks Coffee/ }))
    const edit = screen.getByRole('button', { name: 'Edit details' })
    edit.focus()
    fireEvent.click(edit)
    fireEvent.click(screen.getByRole('button', { name: 'Remove Trip' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    mockGetTransactions.mockResolvedValue({ transactions: [mockTx2], limit: 30 })
    await act(async () => { await qc.invalidateQueries() })
    await waitFor(() => expect(screen.queryByRole('cell', { name: /Starbucks Coffee/ })).toBeNull())
    expect(within(screen.getByRole('dialog')).getByText('Starbucks Coffee')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    fireEvent.keyDown(document, { key: 'Enter' })
    expect(mockSetTransactionReviewedMutateAsync).not.toHaveBeenCalled()
    expect(mockUpdateTransactionTagsMutateAsync).toHaveBeenCalledWith({ id: 'tx-1', data: { tags: [] } })
    await act(async () => resolve({ ...mockTx, tags: [] }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(edit))
    fireEvent.keyDown(edit, { key: 'Enter' })
    expect(mockSetTransactionReviewedMutateAsync).not.toHaveBeenCalled()
  })

  it('prevents duplicate approve requests before a mutation hook rerenders', async () => {
    configureBase([mockTx])
    let resolve!: () => void
    mockSetTransactionReviewedMutateAsync.mockImplementation(() => new Promise<void>(r => { resolve = r }) as never)
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    fireEvent.click(await screen.findByRole('cell', { name: 'Starbucks Coffee' }))
    fireEvent.keyDown(document, { key: 'e' })
    fireEvent.keyDown(document, { key: 'e' })
    expect(mockSetTransactionReviewedMutateAsync).toHaveBeenCalledTimes(1)
    await act(async () => resolve())
  })

  it.each(['Next transaction', 'Previous transaction', 'Close triage'])('guards same-tick approve then %s', async name => {
    configureBase([mockTx, mockTx2, { ...mockTx, id: 'tx-3', name: 'Third', merchant_name: 'Third' }])
    let resolve!: () => void
    mockSetTransactionReviewedMutateAsync.mockImplementationOnce(() => new Promise<void>(r => { resolve = r }) as never)
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    fireEvent.click(await screen.findByRole('cell', { name: 'Amazon' }))
    const approve = screen.getByRole('button', { name: /approve/i })
    const navigate = screen.getByRole('button', { name })
    act(() => { approve.click(); navigate.click() })
    expect(screen.getByText('2 / 3')).toBeInTheDocument()
    expect(screen.getAllByText('Amazon')).toHaveLength(2)
    await act(async () => resolve())
  })

  it('retains paging and clears selection when switching presets', async () => {
    configureBase([mockTx])
    mockGetTransactions.mockResolvedValueOnce({ transactions: [mockTx], next_cursor: 'next-page', limit: 30 })
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await screen.findByRole('cell', { name: 'Starbucks Coffee' })
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    mockGetTransactions.mockResolvedValueOnce({ transactions: [mockTx2], limit: 30 })
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }))
    await screen.findByRole('cell', { name: 'Amazon' })
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(mockGetTransactions).toHaveBeenCalledWith(expect.objectContaining({ cursor: 'next-page' }), undefined, expect.anything())
    fireEvent.click(screen.getByRole('button', { name: 'Reviewed' }))
    await waitFor(() => expect(mockGetTransactions).toHaveBeenLastCalledWith(expect.objectContaining({ reviewed: 'true' }), undefined, expect.anything()))
    expect(screen.queryByText('1 selected')).toBeNull()
  })

  it('bounds mobile triage to a scrollable viewport area', async () => {
    configureBase([mockTx])
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    const edit = await screen.findByRole('button', { name: 'Edit details' })
    const wrapper = edit.closest('.fixed')
    expect(wrapper).toHaveClass('max-h-[calc(100dvh-8rem)]', 'overflow-y-auto', 'md:max-h-none')
  })

  it.each(['triage', 'bulk'])('gates stale %s undo while editing and serializes undo retries', async mode => {
    configureBase([mockTx])
    mockSetTransactionReviewedMutateAsync.mockResolvedValue({})
    mockBulkSetTransactionReviewedMutateAsync.mockResolvedValue({})
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    const cell = await screen.findByRole('cell', { name: 'Starbucks Coffee' })
    if (mode === 'bulk') {
      fireEvent.click(screen.getAllByRole('checkbox')[1])
      fireEvent.click(screen.getByRole('button', { name: 'Mark selected as reviewed' }))
    } else {
      fireEvent.click(cell)
      fireEvent.click(screen.getByRole('button', { name: /approve/i }))
    }
    await waitFor(() => expect(mockToastSuccess).toHaveBeenCalled())
    const [, options] = mockToastSuccess.mock.calls[0] as unknown as [string, { action: { onClick: () => Promise<void> } }]
    const undo = options.action.onClick
    fireEvent.click(cell)
    fireEvent.click(screen.getByRole('button', { name: 'Edit details' }))
    const mutation = mode === 'bulk' ? mockBulkSetTransactionReviewedMutateAsync : mockSetTransactionReviewedMutateAsync
    mutation.mockClear()
    await act(async () => undo())
    expect(mutation).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    let resolve!: () => void
    mutation.mockImplementationOnce(() => new Promise<void>(r => { resolve = r }) as never)
    let pending!: Promise<void>
    act(() => { pending = undo(); void undo() })
    expect(mutation).toHaveBeenCalledTimes(1)
    await act(async () => { resolve(); await pending })
  })

  beforeEach(() => {
    mockUpdateTransactionTagsMutateAsync.mockReset()
    mockUseUpdateTransactionTags.mockReturnValue({ mutateAsync: mockUpdateTransactionTagsMutateAsync, isPending: false })
    mockUseGetAccounts.mockReturnValue({ data: undefined, isLoading: false, isError: false, error: null, refetch: mockRefetchAccounts })
    mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [], isLoading: false, error: null })
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
    expect(screen.getByText('Bank category')).toBeInTheDocument()
    expect(within(screen.getByRole('dialog')).queryByText('Uncategorized')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Change category' }))
    fireEvent.click(screen.getByRole('button', { name: 'Reset category override' }))
    expect(screen.getByText('Bank category')).toBeInTheDocument()
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

  it('bulk Apply stays disabled during search and submits both selected bank enums', async () => {
    configureBase([mockTx])
    mockBulkUpdateTransactionCategoryMutateAsync.mockResolvedValue({})
    mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [{ enum: 'RENT_AND_UTILITIES_TELEPHONE', display: 'Telephone', primary_enum: 'RENT_AND_UTILITIES', primary_display: 'Rent & Utilities' }], isLoading: false, error: null })
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    fireEvent.click(screen.getByRole('button', { name: 'Recategorize selected' }))
    expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Change category' }))
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'telephone' } })
    expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled()
    fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Enter' })
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() => expect(mockBulkUpdateTransactionCategoryMutateAsync).toHaveBeenCalledWith({ data: { transaction_ids: ['tx-1'], kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_TELEPHONE' } }))
  })

  for (const target of ['search', 'new category name'] as const) {
    it(`Escape from bulk category ${target} preserves the pending selection`, async () => {
      configureBase([mockTx])
      mockBulkUpdateTransactionCategoryMutateAsync.mockResolvedValue({ requested: 1, updated: 1 })
      mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [{ enum: 'RENT_AND_UTILITIES_TELEPHONE', display: 'Telephone', primary_enum: 'RENT_AND_UTILITIES', primary_display: 'Rent & Utilities' }], isLoading: false, error: null })
      render(<ReviewInboxPage />, { wrapper: createWrapper() })
      await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())
      fireEvent.click(screen.getAllByRole('checkbox')[1])
      fireEvent.click(screen.getByRole('button', { name: 'Recategorize selected' }))
      fireEvent.click(screen.getByRole('button', { name: 'Change category' }))
      fireEvent.click(screen.getByRole('option', { name: 'Telephone' }))
      fireEvent.click(screen.getByRole('button', { name: 'Change category' }))
      const search = screen.getByRole('combobox', { name: 'Search categories' })
      await waitFor(() => expect(search).toHaveFocus())
      let input = search
      if (target === 'new category name') {
        fireEvent.click(screen.getByRole('button', { name: /create new category/i }))
        input = screen.getByRole('textbox', { name: 'Category name' })
        input.focus()
      }
      fireEvent.keyDown(input, { key: 'Escape' })
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.queryByRole('combobox', { name: 'Search categories' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Change category' })).toHaveFocus()
      expect(screen.getByText('Rent & Utilities › Telephone')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Apply' })).toBeEnabled()
      expect(mockBulkUpdateTransactionCategoryMutateAsync).not.toHaveBeenCalled()
      fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
      await waitFor(() => expect(mockBulkUpdateTransactionCategoryMutateAsync).toHaveBeenCalledWith({ data: { transaction_ids: ['tx-1'], kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_TELEPHONE' } }))
    })
  }

  it('Escape dismisses the bulk dialog when the category picker is closed', async () => {
    configureBase([mockTx])
    render(<ReviewInboxPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument())
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    fireEvent.click(screen.getByRole('button', { name: 'Recategorize selected' }))
    fireEvent.keyDown(screen.getByRole('button', { name: 'Change category' }), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
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
