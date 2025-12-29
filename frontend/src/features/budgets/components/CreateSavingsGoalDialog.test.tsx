/**
 * CreateSavingsGoalDialog Component Tests
 * Issue #564: Frontend Test Coverage Improvement
 *
 * Tests the savings goal creation dialog with form validation and API integration.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CreateSavingsGoalDialog } from './CreateSavingsGoalDialog'

// Mock mutation and query hooks
const mockMutateAsync = vi.fn()
const mockUseCreateSavingsGoal = vi.fn()
const mockUseGetAccounts = vi.fn()

vi.mock('@/api/generated/endpoints/savings-goals/savings-goals', () => ({
  useCreateSavingsGoal: () => mockUseCreateSavingsGoal(),
}))

vi.mock('@/api/generated/endpoints/accounts/accounts', () => ({
  useGetAccounts: (_params: unknown, options: { query: { enabled: boolean } }) => mockUseGetAccounts(options),
}))

// Mock goal templates
vi.mock('../goalTemplates', () => ({
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

// Mock accounts data
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
  const mockOnCancel = vi.fn()
  const mockOnGoalCreated = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
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

      // Check that depository accounts are present
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

      // First set a custom name
      const nameInput = screen.getByLabelText('Name') as HTMLInputElement
      fireEvent.change(nameInput, { target: { value: 'My Custom Goal' } })

      // Select Custom template
      fireEvent.click(screen.getByText('Custom'))

      // Name should remain unchanged
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
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.click(screen.getByText('Create Goal'))

      await waitFor(() => {
        expect(screen.getByText('Name is required')).toBeInTheDocument()
      })
    })

    it('should show error when target amount is invalid', async () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const nameInput = screen.getByLabelText('Name')
      fireEvent.change(nameInput, { target: { value: 'Test Goal' } })

      fireEvent.click(screen.getByText('Create Goal'))

      await waitFor(() => {
        expect(screen.getByText('Amount must be greater than 0')).toBeInTheDocument()
      })
    })

    it('should show error when account not selected', async () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      const nameInput = screen.getByLabelText('Name')
      const amountInput = screen.getByLabelText('Target Amount')

      fireEvent.change(nameInput, { target: { value: 'Test Goal' } })
      fireEvent.change(amountInput, { target: { value: '1000' } })

      fireEvent.click(screen.getByText('Create Goal'))

      await waitFor(() => {
        expect(screen.getByText('Please select an account')).toBeInTheDocument()
      })
    })

    it('should clear error when field is corrected', async () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.click(screen.getByText('Create Goal'))

      await waitFor(() => {
        expect(screen.getByText('Name is required')).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText('Name')
      fireEvent.change(nameInput, { target: { value: 'Fixed Name' } })

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

      fireEvent.click(screen.getByText('Create Goal'))

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

      fireEvent.click(screen.getByText('Create Goal'))

      await waitFor(() => {
        expect(mockOnGoalCreated).toHaveBeenCalledWith(newGoal)
      })
    })

    it('should show API error on submission failure', async () => {
      const user = userEvent.setup()
      mockMutateAsync.mockRejectedValueOnce(new Error('Network error'))

      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      // Use userEvent for proper async handling
      await user.clear(screen.getByLabelText('Name'))
      await user.type(screen.getByLabelText('Name'), 'Test Goal')
      await user.clear(screen.getByLabelText('Target Amount'))
      await user.type(screen.getByLabelText('Target Amount'), '1000')
      await user.selectOptions(screen.getByLabelText('Account'), 'acc-1')

      await user.click(screen.getByText('Create Goal'))

      expect(await screen.findByText('Network error')).toBeInTheDocument()
    })

    it('should show Creating... when submitting', () => {
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

      expect(screen.getByText('Creating...')).toBeInTheDocument()
    })

    it('should disable submit button when submitting', () => {
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

      expect(screen.getByText('Creating...')).toBeDisabled()
    })
  })

  describe('Dialog Actions', () => {
    it('should call onCancel when Cancel button clicked', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      fireEvent.click(screen.getByText('Cancel'))

      expect(mockOnCancel).toHaveBeenCalled()
    })

    it('should call onCancel when X button clicked', () => {
      renderWithQueryClient(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnGoalCreated}
        />
      )

      // Find the X button (close icon)
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
      fireEvent.change(screen.getByLabelText('Deadline (optional)'), { target: { value: '2025-12-31' } })

      fireEvent.click(screen.getByText('Create Goal'))

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalledWith({
          data: {
            name: 'Vacation',
            description: 'Beach trip',
            target_amount: 2000,
            account_id: 'acc-2',
            deadline: '2025-12-31',
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

      fireEvent.click(screen.getByText('Create Goal'))

      await waitFor(() => {
        expect(screen.getByText('Failed to create goal')).toBeInTheDocument()
      })
    })
  })
})
