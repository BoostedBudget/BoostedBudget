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
    expect(screen.getByText('2026-05-01')).toBeInTheDocument()
  })

  it('uses transaction name when merchant_name is null', () => {
    renderTriage({ transaction: { ...mockTx, merchant_name: null } })
    expect(screen.getByText('Coffee Shop')).toBeInTheDocument()
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
