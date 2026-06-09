/**
 * AwaitingMatchTab Tests
 * Issue #1267: Boosts page — accepted suggestions watching for transfer match
 *
 * TDD RED: Tests written before implementation.
 * Key: no false countdown; show informational "watching for transfer" copy.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { ReadOnlyProvider } from '@/auth/ReadOnlyContext'
import { AwaitingMatchTab } from './AwaitingMatchTab'

const mockOnConfirmMatch = mock((_id: string, _amount: number) => {})
const mockOnCancelAccepted = mock((_id: string) => {})

const acceptedItem: TransferSuggestion = {
  id: 'ts-10',
  status: 'accepted',
  amount: 750,
  currency_code: 'USD',
  source: 'rule',
  user_id: 'u1',
  created_at: '2025-01-15T10:00:00Z',
  updated_at: '2025-01-16T10:00:00Z',
  user_action_at: '2025-01-16T10:00:00Z',
  // Mark Done requires a fuzzy-match reservation
  can_confirm_match: true,
}

// Enriched item with destination account name (from W1 backend enrichment)
const enrichedAcceptedItem: TransferSuggestion = {
  ...acceptedItem,
  id: 'ts-10-enriched',
  to_account_name: 'High-Yield Savings',
}

describe('AwaitingMatchTab', () => {
  it('shows empty state when no accepted items', () => {
    render(
      <AwaitingMatchTab
        items={[]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    expect(screen.getByText(/no transfers waiting/i)).toBeTruthy()
  })

  it('renders a row per accepted suggestion', () => {
    const { container } = render(
      <AwaitingMatchTab
        items={[acceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    const rows = container.querySelectorAll('tbody tr')
    expect(rows.length).toBe(1)
  })

  it('shows informational watching copy and no countdown', () => {
    render(
      <AwaitingMatchTab
        items={[acceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    // Should show "watching for transfer" somewhere in the table body
    const bodyText = document.body.textContent ?? ''
    expect(/watching for transfer/i.test(bodyText)).toBe(true)
    // Should NOT show countdown text like "expires in" (no false deadline for accepted rows)
    expect(/expires in/i.test(bodyText)).toBe(false)
  })

  it('calls onCancelAccepted with suggestion id when Cancel is clicked', () => {
    mockOnCancelAccepted.mockClear()
    render(
      <AwaitingMatchTab
        items={[acceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(mockOnCancelAccepted).toHaveBeenCalledWith('ts-10')
  })

  it('calls onConfirmMatch with suggestion id and amount when Mark Done is clicked', () => {
    mockOnConfirmMatch.mockClear()
    render(
      <AwaitingMatchTab
        items={[acceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /mark done|done/i }))
    expect(mockOnConfirmMatch).toHaveBeenCalledWith('ts-10', 750)
  })

  it('shows loading state when isLoading is true', () => {
    const { container } = render(
      <AwaitingMatchTab
        items={[]}
        isLoading={true}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    expect(container.querySelector('.animate-pulse') || screen.queryByText(/loading/i)).toBeTruthy()
  })

  it('disables action buttons when isPending is true', () => {
    render(
      <AwaitingMatchTab
        items={[acceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={true}
      />
    )
    const cancelBtn = screen.getByRole('button', { name: /cancel/i })
    const doneBtn = screen.getByRole('button', { name: /mark done|done/i })
    expect(cancelBtn.getAttribute('disabled')).not.toBeNull()
    expect(doneBtn.getAttribute('disabled')).not.toBeNull()
  })

  it('disables Mark Done when can_confirm_match is false (no reservation)', () => {
    const itemNoReservation: TransferSuggestion = {
      ...acceptedItem,
      id: 'ts-nores',
      can_confirm_match: false,
    }
    render(
      <AwaitingMatchTab
        items={[itemNoReservation]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    const doneBtn = screen.getByRole('button', { name: /mark done|done/i })
    expect(doneBtn.getAttribute('disabled')).not.toBeNull()
    // Cancel should still be enabled
    const cancelBtn = screen.getByRole('button', { name: /cancel/i })
    expect(cancelBtn.getAttribute('disabled')).toBeNull()
  })

  it('enables Mark Done when can_confirm_match is true', () => {
    render(
      <AwaitingMatchTab
        items={[{ ...acceptedItem, can_confirm_match: true }]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    const doneBtn = screen.getByRole('button', { name: /mark done|done/i })
    expect(doneBtn.getAttribute('disabled')).toBeNull()
  })

  // === Gap W4: enriched watching copy with account name ===

  it('shows destination account name in watching copy when to_account_name is present', () => {
    render(
      <AwaitingMatchTab
        items={[enrichedAcceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    const bodyText = document.body.textContent ?? ''
    expect(/High-Yield Savings/i.test(bodyText)).toBe(true)
  })

  it('still shows watching copy when to_account_name is absent (graceful fallback)', () => {
    render(
      <AwaitingMatchTab
        items={[acceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onCancelAccepted={mockOnCancelAccepted}
        isPending={false}
      />
    )
    const bodyText = document.body.textContent ?? ''
    expect(/watching for transfer/i.test(bodyText)).toBe(true)
  })

  // === Gap W6: read-only guarding ===

  it('shows ReadOnlyGuard wrapper around action buttons in read-only mode', () => {
    const { container } = render(
      <ReadOnlyProvider isReadOnly={true}>
        <AwaitingMatchTab
          items={[acceptedItem]}
          isLoading={false}
          onConfirmMatch={mockOnConfirmMatch}
          onCancelAccepted={mockOnCancelAccepted}
          isPending={false}
        />
      </ReadOnlyProvider>
    )
    expect(container.querySelector('.pointer-events-none')).toBeTruthy()
  })
})
