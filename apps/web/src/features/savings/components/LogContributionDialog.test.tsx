// Uses preloaded API mocks from bunfig.toml — do NOT add mock.module() for
// savings-goals here; that is centralized in src/test/mocks/savings-goals-api.ts.

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import React from 'react'
import {
  mockUseLogSavingsGoalContribution,
  mockLogSavingsGoalContributionMutateAsync,
} from '@/test/mocks/savings-goals-api'

import { mockTrackEvent } from '@/test/mocks/track-event'

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { LogContributionDialog } from './LogContributionDialog'

function renderWithQueryClient(ui: React.ReactNode) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>)
}

describe('LogContributionDialog', () => {
  const goalId = 'goal-1'
  const mockOnClose = mock(() => {})
  const mockOnLogged = mock(() => {})

  beforeEach(() => {
    mockLogSavingsGoalContributionMutateAsync.mockClear()
    mockUseLogSavingsGoalContribution.mockClear()
    mockTrackEvent.mockClear()
    mockOnClose.mockClear()
    mockOnLogged.mockClear()
    mockUseLogSavingsGoalContribution.mockReturnValue({
      mutateAsync: mockLogSavingsGoalContributionMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
  })

  describe('Rendering', () => {
    it('renders a malformed currency as a literal input marker', () => {
      renderWithQueryClient(<LogContributionDialog goalId={goalId} isOpen onClose={mockOnClose} onLogged={mockOnLogged} currencyCode="U$D" />)
      expect(screen.getByRole('spinbutton', { name: 'Amount (U$D)' })).toBeInTheDocument()
      expect(screen.getByText('U$D')).toBeInTheDocument()
    })

    it('renders dialog title when open', () => {
      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={true}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /log contribution/i })).toBeInTheDocument()
    })

    it('does not render when closed', () => {
      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={false}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    it('renders amount and date fields', () => {
      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={true}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )
      expect(screen.getByLabelText(/amount/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/date/i)).toBeInTheDocument()
    })
  })

  describe('Validation', () => {
    it('shows error when amount is zero', async () => {
      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={true}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )
      const amountInput = screen.getByLabelText(/amount/i)
      fireEvent.change(amountInput, { target: { value: '0' } })
      fireEvent.click(screen.getByRole('button', { name: /log contribution/i }))

      await waitFor(() => {
        expect(screen.getByText(/must be greater than zero/i)).toBeInTheDocument()
      })
      expect(mockLogSavingsGoalContributionMutateAsync).not.toHaveBeenCalled()
    })

    it('shows error when amount is negative', async () => {
      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={true}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )
      const amountInput = screen.getByLabelText(/amount/i)
      fireEvent.change(amountInput, { target: { value: '-50' } })
      fireEvent.click(screen.getByRole('button', { name: /log contribution/i }))

      await waitFor(() => {
        expect(screen.getByText(/must be greater than zero/i)).toBeInTheDocument()
      })
      expect(mockLogSavingsGoalContributionMutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('Submission', () => {
    it('calls mutate with correct goalId and amount on submit', async () => {
      mockLogSavingsGoalContributionMutateAsync.mockResolvedValueOnce({
        id: 'c-1', amount: 250, source: 'manual', status: 'confirmed',
        savings_goal_id: goalId, confirmed_by_user: true, managed_by_reconciliation: false,
        occurred_on: '2026-06-14', created_at: '2026-06-14T00:00:00Z', updated_at: '2026-06-14T00:00:00Z',
      })

      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={true}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )

      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '250' } })
      fireEvent.click(screen.getByRole('button', { name: /log contribution/i }))

      await waitFor(() => {
        expect(mockLogSavingsGoalContributionMutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            goalID: goalId,
            data: expect.objectContaining({ amount: 250 }),
          })
        )
      })
    })

    it('calls onLogged after successful submission', async () => {
      mockLogSavingsGoalContributionMutateAsync.mockResolvedValueOnce({
        id: 'c-1', amount: 100, source: 'manual', status: 'confirmed',
        savings_goal_id: goalId, confirmed_by_user: true, managed_by_reconciliation: false,
        occurred_on: '2026-06-14', created_at: '2026-06-14T00:00:00Z', updated_at: '2026-06-14T00:00:00Z',
      })

      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={true}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )

      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '100' } })
      fireEvent.click(screen.getByRole('button', { name: /log contribution/i }))

      await waitFor(() => {
        expect(mockOnLogged).toHaveBeenCalled()
      })
    })

    it('fires SAVINGS_CONTRIBUTION_LOGGED analytics event on success', async () => {
      mockLogSavingsGoalContributionMutateAsync.mockResolvedValueOnce({
        id: 'c-2', amount: 75, source: 'manual', status: 'confirmed',
        savings_goal_id: goalId, confirmed_by_user: true, managed_by_reconciliation: false,
        occurred_on: '2026-06-14', created_at: '2026-06-14T00:00:00Z', updated_at: '2026-06-14T00:00:00Z',
      })

      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={true}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )

      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '75' } })
      fireEvent.click(screen.getByRole('button', { name: /log contribution/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          'savings_contribution_logged',
          expect.any(Object)
        )
      })
    })

    it('calls onClose when cancel button clicked', () => {
      renderWithQueryClient(
        <LogContributionDialog
          goalId={goalId}
          isOpen={true}
          onClose={mockOnClose}
          onLogged={mockOnLogged}
        />
      )
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
      expect(mockOnClose).toHaveBeenCalled()
    })
  })
})
