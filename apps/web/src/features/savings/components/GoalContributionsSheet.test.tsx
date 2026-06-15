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

const mockTrackEvent = mock(() => {})
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

import * as RealAnalyticsEvents from '@/shared/analytics/events'
mock.module('@/shared/analytics/events', () => ({
  ...RealAnalyticsEvents,
  ANALYTICS_EVENTS: {
    ...RealAnalyticsEvents.ANALYTICS_EVENTS,
    SAVINGS_CONTRIBUTIONS_VIEWED: 'savings_contributions_viewed',
    SAVINGS_CONTRIBUTION_CONFIRMED: 'savings_contribution_confirmed',
    SAVINGS_CONTRIBUTION_REJECTED: 'savings_contribution_rejected', // R3-7
    SAVINGS_CONTRIBUTION_DELETED: 'savings_contribution_deleted',
  },
}))

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

  describe('Delete action', () => {
    it('calls delete mutation and onContributionChanged on Delete click', async () => {
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

      // Find delete buttons (there should be at least one — for the confirmed row)
      const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
      expect(deleteButtons.length).toBeGreaterThan(0)
      fireEvent.click(deleteButtons[0])

      await waitFor(() => {
        expect(mockDeleteSavingsGoalContributionMutateAsync).toHaveBeenCalled()
        expect(mockOnChange).toHaveBeenCalled()
      })
    })

    it('fires SAVINGS_CONTRIBUTION_DELETED analytics on delete', async () => {
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

      const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
      fireEvent.click(deleteButtons[0])

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          'savings_contribution_deleted',
          expect.any(Object)
        )
      })
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
})
