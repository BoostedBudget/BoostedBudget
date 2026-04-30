import { describe, it, expect, mock, beforeEach } from 'bun:test'
import React from 'react'

const mockMutateAsync = mock(() => Promise.resolve())
const mockUseCreateSavingsGoal = mock(() => ({
  mutateAsync: mockMutateAsync,
  isPending: false,
}))
const mockUseGetAccounts = mock(() => ({
  data: null,
  isLoading: false,
}))

mock.module('@boostedbudget/api-client/endpoints/savings-goals/savings-goals', () => ({
  useCreateSavingsGoal: () => mockUseCreateSavingsGoal(),
}))

mock.module('@boostedbudget/api-client/endpoints/accounts/accounts', () => ({
  useGetAccounts: (_params: unknown, options: { query: { enabled: boolean } }) => mockUseGetAccounts(options),
}))

mock.module('../goalTemplates', () => ({
  GOAL_TEMPLATES: [
    { id: 'emergency', name: 'Emergency Fund', defaultMonths: 12 },
    { id: 'vacation', name: 'Vacation', defaultMonths: 6 },
    { id: 'custom', name: 'Custom', defaultMonths: null },
  ],
  calculateDeadlineFromTemplate: (template: { id: string; defaultMonths: number | null }) => {
    if (!template.defaultMonths) return null
    const date = new Date()
    date.setMonth(date.getMonth() + template.defaultMonths)
    return date.toISOString().split('T')[0]
  },
}))

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CreateSavingsGoalDialog } from './CreateSavingsGoalDialog'

const mockAccounts = [
  { id: 'acc-1', name: 'Checking', type: 'depository', mask: '1234', institution_name: 'Test Bank' },
  { id: 'acc-2', name: 'Savings', type: 'depository', mask: '5678', institution_name: 'Test Bank' },
  { id: 'acc-3', name: 'Credit Card', type: 'credit', mask: '9012', institution_name: 'Test Bank' },
]

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

describe('CreateSavingsGoalDialog', () => {
  const mockOnCancel = mock(() => {})
  const mockOnGoalCreated = mock(() => {})

  beforeEach(() => {
    mockMutateAsync.mockClear()
    mockUseCreateSavingsGoal.mockClear()
    mockUseGetAccounts.mockClear()
    mockOnCancel.mockClear()
    mockOnGoalCreated.mockClear()
    mockUseCreateSavingsGoal.mockReturnValue({
      mutateAsync: mockMutateAsync,
      isPending: false,
    })
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: mockAccounts },
      isLoading: false,
    })
  })

  describe('Rendering', () => {
    it('should render dialog when open', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      expect(screen.getByText('Create Savings Goal')).toBeInTheDocument()
    })

    it('should not render dialog when closed', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      expect(screen.queryByText('Create Savings Goal')).not.toBeInTheDocument()
    })

    it('should render template options', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      expect(screen.getByText('Emergency Fund')).toBeInTheDocument()
      expect(screen.getByText('Vacation')).toBeInTheDocument()
      expect(screen.getByText('Custom')).toBeInTheDocument()
    })

    it('should render form fields', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      expect(screen.getByLabelText('Name')).toBeInTheDocument()
      expect(screen.getByLabelText('Description')).toBeInTheDocument()
      expect(screen.getByLabelText('Target Amount')).toBeInTheDocument()
      expect(screen.getByLabelText('Account')).toBeInTheDocument()
      expect(screen.getByLabelText('Deadline (optional)')).toBeInTheDocument()
    })

    it('should render only depository accounts in dropdown', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const accountSelect = screen.getByLabelText('Account')
      expect(accountSelect).toBeInTheDocument()

      expect(screen.getByText(/Checking.*1234.*Test Bank/)).toBeInTheDocument()
      expect(screen.getByText(/Savings.*5678.*Test Bank/)).toBeInTheDocument()
    })
  })

  describe('Template Selection', () => {
    it('should update name when template selected', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.click(screen.getByText('Emergency Fund'))

      const nameInput = screen.getByLabelText('Name') as HTMLInputElement
      expect(nameInput.value).toBe('Emergency Fund')
    })

    it('should not update name when Custom template selected', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const nameInput = screen.getByLabelText('Name') as HTMLInputElement
      fireEvent.change(nameInput, { target: { value: 'My Custom Goal' } })

      fireEvent.click(screen.getByText('Custom'))

      expect(nameInput.value).toBe('My Custom Goal')
    })

    it('should highlight selected template', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const emergencyButton = screen.getByText('Emergency Fund').closest('button')
      fireEvent.click(emergencyButton!)

      expect(emergencyButton?.className).toContain('selected')
    })
  })

  describe('Form Input', () => {
    it('should update form values on input change', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const nameInput = screen.getByLabelText('Name') as HTMLInputElement
      const amountInput = screen.getByLabelText('Target Amount') as HTMLInputElement

      fireEvent.change(nameInput, { target: { value: 'New Car' } })
      fireEvent.change(amountInput, { target: { value: '15000' } })

      expect(nameInput.value).toBe('New Car')
      expect(amountInput.value).toBe('15000')
    })

    it('should update description field', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const descInput = screen.getByLabelText('Description') as HTMLTextAreaElement
      fireEvent.change(descInput, { target: { value: 'For my dream vacation' } })

      expect(descInput.value).toBe('For my dream vacation')
    })

    it('should update account selection', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const accountSelect = screen.getByLabelText('Account') as HTMLSelectElement
      fireEvent.change(accountSelect, { target: { value: 'acc-1' } })

      expect(accountSelect.value).toBe('acc-1')
    })
  })

  describe('Form Validation', () => {
    it('should show error when name is empty', async () => {
      const user = userEvent.setup()
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      await user.click(screen.getByRole('button', { name: /create goal/i }))

      expect(await screen.findByText('Name is required')).toBeInTheDocument()
    })

    it('should show error when target amount is invalid', async () => {
      const user = userEvent.setup()
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      await user.type(screen.getByLabelText('Name'), 'Test Goal')
      await user.click(screen.getByRole('button', { name: /create goal/i }))

      expect(await screen.findByText('Amount must be greater than 0')).toBeInTheDocument()
    })

    it('should show error when account not selected', async () => {
      const user = userEvent.setup()
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      await user.type(screen.getByLabelText('Name'), 'Test Goal')
      await user.clear(screen.getByLabelText('Target Amount'))
      await user.type(screen.getByLabelText('Target Amount'), '1000')

      // Submit the form
      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText('Please select an account')).toBeInTheDocument()
    })

    it('should clear error when field is corrected', async () => {
      const user = userEvent.setup()
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      await user.click(screen.getByRole('button', { name: /create goal/i }))

      expect(await screen.findByText('Name is required')).toBeInTheDocument()

      await user.type(screen.getByLabelText('Name'), 'Fixed Name')

      expect(screen.queryByText('Name is required')).not.toBeInTheDocument()
    })
  })

  describe('Form Submission', () => {
    it('should call mutateAsync on valid submission', async () => {
      const newGoal = { id: 'goal-1', name: 'Test Goal', target_amount: 1000 }
      mockMutateAsync.mockResolvedValueOnce(newGoal)

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Test Goal' } })
      fireEvent.change(screen.getByLabelText('Target Amount'), { target: { value: '1000' } })
      fireEvent.change(screen.getByLabelText('Account'), { target: { value: 'acc-1' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalledWith({
          data: {
            name: 'Test Goal',
            target_amount: 1000,
            account_id: 'acc-1',
            description: undefined,
            deadline: undefined,
          },
        })
      })
    })

    it('should call onGoalCreated after successful submission', async () => {
      const newGoal = { id: 'goal-1', name: 'Test Goal', target_amount: 1000 }
      mockMutateAsync.mockResolvedValueOnce(newGoal)

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Test Goal' } })
      fireEvent.change(screen.getByLabelText('Target Amount'), { target: { value: '1000' } })
      fireEvent.change(screen.getByLabelText('Account'), { target: { value: 'acc-1' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnGoalCreated).toHaveBeenCalledWith(newGoal)
      })
    })

    it('converts deadline from YYYY-MM-DD to RFC3339 before submission', async () => {
      const newGoal = { id: 'goal-1', name: 'Vacation Fund', target_amount: 5000 }
      mockMutateAsync.mockResolvedValueOnce(newGoal)

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Vacation Fund' } })
      fireEvent.change(screen.getByLabelText('Target Amount'), { target: { value: '5000' } })
      fireEvent.change(screen.getByLabelText('Account'), { target: { value: 'acc-1' } })
      fireEvent.change(screen.getByLabelText('Deadline (optional)'), { target: { value: '2026-12-31' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            data: expect.objectContaining({
              deadline: '2026-12-31T00:00:00.000Z',
            }),
          })
        )
      })
    })

    it('should show API error on submission failure', async () => {
      mockMutateAsync.mockRejectedValueOnce(new Error('Network error'))

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Test Goal' } })
      fireEvent.change(screen.getByLabelText('Target Amount'), { target: { value: '1000' } })
      fireEvent.change(screen.getByLabelText('Account'), { target: { value: 'acc-1' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText('Network error')).toBeInTheDocument()
    })

    it('should show Creating... text and disable button when submitting', () => {
      mockUseCreateSavingsGoal.mockReturnValue({
        mutateAsync: mockMutateAsync,
        isPending: true,
      })

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const creatingButton = screen.getByText('Creating...')
      expect(creatingButton).toBeInTheDocument()
      expect(creatingButton).toBeDisabled()
    })
  })

  describe('Dialog Actions', () => {
    it('should call onCancel when Cancel or X button clicked', () => {
      const { rerender } = renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.click(screen.getByText('Cancel'))
      expect(mockOnCancel).toHaveBeenCalledTimes(1)

      mockOnCancel.mockClear()

      rerender(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const closeButton = screen.getByRole('button', { name: '' })
      fireEvent.click(closeButton)
      expect(mockOnCancel).toHaveBeenCalled()
    })
  })

  describe('Loading States', () => {
    it('should show Loading accounts... when accounts are loading', () => {
      mockUseGetAccounts.mockReturnValue({
        data: null,
        isLoading: true,
      })

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      expect(screen.getByText('Loading accounts...')).toBeInTheDocument()
    })

    it('should disable account select when loading', () => {
      mockUseGetAccounts.mockReturnValue({
        data: null,
        isLoading: true,
      })

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      expect(screen.getByLabelText('Account')).toBeDisabled()
    })

    it('should show warning when no accounts available', () => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [] },
        isLoading: false,
      })

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      expect(screen.getByText('No savings accounts found. Link an account first.')).toBeInTheDocument()
    })
  })

  describe('Edge Cases', () => {
    it('should include description and deadline when provided', async () => {
      const newGoal = { id: 'goal-1', name: 'Test', target_amount: 500 }
      mockMutateAsync.mockResolvedValueOnce(newGoal)

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Vacation' } })
      fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'Beach trip' } })
      fireEvent.change(screen.getByLabelText('Target Amount'), { target: { value: '2000' } })
      fireEvent.change(screen.getByLabelText('Account'), { target: { value: 'acc-2' } })
      fireEvent.change(screen.getByLabelText('Deadline (optional)'), { target: { value: '2030-12-31' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalledWith({
          data: {
            name: 'Vacation',
            description: 'Beach trip',
            target_amount: 2000,
            account_id: 'acc-2',
            deadline: '2030-12-31T00:00:00.000Z',
          },
        })
      })
    })

    it('should handle non-Error rejection', async () => {
      mockMutateAsync.mockRejectedValueOnce('Unknown error')

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Test' } })
      fireEvent.change(screen.getByLabelText('Target Amount'), { target: { value: '100' } })
      fireEvent.change(screen.getByLabelText('Account'), { target: { value: 'acc-1' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText('Failed to create goal')).toBeInTheDocument()
    })
  })
})
