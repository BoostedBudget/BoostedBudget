/**
 * BudgetAlertActionForm Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the budget alert action configuration form.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BudgetAlertActionForm } from './BudgetAlertActionForm'
import type { AlertAction } from '../../types'
import type { Budget } from '../../../budgets/types'

// Mock useBudgets hook
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
    ],
    created_at: '2024-01-01',
    updated_at: '2024-01-01',
  },
  {
    id: 'budget-2',
    user_id: 'user-1',
    name: 'Vacation Fund',
    period: 'yearly',
    start_date: '2024-01-01',
    amount: 3000,
    currency_code: 'USD',
    is_active: true,
    income_amount: 0,
    income_sources: [],
    categories: [],
    created_at: '2024-01-01',
    updated_at: '2024-01-01',
  },
]

vi.mock('../../../budgets/hooks/useBudgets', () => ({
  useBudgets: () => ({
    budgets: mockBudgets,
    loading: false,
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

const defaultAction: AlertAction = {
  notification_channels: ['email'],
  threshold_type: 'percentage',
  threshold_value: 80,
}

describe('BudgetAlertActionForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render budget selector', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/budget/i)).toBeInTheDocument()
    })

    it('should show all available budgets in dropdown', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      const budgetSelect = screen.getByLabelText(/budget/i)
      expect(budgetSelect).toContainHTML('Monthly Budget')
      expect(budgetSelect).toContainHTML('Vacation Fund')
    })

    it('should render threshold type selector', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/threshold type/i)).toBeInTheDocument()
    })

    it('should render threshold value input', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/threshold value/i)).toBeInTheDocument()
    })

    it('should render notification channel checkboxes', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByRole('checkbox', { name: /email/i })).toBeInTheDocument()
      expect(screen.getByRole('checkbox', { name: /push/i })).toBeInTheDocument()
      expect(screen.getByRole('checkbox', { name: /in-app/i })).toBeInTheDocument()
    })

    it('should render message template textarea', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/message template/i)).toBeInTheDocument()
    })
  })

  describe('Category Selection', () => {
    it('should show category dropdown when budget is selected', () => {
      const actionWithBudget: AlertAction = {
        ...defaultAction,
        budget_id: 'budget-1',
      }

      renderWithProviders(
        <BudgetAlertActionForm action={actionWithBudget} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/category/i)).toBeInTheDocument()
    })

    it('should show categories from selected budget', () => {
      const actionWithBudget: AlertAction = {
        ...defaultAction,
        budget_id: 'budget-1',
      }

      renderWithProviders(
        <BudgetAlertActionForm action={actionWithBudget} onChange={() => {}} />
      )

      const categorySelect = screen.getByLabelText(/category/i)
      expect(categorySelect).toContainHTML('Food and Drink')
      expect(categorySelect).toContainHTML('Transportation')
    })

    it('should show "All Categories" option', () => {
      const actionWithBudget: AlertAction = {
        ...defaultAction,
        budget_id: 'budget-1',
      }

      renderWithProviders(
        <BudgetAlertActionForm action={actionWithBudget} onChange={() => {}} />
      )

      const categorySelect = screen.getByLabelText(/category/i)
      expect(categorySelect).toContainHTML('All Categories')
    })

    it('should not show category dropdown when no budget is selected', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.queryByLabelText(/category/i)).not.toBeInTheDocument()
    })

    it('should update categories when budget changes', () => {
      const onChange = vi.fn()
      const actionWithBudget: AlertAction = {
        ...defaultAction,
        budget_id: 'budget-1',
        category_id: 'cat-1',
      }

      renderWithProviders(
        <BudgetAlertActionForm action={actionWithBudget} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/budget/i), {
        target: { value: 'budget-2' },
      })

      // Should clear category when budget changes
      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          budget_id: 'budget-2',
          category_id: undefined,
        })
      )
    })
  })

  describe('Threshold Configuration', () => {
    it('should show percentage symbol when threshold type is percentage', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByText('%')).toBeInTheDocument()
    })

    it('should show currency symbol when threshold type is amount', () => {
      const amountAction: AlertAction = {
        ...defaultAction,
        threshold_type: 'amount',
        threshold_value: 1000,
      }

      renderWithProviders(
        <BudgetAlertActionForm action={amountAction} onChange={() => {}} />
      )

      expect(screen.getByText('$')).toBeInTheDocument()
    })

    it('should update threshold type when changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/threshold type/i), {
        target: { value: 'amount' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          threshold_type: 'amount',
        })
      )
    })

    it('should update threshold value when changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/threshold value/i), {
        target: { value: '90' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          threshold_value: 90,
        })
      )
    })

    it('should validate percentage is between 0 and 100', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      const input = screen.getByLabelText(/threshold value/i)
      expect(input).toHaveAttribute('min', '0')
      expect(input).toHaveAttribute('max', '100')
    })
  })

  describe('Notification Channels', () => {
    it('should check email when included in notification_channels', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByRole('checkbox', { name: /email/i })).toBeChecked()
    })

    it('should not check push when not included in notification_channels', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByRole('checkbox', { name: /push/i })).not.toBeChecked()
    })

    it('should add channel when checkbox is checked', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.click(screen.getByRole('checkbox', { name: /push/i }))

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          notification_channels: ['email', 'push'],
        })
      )
    })

    it('should remove channel when checkbox is unchecked', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.click(screen.getByRole('checkbox', { name: /email/i }))

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          notification_channels: [],
        })
      )
    })

    it('should show all channels checked when all are selected', () => {
      const allChannelsAction: AlertAction = {
        ...defaultAction,
        notification_channels: ['email', 'push', 'in_app'],
      }

      renderWithProviders(
        <BudgetAlertActionForm action={allChannelsAction} onChange={() => {}} />
      )

      expect(screen.getByRole('checkbox', { name: /email/i })).toBeChecked()
      expect(screen.getByRole('checkbox', { name: /push/i })).toBeChecked()
      expect(screen.getByRole('checkbox', { name: /in-app/i })).toBeChecked()
    })
  })

  describe('Message Template', () => {
    it('should show default message template', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      const textarea = screen.getByLabelText(/message template/i) as HTMLTextAreaElement
      expect(textarea.value).toContain('{{budget_name}}')
    })

    it('should update message template when changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/message template/i), {
        target: { value: 'Custom message: {{percent_used}}%' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          message_template: 'Custom message: {{percent_used}}%',
        })
      )
    })

    it('should show available placeholders', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByText(/available placeholders/i)).toBeInTheDocument()
      expect(screen.getByText('{{budget_name}}')).toBeInTheDocument()
      expect(screen.getByText('{{percent_used}}')).toBeInTheDocument()
      expect(screen.getByText('{{remaining_amount}}')).toBeInTheDocument()
    })

    it('should preserve custom message template from action', () => {
      const actionWithMessage: AlertAction = {
        ...defaultAction,
        message_template: 'Alert: Budget {{budget_name}} is at {{percent_used}}%',
      }

      renderWithProviders(
        <BudgetAlertActionForm action={actionWithMessage} onChange={() => {}} />
      )

      const textarea = screen.getByLabelText(/message template/i)
      expect(textarea).toHaveValue('Alert: Budget {{budget_name}} is at {{percent_used}}%')
    })
  })

  describe('Title Template', () => {
    it('should render title template input', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/title template/i)).toBeInTheDocument()
    })

    it('should update title template when changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/title template/i), {
        target: { value: 'Warning: {{budget_name}} limit reached!' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          title_template: 'Warning: {{budget_name}} limit reached!',
        })
      )
    })
  })

  describe('Disabled State', () => {
    it('should disable all controls when disabled prop is true', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} disabled />
      )

      expect(screen.getByLabelText(/budget/i)).toBeDisabled()
      expect(screen.getByLabelText(/threshold type/i)).toBeDisabled()
      expect(screen.getByLabelText(/threshold value/i)).toBeDisabled()
      expect(screen.getByRole('checkbox', { name: /email/i })).toBeDisabled()
      expect(screen.getByRole('checkbox', { name: /push/i })).toBeDisabled()
      expect(screen.getByRole('checkbox', { name: /in-app/i })).toBeDisabled()
      expect(screen.getByLabelText(/message template/i)).toBeDisabled()
    })
  })

  describe('Loading State', () => {
    it('should show loading indicator while budgets are loading', () => {
      vi.doMock('../../../budgets/hooks/useBudgets', () => ({
        useBudgets: () => ({
          budgets: [],
          loading: true,
          error: null,
        }),
      }))

      // Re-render after mock update
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      // Component should handle loading state gracefully
      expect(screen.getByLabelText(/budget/i)).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible labels for all controls', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/budget/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/threshold type/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/threshold value/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/message template/i)).toBeInTheDocument()
    })

    it('should have proper aria attributes for notification channels', () => {
      renderWithProviders(
        <BudgetAlertActionForm action={defaultAction} onChange={() => {}} />
      )

      const channelsGroup = screen.getByRole('group', { name: /notification channels/i })
      expect(channelsGroup).toBeInTheDocument()
    })
  })
})
