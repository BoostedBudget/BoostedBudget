/**
 * HistoryTab Tests
 * Issue #1267: Boosts page — completed/rejected/expired history with pagination
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { HistoryTab } from './HistoryTab'

const mockOnPageChange = mock((_offset: number) => {})

const completedItem: TransferSuggestion = {
  id: 'ts-20',
  status: 'completed',
  amount: 300,
  currency_code: 'USD',
  source: 'rule',
  user_id: 'u1',
  created_at: '2025-01-10T10:00:00Z',
  updated_at: '2025-01-12T10:00:00Z',
  completed_at: '2025-01-12T10:00:00Z',
}

const rejectedItem: TransferSuggestion = {
  id: 'ts-21',
  status: 'rejected',
  amount: 150,
  currency_code: 'USD',
  source: 'rule',
  user_id: 'u1',
  created_at: '2025-01-08T10:00:00Z',
  updated_at: '2025-01-09T10:00:00Z',
}

const expiredItem: TransferSuggestion = {
  id: 'ts-22',
  status: 'expired',
  amount: 200,
  currency_code: 'USD',
  source: 'rule',
  user_id: 'u1',
  created_at: '2025-01-01T10:00:00Z',
  updated_at: '2025-01-08T10:00:00Z',
}

describe('HistoryTab', () => {
  it('shows empty state when history is empty', () => {
    render(
      <HistoryTab
        items={[]}
        total={0}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText(/no completed or past suggestions/i)).toBeTruthy()
  })

  it('renders rows for completed, rejected, and expired items', () => {
    const { container } = render(
      <HistoryTab
        items={[completedItem, rejectedItem, expiredItem]}
        total={3}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    const rows = container.querySelectorAll('tbody tr')
    expect(rows.length).toBe(3)
  })

  it('shows loading state when isLoading is true', () => {
    const { container } = render(
      <HistoryTab
        items={[]}
        total={0}
        limit={20}
        offset={0}
        isLoading={true}
        onPageChange={mockOnPageChange}
      />
    )
    expect(container.querySelector('.animate-pulse') || screen.queryByText(/loading/i)).toBeTruthy()
  })

  it('does not show pagination when total <= limit', () => {
    render(
      <HistoryTab
        items={[completedItem]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    // TablePagination returns null when totalPages <= 1
    expect(screen.queryByRole('button', { name: /next page/i })).toBeNull()
  })

  it('shows pagination when total > limit', () => {
    render(
      <HistoryTab
        items={[completedItem]}
        total={25}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    // TablePagination renders Previous/Next buttons when there are multiple pages
    expect(screen.getByRole('button', { name: /next page/i })).toBeTruthy()
  })

  it('calls onPageChange with correct offset on next page click', () => {
    mockOnPageChange.mockClear()
    render(
      <HistoryTab
        items={[completedItem]}
        total={25}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /next page/i }))
    // offset for page 2 = limit * (page - 1) = 20 * 1 = 20
    expect(mockOnPageChange).toHaveBeenCalledWith(20)
  })
})
