/**
 * ActiveRecommendationsTab Tests
 * Issue #1267: Boosts page — pending suggestions with Accept/Reject actions
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { ActiveRecommendationsTab } from './ActiveRecommendationsTab'

const mockOnAccept = mock((_id: string) => {})
const mockOnReject = mock((_id: string) => {})

const pendingItem: TransferSuggestion = {
  id: 'ts-1',
  status: 'pending',
  amount: 500,
  currency_code: 'USD',
  source: 'rule',
  user_id: 'u1',
  created_at: '2025-01-15T10:00:00Z',
  updated_at: '2025-01-15T10:00:00Z',
}

const pendingItem2: TransferSuggestion = {
  id: 'ts-2',
  status: 'pending',
  amount: 250,
  currency_code: 'USD',
  source: 'rule',
  user_id: 'u1',
  created_at: '2025-01-14T10:00:00Z',
  updated_at: '2025-01-14T10:00:00Z',
}

describe('ActiveRecommendationsTab', () => {
  it('shows empty state when items array is empty', () => {
    render(
      <ActiveRecommendationsTab
        items={[]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    expect(screen.getByText(/no active recommendations/i)).toBeTruthy()
  })

  it('renders a row per suggestion item', () => {
    const { container } = render(
      <ActiveRecommendationsTab
        items={[pendingItem, pendingItem2]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    // DataTable renders rows; check there are 2 data rows (tbody tr)
    const rows = container.querySelectorAll('tbody tr')
    expect(rows.length).toBe(2)
  })

  it('calls onAccept with suggestion id when Accept is clicked', () => {
    mockOnAccept.mockClear()
    render(
      <ActiveRecommendationsTab
        items={[pendingItem]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /accept/i }))
    expect(mockOnAccept).toHaveBeenCalledWith('ts-1')
  })

  it('calls onReject with suggestion id when Reject is clicked', () => {
    mockOnReject.mockClear()
    render(
      <ActiveRecommendationsTab
        items={[pendingItem]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /reject/i }))
    expect(mockOnReject).toHaveBeenCalledWith('ts-1')
  })

  it('shows loading state when isLoading is true', () => {
    const { container } = render(
      <ActiveRecommendationsTab
        items={[]}
        isLoading={true}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    expect(container.querySelector('.animate-pulse') || screen.queryByText(/loading/i)).toBeTruthy()
  })

  it('disables action buttons when isPending is true', () => {
    render(
      <ActiveRecommendationsTab
        items={[pendingItem]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={true}
      />
    )
    const acceptBtn = screen.getByRole('button', { name: /accept/i })
    const rejectBtn = screen.getByRole('button', { name: /reject/i })
    expect(acceptBtn.getAttribute('disabled')).not.toBeNull()
    expect(rejectBtn.getAttribute('disabled')).not.toBeNull()
  })
})
