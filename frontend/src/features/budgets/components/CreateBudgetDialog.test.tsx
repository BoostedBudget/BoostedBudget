import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { CreateBudgetDialog } from './CreateBudgetDialog'
import type { Budget } from '../types'

import {
  mockUseCreateBudget,
  mockCreateBudgetMutateAsync,
} from '@/test/mocks/budgets-api'
import { mockUseGetPlaidCategories } from '@/test/mocks/categories-api'
import { mockUseGetUserTags } from '@/test/mocks/tags-api'

const mockCreatedBudget: Budget = {
  id: 'budget-1',
  user_id: 'user-1',
  name: 'Monthly Budget',
  description: 'My monthly spending budget',
  period: 'monthly',
  start_date: '2025-01-01',
  end_date: '2025-01-31',
  amount: 2000,
  currency_code: 'USD',
  is_active: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
  income_amount: 0,
  income_sources: [],
}

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return render(ui, { wrapper: Wrapper })
}

// Helper to submit form - happy-dom doesn't trigger onSubmit with button click
function submitForm() {
  const form = document.querySelector('form')
  if (form) {
    fireEvent.submit(form)
  }
}

describe('CreateBudgetDialog', () => {
  const mockOnCreate = mock(() => {})
  const mockOnCancel = mock(() => {})

  beforeEach(() => {
    mockOnCreate.mockClear()
    mockOnCancel.mockClear()
    mockCreateBudgetMutateAsync.mockClear()
    mockUseCreateBudget.mockClear()
    mockUseGetPlaidCategories.mockClear()
    mockUseGetUserTags.mockClear()

    // Configure mutation to resolve successfully
    mockCreateBudgetMutateAsync.mockResolvedValue(mockCreatedBudget)
  })


  describe('Rendering', () => {
    it('renders dialog when isOpen is true', () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /create budget/i })).toBeInTheDocument()
    })

    it('does not render dialog when isOpen is false', () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    it('renders all form fields', () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.getByLabelText(/name/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/period/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/amount/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/start date/i)).toBeInTheDocument()
    })

    it('renders period options', () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      const periodSelect = screen.getByLabelText(/period/i) as HTMLSelectElement
      const optionTexts = Array.from(periodSelect.options).map(opt => opt.text.toLowerCase())

      expect(optionTexts).toContain('weekly')
      expect(optionTexts).toContain('monthly')
      expect(optionTexts).toContain('quarterly')
      expect(optionTexts).toContain('yearly')
      expect(optionTexts).toContain('custom')
    })

    it('shows end date field when custom period is selected', async () => {
      const user = userEvent.setup({ delay: null })
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.queryByLabelText(/end date/i)).not.toBeInTheDocument()

      await user.selectOptions(screen.getByLabelText(/period/i) as HTMLSelectElement, 'custom')

      await waitFor(() => {
        expect(screen.getByLabelText(/end date/i)).toBeInTheDocument()
      })
    })
  })

  describe('Validation', () => {
    it('shows error when name is empty', async () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '1000' } })
      submitForm()

      await waitFor(() => {
        expect(screen.getByText(/name is required/i)).toBeInTheDocument()
      })
    })

    it('shows error when amount is invalid', async () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Test Budget' } })
      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '0' } })
      submitForm()

      await waitFor(() => {
        expect(screen.getByText(/amount must be greater than 0/i)).toBeInTheDocument()
      })
    })

    it('shows error when end date is missing for custom period', async () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Test Budget' } })
      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '1000' } })
      fireEvent.change(screen.getByLabelText(/period/i), { target: { value: 'custom' } })
      submitForm()

      await waitFor(() => {
        expect(screen.getByText(/end date is required for custom period/i)).toBeInTheDocument()
      })
    })
  })

  describe('Submission', () => {
    it('calls createBudget mutation with correct payload on submit', async () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Monthly Budget' } })
      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '2000' } })
      submitForm()

      await waitFor(() => {
        expect(mockCreateBudgetMutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            data: expect.objectContaining({
              name: 'Monthly Budget',
              amount: 2000,
              period: 'monthly',
            }),
          })
        )
      })
    }, 15000)

    it('shows loading state during API call', async () => {
      mockCreateBudgetMutateAsync.mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(mockCreatedBudget), 3000))
      )

      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Test Budget' } })
      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '1000' } })
      submitForm()

      await waitFor(() => {
        expect(screen.getByText(/creating/i)).toBeInTheDocument()
      })
    }, 15000)

    it('closes dialog and calls onBudgetCreated on successful creation', async () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Monthly Budget' } })
      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '2000' } })
      submitForm()

      await waitFor(() => {
        expect(mockOnCreate).toHaveBeenCalled()
      })
    }, 15000)

    it('shows error message on API failure', async () => {
      mockCreateBudgetMutateAsync.mockRejectedValue(
        new Error('Failed to create budget')
      )

      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Test Budget' } })
      fireEvent.change(screen.getByLabelText(/amount/i), { target: { value: '1000' } })
      submitForm()

      await waitFor(() => {
        expect(screen.getByText(/failed to create budget/i)).toBeInTheDocument()
      })
    }, 15000)


    it('resets form when reopened after cancel', async () => {
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      })
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )
      const { rerender } = render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument()
      })

      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Test Budget' } })

      rerender(
        <CreateBudgetDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      rerender(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument()
      })

      expect(screen.getByLabelText(/name/i)).toHaveValue('')
    })
  })

  describe('Dialog Close Behavior (Issue #460)', () => {
    it('does not close when clicking outside the dialog (on backdrop)', async () => {
      const user = userEvent.setup({ delay: null })
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.getByRole('dialog')).toBeInTheDocument()

      const backdrop = document.querySelector('.fixed.inset-0.bg-black\\/25')
      if (backdrop) {
        await user.click(backdrop)
      }

      expect(mockOnCancel).not.toHaveBeenCalled()
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('closes when X button is clicked', async () => {
      const user = userEvent.setup({ delay: null })
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.getByRole('dialog')).toBeInTheDocument()

      const closeButtons = screen.getAllByRole('button')
      const xButton = closeButtons.find(btn =>
        btn.querySelector('svg') && !btn.textContent?.includes('Cancel') && !btn.textContent?.includes('Create')
      )

      if (xButton) {
        await user.click(xButton)
        expect(mockOnCancel).toHaveBeenCalled()
      }
    })

    it('closes when Cancel button is clicked', async () => {
      const user = userEvent.setup({ delay: null })
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      await user.click(screen.getByRole('button', { name: /cancel/i }))

      expect(mockOnCancel).toHaveBeenCalled()
    })
  })

  describe('Income Tab (Issue #496)', () => {
    it('renders Budget and Income tabs', () => {
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.getByRole('tab', { name: /budget/i })).toBeInTheDocument()
      expect(screen.getByRole('tab', { name: /income/i })).toBeInTheDocument()
    })

    it('switches between Budget and Income tabs', async () => {
      const user = userEvent.setup({ delay: null })
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.getByLabelText(/name/i)).toBeInTheDocument()

      await user.click(screen.getByRole('tab', { name: /income/i }))

      await waitFor(() => {
        expect(screen.getByText(/no income sources configured/i)).toBeInTheDocument()
      })

      await user.click(screen.getByRole('tab', { name: /budget/i }))

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument()
      })
    })

    it('renders IncomeSourceConfig in Income tab', async () => {
      const user = userEvent.setup({ delay: null })
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      await user.click(screen.getByRole('tab', { name: /income/i }))

      await waitFor(() => {
        expect(screen.getByText(/no income sources configured/i)).toBeInTheDocument()
      })

      expect(screen.getByLabelText(/source type/i)).toBeInTheDocument()
    })

    it('renders track actual income checkbox', async () => {
      const user = userEvent.setup({ delay: null })
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      await user.click(screen.getByRole('tab', { name: /income/i }))

      const checkbox = screen.getByLabelText(/track actual income/i)
      await waitFor(() => {
        expect(checkbox).toBeInTheDocument()
      })
      expect(checkbox).not.toBeChecked()
    })

    it('includes income_sources in submission payload when sources are added', async () => {
      const user = userEvent.setup({ delay: null })
      renderWithProviders(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      await user.type(screen.getByLabelText(/name/i), 'Test Budget')
      await user.clear(screen.getByLabelText(/amount/i))
      await user.type(screen.getByLabelText(/amount/i), '2000')

      await user.click(screen.getByRole('tab', { name: /income/i }))

      await waitFor(() => {
        expect(screen.getByLabelText(/source type/i)).toBeInTheDocument()
      })

      await user.selectOptions(screen.getByLabelText(/source type/i), 'manual')

      await waitFor(() => {
        expect(document.getElementById('manual-amount')).toBeInTheDocument()
      })

      const incomeAmountInput = document.getElementById('manual-amount') as HTMLInputElement
      await user.type(incomeAmountInput, '3000')
      await user.click(screen.getByRole('button', { name: /add income source/i }))

      await user.click(screen.getByRole('button', { name: /create budget/i }))

      await waitFor(() => {
        expect(mockCreateBudgetMutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            data: expect.objectContaining({
              name: 'Test Budget',
              amount: 2000,
              income_sources: [
                expect.objectContaining({
                  type: 'manual',
                  value: '3000',
                }),
              ],
            }),
          })
        )
      })
    }, 20000)

    it('resets Income tab when dialog reopens', async () => {
      const user = userEvent.setup({ delay: null })
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      })
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )
      const { rerender } = render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />,
        { wrapper: Wrapper }
      )

      await user.click(screen.getByRole('tab', { name: /income/i }))
      await user.selectOptions(screen.getByLabelText(/source type/i), 'manual')
      await user.type(screen.getByLabelText(/amount/i), '1000')
      await user.click(screen.getByRole('button', { name: /add income source/i }))

      await waitFor(() => {
        expect(screen.queryByText(/no income sources configured/i)).not.toBeInTheDocument()
      })

      rerender(
        <CreateBudgetDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      rerender(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      )

      expect(screen.getByLabelText(/name/i)).toBeInTheDocument()

      await user.click(screen.getByRole('tab', { name: /income/i }))

      await waitFor(() => {
        expect(screen.getByText(/no income sources configured/i)).toBeInTheDocument()
      })
    }, 20000)
  })
})
