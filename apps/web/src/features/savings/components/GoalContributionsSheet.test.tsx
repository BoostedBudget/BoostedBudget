// Uses preloaded API mocks from bunfig.toml — do NOT add mock.module() for
// savings-goals here; that is centralized in src/test/mocks/savings-goals-api.ts.

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import React from 'react'
import {
  mockUseListSavingsGoalContributions,
  mockUseConfirmSavingsGoalContribution,
  mockUseRejectSavingsGoalContribution,
  mockUseDeleteSavingsGoalContribution,
  mockConfirmSavingsGoalContributionMutateAsync,
  mockRejectSavingsGoalContributionMutateAsync,
  mockDeleteSavingsGoalContributionMutateAsync,
} from '@/test/mocks/savings-goals-api'

import { mockTrackEvent } from '@/test/mocks/track-event'

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { GoalContributionsSheet } from './GoalContributionsSheet'
import type { SavingsGoalContribution } from '@boostedbudget/api-client/models'

function renderWithQueryClient(ui: React.ReactNode) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>)
}

const mockProposed: SavingsGoalContribution = {
  id: 'c-proposed-1',
  savings_goal_id: 'goal-1',
  amount: 150,
  source: 'sync',
  status: 'proposed',
  confirmed_by_user: false,
  managed_by_reconciliation: true,
  occurred_on: '2026-06-10',
  created_at: '2026-06-10T00:00:00Z',
  updated_at: '2026-06-10T00:00:00Z',
}

const mockConfirmed: SavingsGoalContribution = {
  id: 'c-confirmed-1',
  savings_goal_id: 'goal-1',
  amount: 250,
  source: 'manual',
  status: 'confirmed',
  confirmed_by_user: true,
  managed_by_reconciliation: false,
  occurred_on: '2026-06-08',
  created_at: '2026-06-08T00:00:00Z',
  updated_at: '2026-06-08T00:00:00Z',
}

it('labels supplementary full history and formats the supplied goal currency', () => {
  mockUseListSavingsGoalContributions.mockReturnValue({ data: [mockConfirmed], isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
  renderWithQueryClient(<GoalContributionsSheet goalId="goal-1" goalName="Goal" open onOpenChange={() => {}} onContributionChanged={() => {}} currencyCode="CAD" contextNote="Full savings history includes entries outside the budget period." />)
  expect(screen.getByText('CA$250.00')).toBeInTheDocument()
  expect(screen.getByText('Full savings history includes entries outside the budget period.')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /log contribution/i }))
  expect(screen.getByRole('spinbutton', { name: 'Amount (CAD)' })).toBeInTheDocument()
})

it('renders malformed goal currency safely in masked rows and the log dialog', () => {
  mockUseListSavingsGoalContributions.mockReturnValue({ data: [mockConfirmed], isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
  renderWithQueryClient(<GoalContributionsSheet goalId="goal-1" goalName="Goal" open onOpenChange={() => {}} onContributionChanged={() => {}} currencyCode="U$D" />)
  expect(screen.getByText('U$D 250.00')).toHaveAttribute('data-ph-mask')
  fireEvent.click(screen.getByRole('button', { name: /log contribution/i }))
  expect(screen.getByRole('spinbutton', { name: 'Amount (U$D)' })).toBeInTheDocument()
})

// R4-2: rejected tombstone — should NOT appear in the sheet UI.
const mockRejected: SavingsGoalContribution = {
  id: 'c-rejected-1',
  savings_goal_id: 'goal-1',
  amount: 300,
  source: 'sync',
  status: 'rejected',
  confirmed_by_user: false,
  managed_by_reconciliation: true,
  occurred_on: '2026-06-05',
  created_at: '2026-06-05T00:00:00Z',
  updated_at: '2026-06-05T00:00:00Z',
}

// #1625 Task 6: archived when the goal's progress_basis round-tripped and this contribution's
// period closed. Retained and listed, but excluded from `current_amount` server-side.
const mockArchived: SavingsGoalContribution = {
  id: 'c-archived-1',
  savings_goal_id: 'goal-1',
  amount: 400,
  source: 'manual',
  status: 'confirmed',
  confirmed_by_user: true,
  managed_by_reconciliation: false,
  occurred_on: '2026-05-01',
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-20T00:00:00Z',
  archived_at: '2026-05-20T00:00:00Z',
}

// An archived row that was never confirmed before its period closed. Both Confirm and Reject
// now 404 an archived contribution server-side (#1625), regardless of `status`.
const mockArchivedProposed: SavingsGoalContribution = {
  id: 'c-archived-proposed-1',
  savings_goal_id: 'goal-1',
  amount: 175,
  source: 'sync',
  status: 'proposed',
  confirmed_by_user: false,
  managed_by_reconciliation: true,
  occurred_on: '2026-05-02',
  created_at: '2026-05-02T00:00:00Z',
  updated_at: '2026-05-20T00:00:00Z',
  archived_at: '2026-05-20T00:00:00Z',
}

describe('GoalContributionsSheet', () => {
  const goalId = 'goal-1'
  const goalName = 'Vacation Fund'
  const mockOnOpenChange = mock(() => {})
  const mockOnChange = mock(() => {})

  beforeEach(() => {
    mockConfirmSavingsGoalContributionMutateAsync.mockClear()
    mockRejectSavingsGoalContributionMutateAsync.mockClear()
    mockDeleteSavingsGoalContributionMutateAsync.mockClear()
    mockUseListSavingsGoalContributions.mockClear()
    mockUseConfirmSavingsGoalContribution.mockClear()
    mockUseRejectSavingsGoalContribution.mockClear()
    mockUseDeleteSavingsGoalContribution.mockClear()
    mockTrackEvent.mockClear()
    mockOnOpenChange.mockClear()
    mockOnChange.mockClear()

    mockUseListSavingsGoalContributions.mockReturnValue({
      data: [mockProposed, mockConfirmed],
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve({})),
    })
    mockUseConfirmSavingsGoalContribution.mockReturnValue({
      mutateAsync: mockConfirmSavingsGoalContributionMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockUseRejectSavingsGoalContribution.mockReturnValue({ // R3-7
      mutateAsync: mockRejectSavingsGoalContributionMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockUseDeleteSavingsGoalContribution.mockReturnValue({
      mutateAsync: mockDeleteSavingsGoalContributionMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
  })

  describe('Rendering', () => {
    it('renders goal name in sheet header when open', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
      expect(screen.getByText(/Vacation Fund/)).toBeInTheDocument()
    })

    it('does not render contributions when closed', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={false}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
      expect(screen.queryByText(/\$150\.00/)).not.toBeInTheDocument()
    })

    it('renders proposed contributions with Confirm and Reject buttons (R3-7)', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
      // Should show proposed badge
      expect(screen.getByText(/proposed/i)).toBeInTheDocument()
      // Confirm button for proposed row
      expect(screen.getByRole('button', { name: /confirm/i })).toBeInTheDocument()
      // Reject button for proposed row (R3-7: not Delete)
      expect(screen.getByRole('button', { name: /reject/i })).toBeInTheDocument()
      // Proposed rows should NOT have a Delete button (only confirmed rows do)
      const deleteButtons = screen.queryAllByRole('button', { name: /delete/i })
      expect(deleteButtons).toHaveLength(1) // only the confirmed row
    })

    it('renders confirmed contributions with amounts masked (data-ph-mask)', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
      // The $250 confirmed amount should be in the DOM
      expect(screen.getByText(/\$250\.00/)).toBeInTheDocument()
      // It must be wrapped in an element with data-ph-mask
      const maskedElements = document.querySelectorAll('[data-ph-mask]')
      expect(maskedElements.length).toBeGreaterThan(0)
    })

    it('shows loading state while fetching', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
      expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('shows empty state when no contributions', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
      expect(screen.getByText(/no contributions/i)).toBeInTheDocument()
    })

    it('shows "Log contribution" button', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
      expect(screen.getByRole('button', { name: /log contribution/i })).toBeInTheDocument()
    })

    // R4-2: rejected tombstones must not be rendered (the component's own header doc says
    // "Rejected contributions are not shown"). Without the fix, status='rejected' rows
    // fall through the `isProposed` check (not 'proposed') → rendered as "Confirmed".
    it('R4-2: does not render a rejected tombstone row', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockProposed, mockConfirmed, mockRejected],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      // The $300 rejected amount must NOT appear in the UI.
      expect(screen.queryByText(/\$300\.00/)).not.toBeInTheDocument()
    })

    it('R4-2: rejected tombstone is not labeled Confirmed', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockRejected],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      // With only a rejected contribution, the sheet should show empty state, not a "Confirmed" badge.
      expect(screen.queryByText(/confirmed/i)).not.toBeInTheDocument()
      // It should show the empty state message instead.
      expect(screen.getByText(/no contributions/i)).toBeInTheDocument()
    })

    it('R4-2: proposed and confirmed rows still render correctly when rejected row also present', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockProposed, mockConfirmed, mockRejected],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      // proposed and confirmed still appear
      expect(screen.getByText(/\$150\.00/)).toBeInTheDocument()
      expect(screen.getByText(/\$250\.00/)).toBeInTheDocument()
      // rejected row amount is hidden
      expect(screen.queryByText(/\$300\.00/)).not.toBeInTheDocument()
    })
  })

  // #1625 Task 6: `archived_at` is set when a goal's progress_basis round-trips; the row is
  // retained and shown but excluded from `current_amount`, and both Confirm and Reject now
  // 404 an archived contribution server-side.
  describe('Archived contributions (#1625)', () => {
    it('does not render an archived, once-confirmed row as plain "Confirmed"', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockArchived],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      expect(screen.queryByText(/^Confirmed$/)).not.toBeInTheDocument()
      expect(screen.getByText('Closed period')).toBeInTheDocument()
    })

    it('explains why an archived row does not count, and keeps it visible', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockArchived],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      // Retention policy: the row stays visible, amount and all.
      expect(screen.getByText(/\$400\.00/)).toBeInTheDocument()
      // Explains the exclusion in plain language.
      expect(screen.getByText(/does not count toward the current progress/i)).toBeInTheDocument()
    })

    it('does not offer Confirm/Reject on an archived row, even one still in "proposed" status', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockArchivedProposed],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      expect(screen.queryByRole('button', { name: /confirm/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /reject/i })).not.toBeInTheDocument()
      // Delete stays reachable — destroying retained history is still a meaningful, permanent act.
      expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument()
      expect(screen.getByText('Closed period')).toBeInTheDocument()
    })

    it('does not label an archived row as "Proposed" either', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockArchivedProposed],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      expect(screen.queryByText(/^Proposed$/)).not.toBeInTheDocument()
    })

    it('marks the Delete confirmation copy as already-excluded, not merely permanent', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockArchived],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: /delete/i }))
      const dialog = screen.getByRole('alertdialog')
      const text = dialog.textContent ?? ''

      expect(text).toMatch(/does not count toward this goal's current progress/i)
      expect(text).toMatch(/permanent/i)
      // Still no false restore/undo claim.
      expect(text).not.toMatch(/restore|undelete|recover|undo/i)
    })
  })

  describe('Analytics', () => {
    it('fires SAVINGS_CONTRIBUTIONS_VIEWED when opened', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
      expect(mockTrackEvent).toHaveBeenCalledWith('savings_contributions_viewed', expect.any(Object))
    })
  })

  describe('Confirm action', () => {
    it('calls confirm mutation and onContributionChanged on Confirm click', async () => {
      mockConfirmSavingsGoalContributionMutateAsync.mockResolvedValueOnce({
        ...mockProposed, status: 'confirmed',
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

      await waitFor(() => {
        expect(mockConfirmSavingsGoalContributionMutateAsync).toHaveBeenCalledWith({
          goalID: goalId,
          id: mockProposed.id,
        })
        expect(mockOnChange).toHaveBeenCalled()
      })
    })

    it('fires SAVINGS_CONTRIBUTION_CONFIRMED analytics on confirm', async () => {
      mockConfirmSavingsGoalContributionMutateAsync.mockResolvedValueOnce(mockProposed)

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          'savings_contribution_confirmed',
          expect.any(Object)
        )
      })
    })
  })

  // #1569 R18 T1 — Delete is a PERMANENT hard delete of a hand-logged amount
  // (savings_contribution_repository.go DeleteContribution: `DELETE FROM
  // savings_goal_contributions`, no tombstone, no restore path anywhere on this branch), and it
  // was the only explicit-destruction path a user could fire in a single click with no
  // confirmation of any kind. It now goes through the same ConfirmationDialog DeletedGoalsPanel
  // uses for its purge.
  describe('Delete action', () => {
    /** The row's Delete button, not the dialog's confirm button (both match /delete/i). */
    function rowDeleteButton() {
      return screen.getByRole('button', { name: 'Delete' })
    }
    function dialogConfirmButton() {
      return screen.getByRole('button', { name: /delete forever/i })
    }

    it('does NOT call the delete mutation on the first click — it only raises the confirmation', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(rowDeleteButton())

      // The whole point: a dialog that renders but does not gate is a no-op.
      expect(mockDeleteSavingsGoalContributionMutateAsync).not.toHaveBeenCalled()
      expect(mockTrackEvent).not.toHaveBeenCalledWith(
        'savings_contribution_deleted',
        expect.any(Object)
      )
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    })

    it('does NOT call the delete mutation when the confirmation is cancelled', async () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(rowDeleteButton())
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))

      await waitFor(() => {
        expect(screen.queryByRole('alertdialog')).toBeNull()
      })
      expect(mockDeleteSavingsGoalContributionMutateAsync).not.toHaveBeenCalled()
      expect(mockOnChange).not.toHaveBeenCalled()
    })

    it('calls delete mutation and onContributionChanged once the confirmation is confirmed', async () => {
      mockDeleteSavingsGoalContributionMutateAsync.mockResolvedValueOnce(undefined)

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(rowDeleteButton())
      fireEvent.click(dialogConfirmButton())

      await waitFor(() => {
        expect(mockDeleteSavingsGoalContributionMutateAsync).toHaveBeenCalledWith({
          goalID: goalId,
          id: mockConfirmed.id,
        })
        expect(mockOnChange).toHaveBeenCalled()
      })
    })

    it('fires SAVINGS_CONTRIBUTION_DELETED analytics only after confirmation', async () => {
      mockDeleteSavingsGoalContributionMutateAsync.mockResolvedValueOnce(undefined)

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(rowDeleteButton())
      fireEvent.click(dialogConfirmButton())

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          'savings_contribution_deleted',
          expect.any(Object)
        )
      })
    })

    it('names the amount and date, and never promises the contribution can be brought back', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(rowDeleteButton())
      const dialog = screen.getByRole('alertdialog')
      const text = dialog.textContent ?? ''

      expect(text).toMatch(/\$250\.00/)
      expect(text).toMatch(/Jun 8, 2026/)
      expect(text).toMatch(/permanently/i)
      // A false retention/restore claim is the exact class this branch keeps having to remove.
      expect(text).not.toMatch(/restore|undelete|recover|undo/i)
      // The amount in the dialog is PII like the amount in the row.
      expect(dialog.querySelectorAll('[data-ph-mask]').length).toBeGreaterThan(0)
    })

    it('keeps the amount masked and does not confirm Reject (Reject is a tombstone, not a delete)', () => {
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: /reject/i }))
      expect(screen.queryByRole('alertdialog')).toBeNull()
    })
  })

  describe('Reject action (R3-7)', () => {
    it('calls reject mutation and onContributionChanged on Reject click', async () => {
      mockRejectSavingsGoalContributionMutateAsync.mockResolvedValueOnce({
        ...mockProposed, status: 'rejected',
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: /reject/i }))

      await waitFor(() => {
        expect(mockRejectSavingsGoalContributionMutateAsync).toHaveBeenCalledWith({
          goalID: goalId,
          id: mockProposed.id,
        })
        expect(mockOnChange).toHaveBeenCalled()
      })
    })

    it('fires SAVINGS_CONTRIBUTION_REJECTED analytics on reject', async () => {
      mockRejectSavingsGoalContributionMutateAsync.mockResolvedValueOnce({
        ...mockProposed, status: 'rejected',
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: /reject/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          'savings_contribution_rejected',
          expect.any(Object)
        )
      })
    })
  })

  // Issue #1569 T9.5 — a failed read must not render as "No contributions yet". This sheet's own
  // twin, DeletedGoalContributions, already refuses to do that; the live ledger did not.
  describe('failed read', () => {
    it('reports the failed read and does not claim there are no contributions', () => {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: undefined,
        isLoading: false,
        // Widened: the mock is typed from its default `error: null`.
        error: new Error('network down') as unknown as null,
        refetch: mock(() => Promise.resolve({})),
      })

      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      expect(screen.getByRole('alert')).toBeInTheDocument()
      expect(screen.getByText(/couldn't load/i)).toBeInTheDocument()
      expect(screen.queryByText(/No contributions yet/i)).toBeNull()
    })

    // ── #1569 T9-FIX item 7: rows that SURVIVED a failed refetch ────────────────────────
    //
    // The test above uses `data: undefined`, so it cannot see this at all. React Query keeps the
    // last successful listing when a later refetch fails, so `data` can be real rows AND `error`
    // truthy at the same instant — and this sheet rendered the error notice AND the full row
    // list, verbs included. Confirm/Reject/Delete then act on a row we have just said we cannot
    // vouch for; Reject writes a sticky tombstone and Delete destroys a hand-logged
    // contribution outright.
    //
    // Same remedy DeletedGoalsPanel took in round 8, and for the same reason: keep the rows —
    // they are the user's, and blanking them is the false "nothing here" by another route — and
    // disarm the verbs until the listing can be refreshed.
    function withStaleRows() {
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockProposed, mockConfirmed],
        isLoading: false,
        error: new Error('network down') as unknown as null,
        refetch: mock(() => Promise.resolve({})),
      })
      renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )
    }

    it('keeps the rows that survived the failed refetch', () => {
      withStaleRows()
      expect(screen.getByLabelText('Contributions list')).toBeInTheDocument()
      expect(screen.queryByText(/No contributions yet/i)).toBeNull()
    })

    it('disarms every verb on rows it can no longer vouch for', () => {
      withStaleRows()

      expect(screen.getByRole('button', { name: /confirm/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /reject/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled()
    })

    // Second half of the disarm, the hole DeletedGoalsPanel's first draft left behind: the
    // confirmation can already be OPEN when the refetch fails, and its confirm button is not the
    // one the row's `disabled` prop touches.
    it('closes an already-open delete confirmation when the listing goes stale', async () => {
      const { rerender } = renderWithQueryClient(
        <GoalContributionsSheet
          goalId={goalId}
          goalName={goalName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onContributionChanged={mockOnChange}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()

      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [mockProposed, mockConfirmed],
        isLoading: false,
        error: new Error('network down') as unknown as null,
        refetch: mock(() => Promise.resolve({})),
      })
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      rerender(
        <QueryClientProvider client={queryClient}>
          <GoalContributionsSheet
            goalId={goalId}
            goalName={goalName}
            open={true}
            onOpenChange={mockOnOpenChange}
            onContributionChanged={mockOnChange}
          />
        </QueryClientProvider>
      )

      await waitFor(() => {
        expect(screen.queryByRole('alertdialog')).toBeNull()
      })
      expect(mockDeleteSavingsGoalContributionMutateAsync).not.toHaveBeenCalled()
    })

    it('says the listing may be stale rather than only that a request failed', () => {
      withStaleRows()

      const notice = screen.getByRole('status')
      expect(notice.textContent ?? '').toMatch(/out of date/i)
      // A NEGATIVE assertion, like DeletedGoalsPanel's: an unscoped "everything is still kept"
      // here would be the same false retention claim this branch keeps re-shipping.
      expect(notice.textContent ?? '').not.toMatch(/no contributions/i)
    })
  })
})
