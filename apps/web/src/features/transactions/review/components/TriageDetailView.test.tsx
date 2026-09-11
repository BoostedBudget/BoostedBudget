import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { TriageDetailView } from './TriageDetailView'
import type { Transaction } from '../../types'

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
  return { ...render(<TriageDetailView {...props} />), props }
}

describe('TriageDetailView', () => {
  it('renders merchant name, amount, and date', () => {
    renderTriage()
    expect(screen.getByText('Blue Bottle Coffee')).toBeInTheDocument()
    expect(screen.getByText('$42.50')).toBeInTheDocument()
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
    expect(screen.getByText('Food and Drink')).toBeInTheDocument()
  })

  it('does NOT clear the category when Apply is pressed without touching it', () => {
    const { props } = openPanel({ transaction: pfcTx, plaidPrimaries })
    fireEvent.click(screen.getByRole('button', { name: /apply/i }))

    expect(props.onRecategorize).toHaveBeenCalledTimes(1)
    const req = (props.onRecategorize as ReturnType<typeof mock>).mock.calls[0][0]
    expect(req.kind).not.toBe('clear')
    expect(req).toEqual({
      kind: 'pfc',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
    })
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

    const req = (props.onRecategorize as ReturnType<typeof mock>).mock.calls[0][0]
    expect(req).toEqual({ kind: 'custom', custom_category_id: 'cat-99' })
  })

  it('still clears when the user explicitly clears the category', () => {
    const { props } = openPanel({ transaction: pfcTx, plaidPrimaries })
    fireEvent.click(screen.getByRole('button', { name: /clear category/i }))
    fireEvent.click(screen.getByRole('button', { name: /apply/i }))

    expect((props.onRecategorize as ReturnType<typeof mock>).mock.calls[0][0]).toEqual({ kind: 'clear' })
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

    const req = (props.onRecategorize as ReturnType<typeof mock>).mock.calls[0][0]
    expect(req).toEqual({ kind: 'pfc', pfc_primary: 'TRANSPORTATION', pfc_detailed: undefined })
  })
})
