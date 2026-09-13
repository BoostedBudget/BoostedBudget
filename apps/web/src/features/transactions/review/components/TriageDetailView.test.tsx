import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TriageDetailView } from './TriageDetailView'
import type { Transaction } from '../../types'
import { mockUseGetUserCategories, mockUseGetPlaidPrimaryCategories, mockUseGetPlaidDetailedCategories } from '@/test/mocks/categories-api'

beforeEach(() => {
  mockUseGetUserCategories.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(async () => {}) })
  mockUseGetPlaidPrimaryCategories.mockReturnValue({ data: [], isLoading: false, error: null })
  mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [], isLoading: false, error: null })
})

const mockTx: Transaction = {
  id: 'tx-1',
  user_id: 'user-1',
  account_id: 'acct-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: 42.50,
  date: '2026-05-01',
  name: 'Coffee Shop',
  merchant_name: 'Blue Bottle Coffee',
  pending: false,
  reviewed: false,
  category_source: 'plaid',
  created_at: '2026-05-01T10:00:00Z',
  updated_at: '2026-05-01T10:00:00Z',
}

function renderTriage(overrides?: Partial<Parameters<typeof TriageDetailView>[0]>) {
  const props = {
    transaction: mockTx,
    currentIndex: 0,
    total: 3,
    onApprove: mock(),
    onSkip: mock(),
    onRecategorize: mock(),
    onToggleSelect: mock(),
    onNext: mock(),
    onPrev: mock(),
    onClose: mock(),
    isSelected: false,
    isPending: false,
    ...overrides,
  }
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { ...render(<TriageDetailView {...props} />, { wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> }), props }
}

describe('TriageDetailView', () => {
  it('shows masked context, pending status and a signed inflow', () => {
    renderTriage({ transaction: { ...mockTx, amount: -42.5, pending: true, tags: ['Holiday'], pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_COFFEE' }, account: { name: 'Closed checking' } })
    for (const text of ['Coffee Shop', 'Closed checking', 'Holiday', '+$42.50', 'Food & Drink › Coffee']) {
      expect(screen.getByText(text).closest('[data-ph-mask]')).not.toBeNull()
    }
    expect(screen.getByText('Pending')).toBeInTheDocument()
  })

  it('does not approve on Enter from an interactive control', () => {
    const { props } = renderTriage()
    fireEvent.keyDown(screen.getByRole('button', { name: 'Close triage' }), { key: 'Enter' })
    expect(props.onApprove).not.toHaveBeenCalled()
    fireEvent.keyDown(document, { key: 'Enter' })
    expect(props.onApprove).toHaveBeenCalledTimes(1)
  })

  it('disables triage controls behind an overlay', () => {
    renderTriage({ currentIndex: 1, externalOverlayOpen: true })
    for (const name of [/approve/i, /skip/i, /previous transaction/i, /next transaction/i, /close triage/i, /recategorize transaction/i]) {
      expect(screen.getByRole('button', { name })).toBeDisabled()
    }
  })

  it('blocks navigation while preserving a pending category write', async () => {
    let resolve!: () => void
    const onRecategorize = mock(() => new Promise<void>(r => { resolve = r }))
    renderTriage({ transaction: { ...mockTx, pfc_primary: 'FOOD_AND_DRINK' }, onRecategorize })
    fireEvent.click(screen.getByRole('button', { name: 'Recategorize transaction' }))
    fireEvent.click(screen.getByRole('button', { name: 'Reset category override' }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    expect(onRecategorize).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Next transaction' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    resolve()
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull())
  })
  it('renders merchant name, amount, and date', () => {
    renderTriage()
    expect(screen.getByText('Blue Bottle Coffee')).toBeInTheDocument()
    expect(screen.getByText('-$42.50')).toBeInTheDocument()
    // #1631: formatted through the shared formatDate rather than printed raw. Asserting the
    // formatted form AND the absence of the raw one, because getByText('2026-05-01') passed
    // against the unformatted render and so proved nothing about formatting.
    expect(screen.getByText('May 1, 2026')).toBeInTheDocument()
    expect(screen.queryByText('2026-05-01')).not.toBeInTheDocument()
  })

  // The wire value used to be a full timestamp on this field, printed raw, so users saw
  // "2026-05-01T00:00:00Z". formatDate splits on the T and parses component-wise, so it is correct
  // for both the old and the new wire shape -- which is what makes this site safe to fix
  // independently of the backend deploy.
  it('formats a legacy timestamp-shaped date without shifting the day', () => {
    renderTriage({ transaction: { ...mockTx, date: '2026-05-01T00:00:00Z' } })
    expect(screen.getByText('May 1, 2026')).toBeInTheDocument()
    expect(screen.queryByText('2026-05-01T00:00:00Z')).not.toBeInTheDocument()
  })

  it('uses transaction name when merchant_name is null', () => {
    renderTriage({ transaction: { ...mockTx, merchant_name: null } })
    expect(screen.getByText('Coffee Shop')).toBeInTheDocument()
  })

  it('prefers effective_merchant_name over merchant_name when both are set', () => {
    renderTriage({
      transaction: {
        ...mockTx,
        merchant_name: 'WHOLEFDSMRKT #10234',
        effective_merchant_name: 'Whole Foods Market',
      },
    })
    expect(screen.getByText('Whole Foods Market')).toBeInTheDocument()
    expect(screen.queryByText('WHOLEFDSMRKT #10234')).not.toBeInTheDocument()
  })

  it('renders ReviewBadge for unreviewed transaction', () => {
    renderTriage()
    expect(screen.getByText('Unreviewed')).toBeInTheDocument()
  })

  it('renders ReviewBadge for reviewed/plaid transaction', () => {
    renderTriage({ transaction: { ...mockTx, reviewed: true, category_source: 'plaid' } })
    expect(screen.getByText('Categorized')).toBeInTheDocument()
  })

  it('shows progress counter', () => {
    renderTriage({ currentIndex: 1, total: 5 })
    expect(screen.getByText('2 / 5')).toBeInTheDocument()
  })

  it('Approve button fires onApprove', () => {
    const { props } = renderTriage()
    fireEvent.click(screen.getByRole('button', { name: /approve/i }))
    expect(props.onApprove).toHaveBeenCalledTimes(1)
  })

  it('Skip button fires onSkip', () => {
    const { props } = renderTriage()
    fireEvent.click(screen.getByRole('button', { name: /skip/i }))
    expect(props.onSkip).toHaveBeenCalledTimes(1)
  })

  it('Select button fires onToggleSelect', () => {
    const { props } = renderTriage()
    fireEvent.click(screen.getByRole('button', { name: /select/i }))
    expect(props.onToggleSelect).toHaveBeenCalledTimes(1)
  })

  it('shows "Deselect" label when isSelected=true', () => {
    renderTriage({ isSelected: true })
    expect(screen.getByRole('button', { name: /deselect/i })).toBeInTheDocument()
  })

  it('shows "Selected" badge when isSelected=true', () => {
    renderTriage({ isSelected: true })
    expect(screen.getByText('Selected')).toBeInTheDocument()
  })

  it('Recategorize button shows recategorize panel', () => {
    renderTriage()
    expect(screen.queryByText('Recategorize')).toBeInTheDocument()
    // Click the Recategorize action button (not header label)
    fireEvent.click(screen.getByRole('button', { name: /recategorize transaction/i }))
    expect(screen.getByText('Apply')).toBeInTheDocument()
  })

  it('Cancel in recategorize panel hides it', () => {
    renderTriage()
    fireEvent.click(screen.getByRole('button', { name: /recategorize transaction/i }))
    expect(screen.getByText('Apply')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(screen.queryByText('Apply')).toBeNull()
  })

  it('? button opens KeyboardShortcutsHelp dialog', () => {
    renderTriage()
    fireEvent.click(screen.getByRole('button', { name: /keyboard shortcuts/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('disables action buttons when isPending=true', () => {
    renderTriage({ isPending: true })
    expect(screen.getByRole('button', { name: /approve/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /skip/i })).toBeDisabled()
  })

  it('disables prev button at first index', () => {
    renderTriage({ currentIndex: 0 })
    expect(screen.getByRole('button', { name: /previous transaction/i })).toBeDisabled()
  })

  it('disables next button at last index', () => {
    renderTriage({ currentIndex: 2, total: 3 })
    expect(screen.getByRole('button', { name: /next transaction/i })).toBeDisabled()
  })

  describe('keyboard shortcuts', () => {
    it('e key fires onApprove', () => {
      const { props } = renderTriage()
      fireEvent.keyDown(document, { key: 'e' })
      expect(props.onApprove).toHaveBeenCalledTimes(1)
    })

    it('s key fires onSkip', () => {
      const { props } = renderTriage()
      fireEvent.keyDown(document, { key: 's' })
      expect(props.onSkip).toHaveBeenCalledTimes(1)
    })

    it('c key opens recategorize panel', () => {
      renderTriage()
      expect(screen.queryByText('Apply')).toBeNull()
      fireEvent.keyDown(document, { key: 'c' })
      expect(screen.getByText('Apply')).toBeInTheDocument()
    })

    it('x key fires onToggleSelect', () => {
      const { props } = renderTriage()
      fireEvent.keyDown(document, { key: 'x' })
      expect(props.onToggleSelect).toHaveBeenCalledTimes(1)
    })

    it('shortcuts do not fire when recategorize panel is open', () => {
      const { props } = renderTriage()
      fireEvent.click(screen.getByRole('button', { name: /recategorize transaction/i }))
      fireEvent.keyDown(document, { key: 'e' })
      expect(props.onApprove).not.toHaveBeenCalled()
    })

    it('Meta+z fires onUndo when provided', () => {
      const onUndo = mock()
      renderTriage({ onUndo })
      fireEvent.keyDown(document, { key: 'z', metaKey: true })
      expect(onUndo).toHaveBeenCalledTimes(1)
    })

    it('Control+z fires onUndo when provided', () => {
      const onUndo = mock()
      renderTriage({ onUndo })
      fireEvent.keyDown(document, { key: 'z', ctrlKey: true })
      expect(onUndo).toHaveBeenCalledTimes(1)
    })

    it('disables keyboard shortcuts when externalOverlayOpen=true', () => {
      const { props } = renderTriage({ externalOverlayOpen: true })
      fireEvent.keyDown(document, { key: 'e' })
      expect(props.onApprove).not.toHaveBeenCalled()
    })
  })
})

// ─── Issue #1625 D: the recategorize panel must seed from the transaction ─────
//
// The panel used to open with an empty `pendingCategory`, so CategoryEditor rendered
// "Uncategorized" for a transaction that already had a perfectly good Plaid PFC — and
// because handleSaveCategory treats an empty selection as { kind: 'clear' }, pressing
// Apply without touching the category DESTROYED that category. These pin both halves.
describe('#1625 recategorize panel seeding', () => {
  const pfcTx: Transaction = {
    ...mockTx,
    pfc_primary: 'FOOD_AND_DRINK',
    pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
    pfc_primary_display: 'Food and Drink',
    pfc_detailed_display: 'Coffee',
  }

  const plaidPrimaries = [{ enum: 'FOOD_AND_DRINK', display: 'Food and Drink' }] as never

  function openPanel(overrides?: Partial<Parameters<typeof TriageDetailView>[0]>) {
    const r = renderTriage(overrides)
    fireEvent.click(screen.getByRole('button', { name: /recategorize transaction/i }))
    return r
  }

  it('shows the existing PFC category, not "Uncategorized"', () => {
    openPanel({ transaction: pfcTx, plaidPrimaries })
    expect(screen.queryByText('Uncategorized')).not.toBeInTheDocument()
    expect(screen.getAllByText(/Food.*Drink › Coffee/)).toHaveLength(2)
  })

  it('does NOT clear the category when Apply is pressed without touching it', () => {
    const { props } = openPanel({ transaction: pfcTx, plaidPrimaries })
    fireEvent.click(screen.getByRole('button', { name: /apply/i }))

    expect(screen.getByRole('button', { name: /apply/i })).toBeDisabled()
    expect(props.onRecategorize).not.toHaveBeenCalled()
  })

  it('round-trips an untouched CUSTOM category as custom, not as a PFC enum', () => {
    const customTx: Transaction = {
      ...mockTx,
      category_override_kind: 'custom',
      custom_category_override_id: 'cat-99',
    }
    const { props } = openPanel({
      transaction: customTx,
      availableCategories: [{ id: 'cat-99', name: 'Coffee Budget', color: '#fff' }] as never,
    })
    fireEvent.click(screen.getByRole('button', { name: /apply/i }))

    expect(screen.getAllByText('Coffee Budget')).toHaveLength(2)
    expect(screen.getByRole('button', { name: /apply/i })).toBeDisabled()
    expect(props.onRecategorize).not.toHaveBeenCalled()
  })

  it('keeps failed recategorization open with its selected draft', async () => {
    const onRecategorize = mock(async () => { throw new Error('offline') })
    openPanel({ transaction: pfcTx, plaidPrimaries, onRecategorize })
    fireEvent.click(screen.getByRole('button', { name: /reset category override/i }))
    fireEvent.click(screen.getByRole('button', { name: /apply/i }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Failed to recategorize'))
    expect(screen.getByText('Uses the bank category when available.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /apply/i })).toBeEnabled()
  })

  it('still clears when the user explicitly clears the category', async () => {
    const { props } = openPanel({ transaction: pfcTx, plaidPrimaries })
    fireEvent.click(screen.getByRole('button', { name: /reset category override/i }))
    fireEvent.click(screen.getByRole('button', { name: /apply/i }))

    expect((props.onRecategorize as ReturnType<typeof mock>).mock.calls[0][0]).toEqual({ kind: 'clear' })
    await waitFor(() => expect(screen.queryByRole('button', { name: /apply/i })).not.toBeInTheDocument())
  })

  it('re-seeds when the transaction changes underneath an open panel', () => {
    const { rerender, props } = openPanel({ transaction: pfcTx, plaidPrimaries })
    const nextTx: Transaction = {
      ...mockTx,
      id: 'tx-2',
      pfc_primary: 'TRANSPORTATION',
      pfc_primary_display: 'Transportation',
    }
    rerender(<TriageDetailView {...props} transaction={nextTx} plaidPrimaries={plaidPrimaries} />)
    fireEvent.click(screen.getByRole('button', { name: /apply/i }))

    expect(screen.getAllByText('Transportation')).toHaveLength(2)
    expect(screen.getByRole('button', { name: /apply/i })).toBeDisabled()
    expect(props.onRecategorize).not.toHaveBeenCalled()
  })
})
