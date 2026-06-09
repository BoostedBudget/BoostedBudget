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
import { AwaitingMatchTab } from './AwaitingMatchTab'

const mockOnConfirmMatch = mock((_id: string, _amount: number) => {})
const mockOnDeclineMatch = mock((_id: string) => {})

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
}

describe('AwaitingMatchTab', () => {
  it('shows empty state when no accepted items', () => {
    render(
      <AwaitingMatchTab
        items={[]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onDeclineMatch={mockOnDeclineMatch}
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
        onDeclineMatch={mockOnDeclineMatch}
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
        onDeclineMatch={mockOnDeclineMatch}
        isPending={false}
      />
    )
    // Should show "watching for transfer" somewhere in the table body
    const bodyText = document.body.textContent ?? ''
    expect(/watching for transfer/i.test(bodyText)).toBe(true)
    // Should NOT show countdown text like "expires in" (no false deadline for accepted rows)
    expect(/expires in/i.test(bodyText)).toBe(false)
  })

  it('calls onDeclineMatch with suggestion id when Cancel is clicked', () => {
    mockOnDeclineMatch.mockClear()
    render(
      <AwaitingMatchTab
        items={[acceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onDeclineMatch={mockOnDeclineMatch}
        isPending={false}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(mockOnDeclineMatch).toHaveBeenCalledWith('ts-10')
  })

  it('calls onConfirmMatch with suggestion id and amount when Mark Done is clicked', () => {
    mockOnConfirmMatch.mockClear()
    render(
      <AwaitingMatchTab
        items={[acceptedItem]}
        isLoading={false}
        onConfirmMatch={mockOnConfirmMatch}
        onDeclineMatch={mockOnDeclineMatch}
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
        onDeclineMatch={mockOnDeclineMatch}
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
        onDeclineMatch={mockOnDeclineMatch}
        isPending={true}
      />
    )
    const cancelBtn = screen.getByRole('button', { name: /cancel/i })
    const doneBtn = screen.getByRole('button', { name: /mark done|done/i })
    expect(cancelBtn.getAttribute('disabled')).not.toBeNull()
    expect(doneBtn.getAttribute('disabled')).not.toBeNull()
  })
})
