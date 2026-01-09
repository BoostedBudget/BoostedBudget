/**
 * RolloverActionForm Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the category rollover action configuration form.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { RolloverAction } from '../../types'
import type { Budget } from '../../../budgets/types'

// Mock budgets with categories
const mockBudgets: Budget[] = [
  {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'Monthly Budget',
    period: 'monthly',
    start_date: '2024-01-01',
    amount: 5000,
    currency_code: 'USD',
    is_active: true,
    income_amount: 6000,
    income_sources: [],
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
        spent_amount: 200,
        created_at: '2024-01-01',
        updated_at: '2024-01-01',
        tags: [],
        match_logic: 'or',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation', 'Gas'],
        allocated_amount: 300,
        spent_amount: 150,
        created_at: '2024-01-01',
        updated_at: '2024-01-01',
        tags: [],
        match_logic: 'or',
      },
      {
        id: 'cat-3',
        budget_id: 'budget-1',
        category: ['Shopping', 'Clothing'],
        allocated_amount: 200,
        spent_amount: 100,
        created_at: '2024-01-01',
        updated_at: '2024-01-01',
        tags: [],
        match_logic: 'or',
      },
    ],
    created_at: '2024-01-01',
    updated_at: '2024-01-01',
  },
  {
    id: 'budget-2',
    user_id: 'user-1',
    name: 'Savings Budget',
    period: 'monthly',
    start_date: '2024-01-01',
    amount: 2000,
    currency_code: 'USD',
    is_active: true,
    income_amount: 0,
    income_sources: [],
    categories: [
      {
        id: 'cat-4',
        budget_id: 'budget-2',
        category: ['Savings', 'Emergency Fund'],
        allocated_amount: 1000,
        spent_amount: 0,
        created_at: '2024-01-01',
        updated_at: '2024-01-01',
        tags: [],
        match_logic: 'or',
      },
    ],
    created_at: '2024-01-01',
    updated_at: '2024-01-01',
  },
]

mock.module('../../../budgets/hooks/useBudgets', () => ({
  useBudgets: () => ({
    budgets: mockBudgets,
    loading: false,
    error: null,
  }),
}))

import { RolloverActionForm } from './RolloverActionForm'

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

const defaultAction: RolloverAction = {
  source_budget_category_id: 'cat-1',
  target_budget_category_id: 'cat-2',
  rollover_type: 'full',
}

describe('RolloverActionForm', () => {

  describe('Rendering', () => {
    it('should render source budget selector', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/source budget/i)).toBeInTheDocument()
    })

    it('should render source category selector', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/source category/i)).toBeInTheDocument()
    })

    it('should render target budget selector', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/target budget/i)).toBeInTheDocument()
    })

    it('should render target category selector', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/target category/i)).toBeInTheDocument()
    })

    it('should render rollover type selector', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/rollover type/i)).toBeInTheDocument()
    })

    it('should show all rollover type options', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/rollover type/i)
      expect(select).toContainHTML('Full')
      expect(select).toContainHTML('Capped')
      expect(select).toContainHTML('Percentage')
    })
  })

  describe('Source Selection', () => {
    it('should show budgets in source budget dropdown', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/source budget/i)
      expect(select).toContainHTML('Monthly Budget')
      expect(select).toContainHTML('Savings Budget')
    })

    it('should show categories from selected source budget', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/source category/i)
      expect(select).toContainHTML('Food and Drink')
      expect(select).toContainHTML('Transportation')
    })

    it('should update source category when source budget changes', () => {
      const onChange = mock(() => {})

      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/source budget/i), {
        target: { value: 'budget-2' },
      })

      // Should clear source category when budget changes
      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          source_budget_category_id: '',
        })
      )
    })

    it('should call onChange when source category is changed', () => {
      const onChange = mock(() => {})

      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/source category/i), {
        target: { value: 'cat-2' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          source_budget_category_id: 'cat-2',
        })
      )
    })
  })

  describe('Target Selection', () => {
    it('should show "Unallocated Funds" option in target category', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/target category/i)
      expect(select).toContainHTML('Unallocated Funds')
    })

    it('should allow cross-budget rollover', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      // Target budget should show all budgets
      const select = screen.getByLabelText(/target budget/i)
      expect(select).toContainHTML('Monthly Budget')
      expect(select).toContainHTML('Savings Budget')
    })

    it('should show categories from selected target budget', () => {
      const actionWithDifferentTarget: RolloverAction = {
        ...defaultAction,
        target_budget_category_id: 'cat-4',
      }

      renderWithProviders(
        <RolloverActionForm action={actionWithDifferentTarget} onChange={() => {}} />
      )

      // When target is in budget-2, should show budget-2 categories
      const select = screen.getByLabelText(/target category/i)
      expect(select).toContainHTML('Savings')
    })

    it('should call onChange when target is set to Unallocated Funds', () => {
      const onChange = mock(() => {})

      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/target category/i), {
        target: { value: '' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          target_budget_category_id: null,
        })
      )
    })
  })

  describe('Rollover Type: Full', () => {
    it('should select full by default', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/rollover type/i)).toHaveValue('full')
    })

    it('should not show cap amount input for full type', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.queryByLabelText(/cap amount/i)).not.toBeInTheDocument()
    })

    it('should not show percentage input for full type', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.queryByLabelText(/percentage/i)).not.toBeInTheDocument()
    })
  })

  describe('Rollover Type: Capped', () => {
    const cappedAction: RolloverAction = {
      ...defaultAction,
      rollover_type: 'capped',
      cap_amount: 100,
    }

    it('should show cap amount input when capped type is selected', () => {
      renderWithProviders(
        <RolloverActionForm action={cappedAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/cap amount/i)).toBeInTheDocument()
    })

    it('should display cap amount value', () => {
      renderWithProviders(
        <RolloverActionForm action={cappedAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/cap amount/i)).toHaveValue(100)
    })

    it('should call onChange when cap amount is changed', () => {
      const onChange = mock(() => {})

      renderWithProviders(
        <RolloverActionForm action={cappedAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/cap amount/i), {
        target: { value: '250' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          cap_amount: 250,
        })
      )
    })

    it('should not show percentage input for capped type', () => {
      renderWithProviders(
        <RolloverActionForm action={cappedAction} onChange={() => {}} />
      )

      expect(screen.queryByLabelText(/percentage/i)).not.toBeInTheDocument()
    })
  })

  describe('Rollover Type: Percentage', () => {
    const percentageAction: RolloverAction = {
      ...defaultAction,
      rollover_type: 'percentage',
      percentage: 50,
    }

    it('should show percentage input when percentage type is selected', () => {
      renderWithProviders(
        <RolloverActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/percentage/i)).toBeInTheDocument()
    })

    it('should display percentage value', () => {
      renderWithProviders(
        <RolloverActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/percentage/i)).toHaveValue(50)
    })

    it('should call onChange when percentage is changed', () => {
      const onChange = mock(() => {})

      renderWithProviders(
        <RolloverActionForm action={percentageAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/percentage/i), {
        target: { value: '75' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          percentage: 75,
        })
      )
    })

    it('should not show cap amount input for percentage type', () => {
      renderWithProviders(
        <RolloverActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.queryByLabelText(/cap amount/i)).not.toBeInTheDocument()
    })

    it('should validate percentage between 0 and 100', () => {
      renderWithProviders(
        <RolloverActionForm action={percentageAction} onChange={() => {}} />
      )

      const input = screen.getByLabelText(/percentage/i)
      expect(input).toHaveAttribute('min', '0')
      expect(input).toHaveAttribute('max', '100')
    })
  })

  describe('Rollover Type Switching', () => {
    it('should call onChange when rollover type is changed to capped', () => {
      const onChange = mock(() => {})

      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/rollover type/i), {
        target: { value: 'capped' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          rollover_type: 'capped',
        })
      )
    })

    it('should call onChange when rollover type is changed to percentage', () => {
      const onChange = mock(() => {})

      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/rollover type/i), {
        target: { value: 'percentage' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          rollover_type: 'percentage',
        })
      )
    })
  })

  describe('Disabled State', () => {
    it('should disable all controls when disabled prop is true', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} disabled />
      )

      expect(screen.getByLabelText(/source budget/i)).toBeDisabled()
      expect(screen.getByLabelText(/source category/i)).toBeDisabled()
      expect(screen.getByLabelText(/target budget/i)).toBeDisabled()
      expect(screen.getByLabelText(/target category/i)).toBeDisabled()
      expect(screen.getByLabelText(/rollover type/i)).toBeDisabled()
    })
  })

  describe('Description', () => {
    it('should show description for full rollover type', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByText(/transfer all remaining funds/i)).toBeInTheDocument()
    })

    it('should show description for capped rollover type', () => {
      const cappedAction: RolloverAction = {
        ...defaultAction,
        rollover_type: 'capped',
        cap_amount: 100,
      }

      renderWithProviders(
        <RolloverActionForm action={cappedAction} onChange={() => {}} />
      )

      expect(screen.getByText(/transfer up to a maximum amount/i)).toBeInTheDocument()
    })

    it('should show description for percentage rollover type', () => {
      const percentageAction: RolloverAction = {
        ...defaultAction,
        rollover_type: 'percentage',
        percentage: 50,
      }

      renderWithProviders(
        <RolloverActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByText(/transfer a percentage of remaining funds/i)).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible labels for all controls', () => {
      renderWithProviders(
        <RolloverActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/source budget/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/source category/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/target budget/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/target category/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/rollover type/i)).toBeInTheDocument()
    })
  })
})
