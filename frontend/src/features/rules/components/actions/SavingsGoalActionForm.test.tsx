/**
 * SavingsGoalActionForm Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the savings goal action configuration form.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SavingsGoalActionForm } from './SavingsGoalActionForm'
import type { SavingsGoalAction } from '../../types'

// Mock savings goals
const mockSavingsGoals = [
  {
    id: 'goal-1',
    user_id: 'user-1',
    name: 'Emergency Fund',
    target_amount: 10000,
    current_amount: 2500,
    currency_code: 'USD',
    account_id: 'acc-2',
    is_completed: false,
    created_at: '2024-01-01',
    updated_at: '2024-01-01',
  },
  {
    id: 'goal-2',
    user_id: 'user-1',
    name: 'Vacation Fund',
    target_amount: 5000,
    current_amount: 1000,
    currency_code: 'USD',
    account_id: 'acc-2',
    is_completed: false,
    created_at: '2024-01-01',
    updated_at: '2024-01-01',
  },
]

// Mock accounts
const mockAccounts = [
  {
    id: 'acc-1',
    name: 'Primary Checking',
    type: 'depository',
    subtype: 'checking',
  },
  {
    id: 'acc-2',
    name: 'Savings Account',
    type: 'depository',
    subtype: 'savings',
  },
]

vi.mock('@/api/generated/endpoints/accounts/accounts', () => ({
  useGetAccounts: () => ({
    data: { accounts: mockAccounts },
    isLoading: false,
    error: null,
  }),
}))

vi.mock('@/api/generated/endpoints/savings-goals/savings-goals', () => ({
  useGetSavingsGoals: () => ({
    data: mockSavingsGoals,
    isLoading: false,
    error: null,
  }),
}))

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = createQueryClient()
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>)
}

const defaultAction: SavingsGoalAction = {
  savings_goal_id: 'goal-1',
  from_account_id: 'acc-1',
  amount_type: 'fixed',
  amount: 100,
}

describe('SavingsGoalActionForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render savings goal selector', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/savings goal/i)).toBeInTheDocument()
    })

    it('should render from account selector', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/from account/i)).toBeInTheDocument()
    })

    it('should render amount type selector', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/amount type/i)).toBeInTheDocument()
    })

    it('should show all amount type options', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/amount type/i)
      expect(select).toContainHTML('Fixed')
      expect(select).toContainHTML('Percentage')
      expect(select).toContainHTML('Remaining')
    })

    it('should show all savings goals in dropdown', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/savings goal/i)
      expect(select).toContainHTML('Emergency Fund')
      expect(select).toContainHTML('Vacation Fund')
    })

    it('should show all accounts in from dropdown', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/from account/i)
      expect(select).toContainHTML('Primary Checking')
      expect(select).toContainHTML('Savings Account')
    })
  })

  describe('Goal Selection', () => {
    it('should call onChange when savings goal is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/savings goal/i), {
        target: { value: 'goal-2' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          savings_goal_id: 'goal-2',
        })
      )
    })

    it('should display selected goal', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/savings goal/i)).toHaveValue('goal-1')
    })

    it('should show goal progress when goal is selected', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      // Emergency Fund: $2,500 of $10,000 = 25%
      expect(screen.getByText(/\$2,500.*\$10,000/i)).toBeInTheDocument()
    })
  })

  describe('Account Selection', () => {
    it('should call onChange when from account is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/from account/i), {
        target: { value: 'acc-2' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          from_account_id: 'acc-2',
        })
      )
    })

    it('should display selected from account', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/from account/i)).toHaveValue('acc-1')
    })
  })

  describe('Amount Type: Fixed', () => {
    it('should show amount input when fixed type is selected', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/^amount$/i)).toBeInTheDocument()
    })

    it('should display amount value', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/^amount$/i)).toHaveValue(100)
    })

    it('should call onChange when amount is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/^amount$/i), {
        target: { value: '250' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          amount: 250,
        })
      )
    })

    it('should show dollar sign for fixed amount', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByText('$')).toBeInTheDocument()
    })
  })

  describe('Amount Type: Percentage', () => {
    const percentageAction: SavingsGoalAction = {
      ...defaultAction,
      amount_type: 'percentage',
      percentage: 10,
    }

    it('should show percentage input when percentage type is selected', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/percentage/i)).toBeInTheDocument()
    })

    it('should display percentage value', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/percentage/i)).toHaveValue(10)
    })

    it('should call onChange when percentage is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <SavingsGoalActionForm action={percentageAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/percentage/i), {
        target: { value: '25' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          percentage: 25,
        })
      )
    })

    it('should show percentage symbol', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByText('%')).toBeInTheDocument()
    })

    it('should validate percentage between 0 and 100', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={percentageAction} onChange={() => {}} />
      )

      const input = screen.getByLabelText(/percentage/i)
      expect(input).toHaveAttribute('min', '0')
      expect(input).toHaveAttribute('max', '100')
    })
  })

  describe('Amount Type: Remaining', () => {
    const remainingAction: SavingsGoalAction = {
      ...defaultAction,
      amount_type: 'remaining',
      threshold: 500,
    }

    it('should show threshold input when remaining type is selected', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={remainingAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/minimum balance/i)).toBeInTheDocument()
    })

    it('should display threshold value', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={remainingAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/minimum balance/i)).toHaveValue(500)
    })

    it('should call onChange when threshold is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <SavingsGoalActionForm action={remainingAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/minimum balance/i), {
        target: { value: '1000' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          threshold: 1000,
        })
      )
    })

    it('should show description for remaining type', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={remainingAction} onChange={() => {}} />
      )

      // Multiple elements match, check at least one exists
      const descriptions = screen.getAllByText(/transfer all funds above/i)
      expect(descriptions.length).toBeGreaterThan(0)
    })
  })

  describe('Amount Type Switching', () => {
    it('should call onChange when amount type is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/amount type/i), {
        target: { value: 'percentage' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          amount_type: 'percentage',
        })
      )
    })
  })

  describe('Disabled State', () => {
    it('should disable all controls when disabled prop is true', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} disabled />
      )

      expect(screen.getByLabelText(/savings goal/i)).toBeDisabled()
      expect(screen.getByLabelText(/from account/i)).toBeDisabled()
      expect(screen.getByLabelText(/amount type/i)).toBeDisabled()
      expect(screen.getByLabelText(/^amount$/i)).toBeDisabled()
    })
  })

  describe('Description', () => {
    it('should show description for fixed amount type', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByText(/transfer a specific dollar amount/i)).toBeInTheDocument()
    })

    it('should show description for percentage amount type', () => {
      const percentageAction: SavingsGoalAction = {
        ...defaultAction,
        amount_type: 'percentage',
        percentage: 10,
      }

      renderWithProviders(
        <SavingsGoalActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByText(/transfer a percentage of the balance/i)).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible labels for all controls', () => {
      renderWithProviders(
        <SavingsGoalActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/savings goal/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/from account/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/amount type/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/^amount$/i)).toBeInTheDocument()
    })
  })
})
