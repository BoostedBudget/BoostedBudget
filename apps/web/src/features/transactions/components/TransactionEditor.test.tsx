import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { ReadOnlyProvider } from '@/auth/ReadOnlyContext'
import { TransactionEditor } from './TransactionEditor'
import type { Transaction } from '../types'
import {
  mockUpdateTransactionCategoryMutateAsync,
  mockSetTransactionReviewedMutateAsync,
  mockUpdateTransactionTagsMutateAsync,
} from '@/test/mocks/transactions-api'

function createWrapper() {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    )
  }
}

const baseTransaction: Transaction = {
  id: 'txn-1',
  user_id: 'user-1',
  account_id: 'acc-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: -12.45,
  date: '2024-01-15',
  name: 'KFC',
  pending: false,
  created_at: '2024-01-15T00:00:00Z',
  updated_at: '2024-01-15T00:00:00Z',
}

function renderEditor(transaction: Transaction) {
  const Wrapper = createWrapper()
  return render(
    <Wrapper>
      <ReadOnlyProvider isReadOnly={false}>
        <TransactionEditor
          transaction={transaction}
          availableTags={[]}
          availableCategories={[]}
          isOpen={true}
          onClose={() => {}}
          onSave={() => {}}
        />
      </ReadOnlyProvider>
    </Wrapper>
  )
}

describe('TransactionEditor', () => {
  for (const target of ['search', 'new category name'] as const) {
    it(`Escape from category ${target} closes only the picker and preserves the editor draft`, async () => {
      const onClose = mock(() => {})
      function Harness() {
        const [open, setOpen] = useState(true)
        return <TransactionEditor transaction={{ ...baseTransaction, tags: ['Trip'] }} availableTags={[]} availableCategories={[]} isOpen={open} onClose={() => { onClose(); setOpen(false) }} onSave={() => {}} />
      }
      render(<ReadOnlyProvider isReadOnly={false}><Harness /></ReadOnlyProvider>, { wrapper: createWrapper() })
      fireEvent.click(screen.getByRole('button', { name: 'Remove Trip' }))
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
      expect(onClose).not.toHaveBeenCalled()
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.queryByRole('combobox', { name: 'Search categories' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Change category' })).toHaveFocus()
      expect(screen.getByText('No tags')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    })
  }

  for (const dismissal of ['Escape', 'Cancel', 'Save'] as const) {
    it(`restores actual focus to the opener after ${dismissal}`, async () => {
      mockUpdateTransactionTagsMutateAsync.mockResolvedValue({ ...baseTransaction, tags: [] })
      function Harness() {
        const [open, setOpen] = useState(false)
        return <><button onClick={() => setOpen(true)}>Edit selected transaction</button>
          <TransactionEditor transaction={{ ...baseTransaction, tags: ['Trip'] }} availableTags={[]} availableCategories={[]} isOpen={open} onClose={() => setOpen(false)} onSave={() => {}} />
        </>
      }
      const Wrapper = createWrapper()
      render(<ReadOnlyProvider isReadOnly={false}><Harness /></ReadOnlyProvider>, { wrapper: Wrapper })
      const opener = screen.getByRole('button', { name: 'Edit selected transaction' })
      opener.focus()
      fireEvent.click(opener)
      await waitFor(() => expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true))
      if (dismissal === 'Escape') fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
      else {
        if (dismissal === 'Save') fireEvent.click(screen.getByRole('button', { name: 'Remove Trip' }))
        fireEvent.click(screen.getByRole('button', { name: dismissal }))
      }
      await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
      await waitFor(() => expect(document.activeElement === opener).toBe(true))
    })
  }

  it('focuses the main region when saving removes the opener from a filtered list', async () => {
    mockUpdateTransactionTagsMutateAsync.mockResolvedValue({ ...baseTransaction, tags: [] })
    function Harness() {
      const [open, setOpen] = useState(false)
      const [removed, setRemoved] = useState(false)
      return <main>{!removed && <button onClick={() => setOpen(true)}>Edit selected transaction</button>}
        <TransactionEditor transaction={{ ...baseTransaction, tags: ['Trip'] }} availableTags={[]} availableCategories={[]} isOpen={open} onClose={() => setOpen(false)} onSave={() => setRemoved(true)} />
      </main>
    }
    const Wrapper = createWrapper()
    render(<ReadOnlyProvider isReadOnly={false}><Harness /></ReadOnlyProvider>, { wrapper: Wrapper })
    const opener = screen.getByRole('button', { name: 'Edit selected transaction' })
    opener.focus()
    fireEvent.click(opener)
    fireEvent.click(screen.getByRole('button', { name: 'Remove Trip' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(document.activeElement === screen.getByRole('main')).toBe(true))
  })

  it('serializes review requests and keeps a failed review open for retry', async () => {
    let reject!: (error: Error) => void
    mockSetTransactionReviewedMutateAsync.mockClear()
    mockSetTransactionReviewedMutateAsync.mockImplementationOnce(() => new Promise<object>((_resolve, fail) => { reject = fail }))
    const onSave = mock(() => {})
    const onClose = mock(() => {})
    const Wrapper = createWrapper()
    render(<ReadOnlyProvider isReadOnly={false}><TransactionEditor transaction={baseTransaction} account={{ name: 'Checking 1234' }} availableTags={[]} availableCategories={[]} isOpen onClose={onClose} onSave={onSave} /></ReadOnlyProvider>, { wrapper: Wrapper })
    expect(screen.getByText('Checking 1234').hasAttribute('data-ph-mask')).toBe(true)
    const review = screen.getByRole('button', { name: 'Mark reviewed' })
    act(() => { review.click(); review.click() })
    expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Add tag' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(mockSetTransactionReviewedMutateAsync).toHaveBeenCalledTimes(1)
    await act(async () => reject(new Error('offline')))
    expect(screen.getByRole('alert').textContent).toContain('Failed to update review status')
    expect(onSave).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Mark reviewed' })).not.toBeDisabled()
  })
  it('invalidates generated detail and budget views after review without replaying a stale transaction', async () => {
    const client = new QueryClient()
    const detail = ['/api/v1/transactions/txn-1']
    const budget = ['/api/v1/budgets/b1/categories/c1/transactions']
    client.setQueryData(detail, baseTransaction)
    client.setQueryData(budget, { transactions: [baseTransaction] })
    mockSetTransactionReviewedMutateAsync.mockResolvedValue({})
    render(<QueryClientProvider client={client}><ReadOnlyProvider isReadOnly={false}><TransactionEditor transaction={baseTransaction} availableTags={[]} availableCategories={[]} isOpen onClose={() => {}} onSave={() => {}} /></ReadOnlyProvider></QueryClientProvider>)
    fireEvent.click(screen.getByRole('button', { name: 'Mark reviewed' }))
    await waitFor(() => expect(client.getQueryState(detail)?.isInvalidated).toBe(true))
    expect(client.getQueryState(budget)?.isInvalidated).toBe(true)
  })
  it('seeds a new ID but preserves drafts when the same transaction refetches', () => {
    const Wrapper = createWrapper()
    const view = (transaction: Transaction) => <ReadOnlyProvider isReadOnly={false}><TransactionEditor transaction={transaction} availableTags={[]} availableCategories={[]} isOpen onClose={() => {}} onSave={() => {}} /></ReadOnlyProvider>
    const { rerender } = render(view({ ...baseTransaction, tags: ['Original'] }), { wrapper: Wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Remove Original' }))
    rerender(view({ ...baseTransaction, tags: ['Server refresh'] }))
    expect(screen.getByText('No tags')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).not.toBeDisabled()
    rerender(view({ ...baseTransaction, id: 'txn-2', tags: ['New transaction'] }))
    expect(screen.getByText('New transaction')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('serializes tag-only saves and prevents competing review and every dismissal while pending', async () => {
    let resolve!: (transaction: Transaction) => void
    mockUpdateTransactionTagsMutateAsync.mockClear()
    mockUpdateTransactionCategoryMutateAsync.mockClear()
    mockSetTransactionReviewedMutateAsync.mockClear()
    mockUpdateTransactionTagsMutateAsync.mockImplementationOnce(() => new Promise<Transaction>(done => { resolve = done }))
    const onClose = mock(() => {})
    const Wrapper = createWrapper()
    render(<ReadOnlyProvider isReadOnly={false}><TransactionEditor transaction={{ ...baseTransaction, tags: ['Trip'] }} availableTags={[]} availableCategories={[]} isOpen onClose={onClose} onSave={() => {}} /></ReadOnlyProvider>, { wrapper: Wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Remove Trip' }))
    const save = screen.getByRole('button', { name: 'Save' })
    act(() => { save.click(); save.click() })
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Mark reviewed' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    fireEvent.pointerDown(document.body)
    expect(onClose).not.toHaveBeenCalled()
    expect(mockUpdateTransactionTagsMutateAsync).toHaveBeenCalledTimes(1)
    expect(mockSetTransactionReviewedMutateAsync).not.toHaveBeenCalled()
    await act(async () => resolve({ ...baseTransaction, tags: [] }))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(mockUpdateTransactionCategoryMutateAsync).not.toHaveBeenCalled()
  })

  it('shows a shared contextual dialog with a masked signed amount', () => {
    renderEditor({ ...baseTransaction, pending: true })
    expect(screen.getByRole('dialog').getAttribute('data-slot')).toBe('dialog-content')
    expect(screen.getByText('+$12.45')).toHaveAttribute('data-ph-mask')
    expect(screen.getByText('Pending')).toBeInTheDocument()
    expect(screen.getByText(/Jan 15, 2024/)).toBeInTheDocument()
  })

  it('retains the category draft after partial success and retries only the failed category', async () => {
    const client = new QueryClient()
    const key = ['transactions', 'infinite', { reviewed: false }]
    client.setQueryData(key, { pages: [{ transactions: [{ ...baseTransaction, source: 'banking' }] }], pageParams: [null] })
    mockUpdateTransactionTagsMutateAsync.mockClear()
    mockUpdateTransactionTagsMutateAsync.mockResolvedValue({ ...baseTransaction, tags: [] })
    mockUpdateTransactionCategoryMutateAsync.mockClear()
    mockUpdateTransactionCategoryMutateAsync.mockRejectedValueOnce(new Error('offline'))
    mockUpdateTransactionCategoryMutateAsync.mockResolvedValue({ ...baseTransaction, tags: [], category_override_kind: 'pfc', pfc_primary_override: 'TRANSPORTATION' })
    const onSave = mock(() => {})
    const onClose = mock(() => {})
    render(<QueryClientProvider client={client}><ReadOnlyProvider isReadOnly={false}>
      <TransactionEditor transaction={{ ...baseTransaction, tags: ['Trip'], pfc_primary: 'FOOD_AND_DRINK' }} availableTags={[]} availableCategories={[]} plaidPrimaries={[{ enum: 'TRANSPORTATION', display: 'Transportation' }]} isOpen onClose={onClose} onSave={onSave} />
    </ReadOnlyProvider></QueryClientProvider>)
    fireEvent.click(screen.getByRole('button', { name: 'Remove Trip' }))
    fireEvent.click(screen.getByRole('button', { name: /change category/i }))
    fireEvent.click(await screen.findByRole('option', { name: 'Transportation' }))
    expect((screen.getByRole('button', { name: 'Mark reviewed' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByRole('alert')
    expect(onSave).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
    expect(client.getQueryState(key)?.isInvalidated).toBe(true)
    expect(client.getQueryData<{pages: {transactions: {source: string; tags: string[]}[]}[]}>(key)?.pages[0].transactions[0]).toMatchObject({ source: 'banking', tags: [] })
    expect(screen.getByRole('button', { name: 'Save' })).not.toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    expect(mockUpdateTransactionTagsMutateAsync).toHaveBeenCalledTimes(1)
    expect(mockUpdateTransactionCategoryMutateAsync).toHaveBeenCalledTimes(2)
  })
  describe('merchant display name precedence', () => {
    it('prefers effective_merchant_name over merchant_name when both are set', () => {
      renderEditor({
        ...baseTransaction,
        merchant_name: 'WHOLEFDSMRKT #10234',
        effective_merchant_name: 'Whole Foods Market',
      })
      expect(screen.getByText('Whole Foods Market')).toBeInTheDocument()
      expect(screen.queryByText('WHOLEFDSMRKT #10234')).not.toBeInTheDocument()
    })

    it('falls back to merchant_name when effective_merchant_name is absent', () => {
      renderEditor({ ...baseTransaction, merchant_name: 'STARBUCKS #99' })
      expect(screen.getByText('STARBUCKS #99')).toBeInTheDocument()
    })

    it('falls back to name when both effective_merchant_name and merchant_name are absent', () => {
      renderEditor({ ...baseTransaction, merchant_name: null })
      expect(screen.getByText('KFC')).toBeInTheDocument()
    })
  })

  describe('getEffectiveCategory — PFC v2 fields', () => {
    it('shows pfc_primary as initial category (CategoryEditor shows selectedCategory[0])', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD',
      }
      renderEditor(txn)
      // CategoryEditor resolves raw enum to display label via formatPFCEnum
      expect(screen.getByText(/Food.*Drink/)).toBeInTheDocument()
    })

    it('shows pfc_primary when pfc_detailed is absent', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
      }
      renderEditor(txn)
      expect(screen.getByText(/Food.*Drink/)).toBeInTheDocument()
    })

    it('shows pfc_primary_override when category_override_kind is pfc', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        category_override_kind: 'pfc',
        pfc_primary_override: 'TRANSPORTATION',
        pfc_detailed_override: 'TRANSPORTATION_GAS',
      }
      renderEditor(txn)
      // Override takes precedence; CategoryEditor resolves override enum to display label
      expect(screen.getByText('Transportation › Gas')).toBeInTheDocument()
    })
  })

  describe('getEffectiveCategory — explicit-uncategorized guard', () => {
    it('shows Uncategorized when no pfc_primary and no override', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: null,
        category_override_kind: null,
      }
      renderEditor(txn)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
    })

    it('shows pfc_primary when category_source is manual but pfc_primary is set', () => {
      // In PFC v2, category_source='manual' does NOT suppress pfc_primary display.
      // The transaction still has its Plaid-assigned pfc_primary.
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        category_override_kind: null,
        category_source: 'manual',
      }
      renderEditor(txn)
      expect(screen.getByText(/Food.*Drink/)).toBeInTheDocument()
    })
  })

  describe('save category — Plaid primary encoding', () => {
    const plaidPrimaries = [
      { enum: 'FOOD_AND_DRINK', display: 'Food And Drink' },
      { enum: 'TRANSPORTATION', display: 'Transportation' },
    ]

    beforeEach(() => {
      mockUpdateTransactionCategoryMutateAsync.mockClear()
      mockUpdateTransactionCategoryMutateAsync.mockResolvedValue({ ...baseTransaction })
    })

    it('sends PFC enum not display name when selecting a Plaid primary', async () => {
      const txn: Transaction = { ...baseTransaction, pfc_primary: 'FOOD_AND_DRINK' }
      const Wrapper = createWrapper()
      render(
        <ReadOnlyProvider isReadOnly={false}>
          <TransactionEditor
            transaction={txn}
            availableTags={[]}
            availableCategories={[]}
            plaidPrimaries={plaidPrimaries}
            isOpen={true}
            onClose={() => {}}
            onSave={() => {}}
          />
        </ReadOnlyProvider>,
        { wrapper: Wrapper }
      )

      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      await waitFor(() => expect(screen.getByRole('option', { name: 'Transportation' })).toBeInTheDocument())
      fireEvent.click(screen.getByRole('option', { name: 'Transportation' }))

      await waitFor(() => expect(screen.getByRole('button', { name: /save/i })).not.toBeDisabled())
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(mockUpdateTransactionCategoryMutateAsync).toHaveBeenCalledWith({
          id: 'txn-1',
          data: { kind: 'pfc', pfc_primary: 'TRANSPORTATION', pfc_detailed: undefined },
        })
      })
    })
  })

  // Review toggle (#1203)
  describe('review state', () => {
    beforeEach(() => {
      mockSetTransactionReviewedMutateAsync.mockClear()
      mockSetTransactionReviewedMutateAsync.mockResolvedValue({})
    })

    it('shows Unreviewed and marks reviewed', async () => {
      renderEditor({ ...baseTransaction, reviewed: false })
      expect(screen.getByTestId('review-status-badge').textContent).toContain('Unreviewed')
      fireEvent.click(screen.getByRole('button', { name: /mark reviewed/i }))
      await waitFor(() =>
        expect(mockSetTransactionReviewedMutateAsync).toHaveBeenCalledWith({
          id: 'txn-1',
          data: { reviewed: true },
        })
      )
    })

    it('shows Reviewed and marks unreviewed', async () => {
      renderEditor({ ...baseTransaction, reviewed: true })
      expect(screen.getByTestId('review-status-badge').textContent).toContain('Reviewed')
      fireEvent.click(screen.getByRole('button', { name: /mark unreviewed/i }))
      await waitFor(() =>
        expect(mockSetTransactionReviewedMutateAsync).toHaveBeenCalledWith({
          id: 'txn-1',
          data: { reviewed: false },
        })
      )
    })
  })
})
