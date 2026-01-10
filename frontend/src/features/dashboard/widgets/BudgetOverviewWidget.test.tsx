import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import type { Budget, BudgetWithMetrics } from '../../budgets/types'

const mockRefetch = mock(() => {})
const mockUseCurrentBudget = mock(() => ({
  budget: null,
  metrics: null,
  loading: true,
  error: null,
  hasNoBudget: false,
  refetch: mockRefetch,
}))

mock.module('../../budgets/hooks/useCurrentBudget', () => ({
  useCurrentBudget: mockUseCurrentBudget,
}))

import { BudgetOverviewWidget } from './BudgetOverviewWidget'

describe('BudgetOverviewWidget', () => {
  const mockBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-123',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2025-01-01T00:00:00Z',
    end_date: '2025-01-31T23:59:59Z',
    amount: 2000,
    currency_code: 'USD',
    is_active: true,
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink'],
        allocated_amount: 500,
        spent_amount: 250,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        tags: [],
        match_logic: 'category_only',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation'],
        allocated_amount: 300,
        spent_amount: 100,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        tags: [],
        match_logic: 'category_only',
      },
    ],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    income_amount: 0,
    income_sources: [],
  }

  const mockMetrics: BudgetWithMetrics = {
    ...mockBudget,
    total_allocated: 800,
    total_spent: 350,
    remaining_amount: 1650,
    percent_used: 18,
    is_over_budget: false,
    unallocated_funds: 1200,
    is_fully_allocated: false,
    categories: mockBudget.categories!.map((cat) => ({
      ...cat,
      percent_used: cat.spent_amount / cat.allocated_amount * 100,
      remaining_amount: cat.allocated_amount - cat.spent_amount,
    })),
  }

  const defaultProps = {
    widgetId: 'widget-1',
    onRemove: mock(() => {}),
  }

  const setMockState = (state: Partial<ReturnType<typeof mockUseCurrentBudget>>) => {
    mockUseCurrentBudget.mockReturnValue({
      budget: null,
      metrics: null,
      loading: false,
      error: null,
      hasNoBudget: false,
      refetch: mockRefetch,
      ...state,
    })
  }

  beforeEach(() => {
    mockUseCurrentBudget.mockClear()
    mockRefetch.mockClear()
  })

  describe('Loading State', () => {
    it('should show loading spinner initially', () => {
      setMockState({ loading: true })

      render(<BudgetOverviewWidget {...defaultProps} />)

      const loading = document.querySelector('.animate-spin')
      expect(loading).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      setMockState({ budget: mockBudget, metrics: mockMetrics })
    })

    it('should render budget name', () => {
      render(<BudgetOverviewWidget {...defaultProps} />)
      expect(screen.getByText('Monthly Budget')).toBeInTheDocument()
    })

    it('should render budget amount', () => {
      render(<BudgetOverviewWidget {...defaultProps} />)
      expect(screen.getByText('$2,000.00')).toBeInTheDocument()
    })

    it('should render spent amount', () => {
      render(<BudgetOverviewWidget {...defaultProps} />)
      expect(screen.getByText('$350.00')).toBeInTheDocument()
    })

    it('should render progress bar', () => {
      render(<BudgetOverviewWidget {...defaultProps} />)
      expect(screen.getByRole('progressbar')).toBeInTheDocument()
    })

    it('should show percentage used', () => {
      render(<BudgetOverviewWidget {...defaultProps} />)
      expect(screen.getByText('18%')).toBeInTheDocument()
    })
  })

  describe('No Budget State', () => {
    beforeEach(() => {
      setMockState({ hasNoBudget: true })
    })

    it('should show "no budget" message', () => {
      render(<BudgetOverviewWidget {...defaultProps} />)
      expect(screen.getByText(/no active budget/i)).toBeInTheDocument()
    })

    it('should show create budget prompt with correct link', () => {
      render(<BudgetOverviewWidget {...defaultProps} />)
      const createBtn = screen.getByRole('link', { name: /create budget/i })
      expect(createBtn).toBeInTheDocument()
      expect(createBtn).toHaveAttribute('href', '/budgets')
    })
  })

  describe('Error State', () => {
    it('should show error message on failure', () => {
      setMockState({ error: new Error('Network error') })

      render(<BudgetOverviewWidget {...defaultProps} />)

      expect(screen.getByText(/error/i)).toBeInTheDocument()
    })
  })
})
