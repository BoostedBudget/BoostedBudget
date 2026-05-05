import { describe, it, expect, mock, beforeEach } from 'bun:test'
import React from 'react'

const mockMutateAsync = mock(() => Promise.resolve())
const mockUseUpdateSavingsGoal = mock(() => ({
  mutateAsync: mockMutateAsync,
  isPending: false,
}))

const mockTrackEvent = mock(() => {})
const mockUseTrackEvent = mock(() => mockTrackEvent)

mock.module('@boostedbudget/api-client/endpoints/savings-goals/savings-goals', () => ({
  useUpdateSavingsGoal: () => mockUseUpdateSavingsGoal(),
}))

mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockUseTrackEvent(),
}))

mock.module('@/shared/analytics/events', () => ({
  ANALYTICS_EVENTS: {
    SAVINGS_GOAL_EDITED: 'savings_goal_edited',
  },
}))

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { UpdateSavingsGoalDialog } from './UpdateSavingsGoalDialog'
import type { SavingsGoal } from '../types'

const mockGoal: SavingsGoal = {
  id: 'goal-1',
  name: 'Vacation Fund',
  description: 'Beach trip savings',
  target_amount: 5000,
  current_amount: 1000,
  account_id: 'acc-1',
  deadline: '2027-06-15T00:00:00Z',
  status: 'active',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

function renderWithQueryClient(ui: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      {ui}
    </QueryClientProvider>
  )
}

describe('UpdateSavingsGoalDialog', () => {
  const mockOnCancel = mock(() => {})
  const mockOnGoalUpdated = mock(() => {})

  beforeEach(() => {
    mockMutateAsync.mockClear()
    mockUseUpdateSavingsGoal.mockClear()
    mockTrackEvent.mockClear()
    mockUseTrackEvent.mockClear()
    mockOnCancel.mockClear()
    mockOnGoalUpdated.mockClear()
    mockUseUpdateSavingsGoal.mockReturnValue({
      mutateAsync: mockMutateAsync,
      isPending: false,
    })
  })

  describe('Rendering', () => {
    it('does not render when goal is null', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={false}
          goal={null}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      expect(screen.queryByText('Edit Savings Goal')).not.toBeInTheDocument()
    })

    it('shows "Edit Savings Goal" title when open', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      expect(screen.getByText('Edit Savings Goal')).toBeInTheDocument()
    })

    it('pre-fills name field from goal.name', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const nameInput = screen.getByLabelText('Name') as HTMLInputElement
      expect(nameInput.value).toBe('Vacation Fund')
    })

    it('pre-fills target_amount from goal.target_amount', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const amountInput = screen.getByLabelText('Target Amount') as HTMLInputElement
      expect(amountInput.value).toBe('5000')
    })

    it('pre-fills deadline from goal.deadline (split at T)', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const deadlineInput = screen.getByLabelText('Deadline (optional)') as HTMLInputElement
      expect(deadlineInput.value).toBe('2027-06-15')
    })

    it('pre-fills description from goal.description', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const descInput = screen.getByLabelText('Description') as HTMLTextAreaElement
      expect(descInput.value).toBe('Beach trip savings')
    })

    it('leaves deadline empty when goal has no deadline', () => {
      const goalNoDeadline = { ...mockGoal, deadline: undefined }
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={goalNoDeadline as SavingsGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const deadlineInput = screen.getByLabelText('Deadline (optional)') as HTMLInputElement
      expect(deadlineInput.value).toBe('')
    })

    it('does not render account field', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      expect(screen.queryByLabelText('Account')).not.toBeInTheDocument()
    })
  })

  describe('Form Validation', () => {
    it('shows validation error if name is empty', async () => {
      const user = userEvent.setup()
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const nameInput = screen.getByLabelText('Name') as HTMLInputElement
      await user.clear(nameInput)

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText('Name is required')).toBeInTheDocument()
    })

    it('shows validation error if target_amount is 0', async () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      fireEvent.change(screen.getByLabelText('Target Amount'), { target: { value: '0' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText('Amount must be greater than 0')).toBeInTheDocument()
    })

    it('shows validation error if target_amount is negative', async () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      fireEvent.change(screen.getByLabelText('Target Amount'), { target: { value: '-100' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText('Amount must be greater than 0')).toBeInTheDocument()
    })
  })

  describe('Form Submission', () => {
    it('calls update mutation with correct payload on submit', async () => {
      const updatedGoal = { ...mockGoal, name: 'Updated Vacation' }
      mockMutateAsync.mockResolvedValueOnce(updatedGoal)

      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Updated Vacation' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalledWith({
          id: 'goal-1',
          data: expect.objectContaining({
            name: 'Updated Vacation',
            target_amount: 5000,
          }),
        })
      })
    })

    it('calls onGoalUpdated callback after successful update', async () => {
      const updatedGoal = { ...mockGoal, name: 'Updated' }
      mockMutateAsync.mockResolvedValueOnce(updatedGoal)

      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnGoalUpdated).toHaveBeenCalledWith(updatedGoal)
      })
    })

    it('fires SAVINGS_GOAL_EDITED analytics event after update', async () => {
      const updatedGoal = { ...mockGoal }
      mockMutateAsync.mockResolvedValueOnce(updatedGoal)

      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith('savings_goal_edited')
      })
    })

    it('shows API error if update fails', async () => {
      mockMutateAsync.mockRejectedValueOnce(new Error('Network error'))

      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText('Network error')).toBeInTheDocument()
    })

    it('shows "Saving..." and disables button when submitting', () => {
      mockUseUpdateSavingsGoal.mockReturnValue({
        mutateAsync: mockMutateAsync,
        isPending: true,
      })

      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      const savingButton = screen.getByText('Saving...')
      expect(savingButton).toBeInTheDocument()
      expect(savingButton).toBeDisabled()
    })

    it('converts deadline to ISO string before submission', async () => {
      const updatedGoal = { ...mockGoal }
      mockMutateAsync.mockResolvedValueOnce(updatedGoal)

      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      fireEvent.change(screen.getByLabelText('Deadline (optional)'), { target: { value: '2028-03-01' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            data: expect.objectContaining({
              deadline: '2028-03-01T00:00:00.000Z',
            }),
          })
        )
      })
    })
  })

  describe('Dialog Actions', () => {
    it('calls onCancel when Cancel button is clicked', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      fireEvent.click(screen.getByText('Cancel'))
      expect(mockOnCancel).toHaveBeenCalledTimes(1)
    })

    it('calls onCancel when X button is clicked', () => {
      renderWithQueryClient(
        <UpdateSavingsGoalDialog
          isOpen={true}
          goal={mockGoal}
          onCancel={mockOnCancel}
          onGoalUpdated={mockOnGoalUpdated}
        />
      )

      // The X button contains the XIcon — find the button near it
      const xButton = document.querySelector('.update-goal-dialog button[type="button"]')
      fireEvent.click(xButton!)
      expect(mockOnCancel).toHaveBeenCalled()
    })
  })
})
