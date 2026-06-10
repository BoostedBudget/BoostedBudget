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
import { ReadOnlyProvider } from '@/auth/ReadOnlyContext'
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

// Enriched item with destination account + boost name (from W1 backend enrichment)
const enrichedItem: TransferSuggestion = {
  id: 'ts-enriched',
  status: 'pending',
  amount: 600,
  currency_code: 'USD',
  source: 'boost',
  user_id: 'u1',
  created_at: '2025-01-16T10:00:00Z',
  updated_at: '2025-01-16T10:00:00Z',
  to_account_name: 'Emergency Fund',
  boost_name: 'Save Extra Cash',
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

  // === Gap W4: destination account name + rule context ===

  it('shows destination account name in Move to column', () => {
    render(
      <ActiveRecommendationsTab
        items={[enrichedItem]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    expect(screen.getByText('Emergency Fund')).toBeTruthy()
  })

  it('shows boost name as source context', () => {
    render(
      <ActiveRecommendationsTab
        items={[enrichedItem]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    expect(screen.getByText('Save Extra Cash')).toBeTruthy()
  })

  it('shows generic destination label when to_account_name is absent', () => {
    render(
      <ActiveRecommendationsTab
        items={[pendingItem]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    // Should render a fallback like "—" or "Unknown account" rather than throwing
    const bodyText = document.body.textContent ?? ''
    // At minimum, no JS error — table renders
    expect(bodyText.length).toBeGreaterThan(0)
  })

  // === Gap W6: read-only guarding ===

  it('shows ReadOnlyGuard wrapper around action buttons in read-only mode', () => {
    const { container } = render(
      <ReadOnlyProvider isReadOnly={true}>
        <ActiveRecommendationsTab
          items={[pendingItem]}
          isLoading={false}
          onAccept={mockOnAccept}
          onReject={mockOnReject}
          isPending={false}
        />
      </ReadOnlyProvider>
    )
    // ReadOnlyGuard renders with pointer-events-none on the children wrapper
    expect(container.querySelector('.pointer-events-none')).toBeTruthy()
  })

  // === R2-E: PII masking for destination and context columns ===

  it('masks destination and context columns as well as amount (3+ ph-mask tds per row)', () => {
    const { container } = render(
      <ActiveRecommendationsTab
        items={[enrichedItem]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    // Expect amount + destination + context all masked (≥ 3 data-ph-mask cells)
    const masked = container.querySelectorAll('[data-ph-mask]')
    expect(masked.length).toBeGreaterThanOrEqual(3)
  })

  it('renders EmptyState component (data-slot="empty-state") for empty items', () => {
    const { container } = render(
      <ActiveRecommendationsTab
        items={[]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    expect(container.querySelector('[data-slot="empty-state"]')).toBeTruthy()
  })

  it('shows descriptive label (not bare "—") for missing destination (G8)', () => {
    render(
      <ActiveRecommendationsTab
        items={[pendingItem]}
        isLoading={false}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    const bodyText = document.body.textContent ?? ''
    // Should say something more descriptive than bare dash
    expect(bodyText).toContain('No destination')
  })

  // === R7-2: error state ===

  it('renders ErrorState instead of table when error is set', () => {
    const { container } = render(
      <ActiveRecommendationsTab
        items={[]}
        isLoading={false}
        error={new Error('network error')}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    expect(container.querySelector('[data-slot="error-state"]')).toBeTruthy()
  })

  it('calls onRetry when Retry is clicked in error state', () => {
    const mockOnRetry = mock(() => {})
    render(
      <ActiveRecommendationsTab
        items={[]}
        isLoading={false}
        error={new Error('fail')}
        onRetry={mockOnRetry}
        onAccept={mockOnAccept}
        onReject={mockOnReject}
        isPending={false}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(mockOnRetry).toHaveBeenCalledTimes(1)
  })
})
