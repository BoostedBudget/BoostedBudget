/**
 * HistoryTab Tests
 * Issue #1267: Boosts page — completed/rejected/expired history with pagination
 *
 * Gap W3: event-date column, completion_source labels, matched-tx date, rejected reason.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { HistoryTab } from './HistoryTab'

const mockOnPageChange = mock((_offset: number) => {})

// created_at is old (Jan 1); completed_at is Jan 15; updated_at is Jan 20 (later than completed_at).
// The event-date column must show Jan 15 (completed_at), NOT Jan 20 (updated_at).
// The distinction between completed_at and updated_at is intentional — regression guard for R4-5.
const completedItem: TransferSuggestion = {
  id: 'ts-20',
  status: 'completed',
  amount: 300,
  currency_code: 'USD',
  source: 'boost',
  user_id: 'u1',
  created_at: '2025-01-01T10:00:00Z',
  updated_at: '2025-01-20T10:00:00Z',
  completed_at: '2025-01-15T10:00:00Z',
}

const rejectedItem: TransferSuggestion = {
  id: 'ts-21',
  status: 'rejected',
  amount: 150,
  currency_code: 'USD',
  source: 'boost',
  user_id: 'u1',
  created_at: '2025-01-08T10:00:00Z',
  updated_at: '2025-01-09T10:00:00Z',
}

const expiredItem: TransferSuggestion = {
  id: 'ts-22',
  status: 'expired',
  amount: 200,
  currency_code: 'USD',
  source: 'boost',
  user_id: 'u1',
  created_at: '2025-01-01T10:00:00Z',
  updated_at: '2025-01-08T10:00:00Z',
}

// Auto-matched completed item (exact_match + matched_transaction_at)
// updated_at (Feb 20) intentionally differs from completed_at (Feb 15) — R4-5 regression guard
const autoMatchedItem: TransferSuggestion = {
  id: 'ts-30',
  status: 'completed',
  amount: 500,
  currency_code: 'USD',
  source: 'boost',
  user_id: 'u1',
  created_at: '2025-02-01T08:00:00Z',
  updated_at: '2025-02-20T09:00:00Z',
  completed_at: '2025-02-15T09:00:00Z',
  completion_source: 'exact_match',
  matched_transaction_at: '2025-02-15T09:00:00Z',
}

// Manually confirmed completed item
const manualConfirmedItem: TransferSuggestion = {
  id: 'ts-31',
  status: 'completed',
  amount: 400,
  currency_code: 'USD',
  source: 'boost',
  user_id: 'u1',
  created_at: '2025-03-05T10:00:00Z',
  updated_at: '2025-03-14T12:00:00Z',
  completed_at: '2025-03-14T12:00:00Z',
  completion_source: 'user_confirmed',
}

// Rejected item with user_notes as the rejection reason
const rejectedWithReasonItem: TransferSuggestion = {
  id: 'ts-32',
  status: 'rejected',
  amount: 200,
  currency_code: 'USD',
  source: 'boost',
  user_id: 'u1',
  created_at: '2025-01-08T10:00:00Z',
  updated_at: '2025-01-09T10:00:00Z',
  user_notes: "Didn't have enough in checking",
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

  // === Gap W3: event-date, completion_source labels, matched-tx date, rejected reason ===

  it('shows event date from completed_at (not created_at) for completed items', () => {
    // completedItem: created Jan 1, completed Jan 15 — must see Jan 15
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
    // Jan 15, 2025 must appear (event date); Jan 1 only in created_at, which is not displayed
    expect(screen.getByText(/Jan 15, 2025/i)).toBeTruthy()
  })

  it('shows "Auto-matched" label for completion_source exact_match', () => {
    render(
      <HistoryTab
        items={[autoMatchedItem]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText(/auto-matched/i)).toBeTruthy()
  })

  it('shows "You confirmed" label for completion_source user_confirmed', () => {
    render(
      <HistoryTab
        items={[manualConfirmedItem]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText(/you confirmed/i)).toBeTruthy()
  })

  it('shows matched transaction date when matched_transaction_at is present', () => {
    render(
      <HistoryTab
        items={[autoMatchedItem]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    // matched_transaction_at is Feb 15, 2025 — should appear in the row
    const bodyText = document.body.textContent ?? ''
    expect(/Feb 15, 2025/i.test(bodyText)).toBe(true)
  })

  it('shows user_notes as rejected reason for rejected items', () => {
    render(
      <HistoryTab
        items={[rejectedWithReasonItem]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText(/didn't have enough in checking/i)).toBeTruthy()
  })

  // === R3-1: expired rows show explanatory reason when no notes/reason ===

  it('shows "No matching transfer found" for expired items with no notes', () => {
    render(
      <HistoryTab
        items={[expiredItem]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText(/no matching transfer found/i)).toBeTruthy()
  })

  it('still shows user_notes for expired items that have notes', () => {
    const expiredWithNote: TransferSuggestion = {
      ...expiredItem,
      id: 'ts-23',
      user_notes: 'Transfer was made manually',
    }
    render(
      <HistoryTab
        items={[expiredWithNote]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText(/transfer was made manually/i)).toBeTruthy()
  })

  it('shows reason for expired items when reason is set but user_notes is absent', () => {
    const expiredWithReason: TransferSuggestion = {
      ...expiredItem,
      id: 'ts-24',
      reason: 'Budget period ended without a match',
    }
    render(
      <HistoryTab
        items={[expiredWithReason]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText(/budget period ended without a match/i)).toBeTruthy()
  })

  // === R7-1: destination column (AC4) ===

  it('shows destination account name for completed items', () => {
    const completedWithDest = { ...completedItem, to_account_name: 'High-Yield Savings' }
    render(
      <HistoryTab
        items={[completedWithDest]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText('High-Yield Savings')).toBeTruthy()
  })

  it('shows destination account name for expired items', () => {
    const expiredWithDest = { ...expiredItem, to_account_name: 'Emergency Fund' }
    render(
      <HistoryTab
        items={[expiredWithDest]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(screen.getByText('Emergency Fund')).toBeTruthy()
  })

  it('shows "No destination" fallback when to_account_name is null', () => {
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
    // completedItem has no to_account_name — should show descriptive fallback
    expect(screen.getByText('No destination')).toBeTruthy()
  })

  // === R2-E: PII masking + EmptyState ===

  it('masks amount, destination, and notes columns (≥3 ph-mask tds per row)', () => {
    const completedWithDest = { ...completedItem, to_account_name: 'High-Yield Savings' }
    const { container } = render(
      <HistoryTab
        items={[completedWithDest]}
        total={1}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    const masked = container.querySelectorAll('[data-ph-mask]')
    expect(masked.length).toBeGreaterThanOrEqual(3)
  })

  it('renders EmptyState component (data-slot="empty-state") for empty items', () => {
    const { container } = render(
      <HistoryTab
        items={[]}
        total={0}
        limit={20}
        offset={0}
        isLoading={false}
        onPageChange={mockOnPageChange}
      />
    )
    expect(container.querySelector('[data-slot="empty-state"]')).toBeTruthy()
  })

  // === R7-2: error state ===

  it('renders ErrorState instead of table when error is set', () => {
    const { container } = render(
      <HistoryTab
        items={[]}
        total={0}
        limit={20}
        offset={0}
        isLoading={false}
        error={new Error('fetch failed')}
        onPageChange={mockOnPageChange}
      />
    )
    expect(container.querySelector('[data-slot="error-state"]')).toBeTruthy()
  })

  it('calls onRetry when Retry is clicked in error state', () => {
    const mockOnRetry = mock((_?: unknown) => {})
    render(
      <HistoryTab
        items={[]}
        total={0}
        limit={20}
        offset={0}
        isLoading={false}
        error={new Error('fail')}
        onRetry={mockOnRetry}
        onPageChange={mockOnPageChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(mockOnRetry).toHaveBeenCalledTimes(1)
  })
})
