import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import type { ReactNode } from 'react'
import type { Budget } from '../../budgets/types'
import {
  mockUseGetCurrentBudget,
  mockRefetchCurrentBudget,
} from '@/test/mocks/budgets-api'
import { BudgetOverviewWidget } from './BudgetOverviewWidget'

// Create QueryClient + Router wrapper for tests
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      </MemoryRouter>
    )
  }
}

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

  const defaultProps = {
    widgetId: 'widget-1',
    onRemove: mock(() => {}),
  }

  // Helper to configure API mock state
  function setMockState(options: {
    budget?: Budget | null
    loading?: boolean
    error?: Error | null
  }) {
    const { budget = null, loading = false, error = null } = options
    mockUseGetCurrentBudget.mockReturnValue({
      data: budget,
      isLoading: loading,
      error,
      refetch: mockRefetchCurrentBudget,
    })
  }

  beforeEach(() => {
    mockUseGetCurrentBudget.mockClear()
    mockRefetchCurrentBudget.mockClear()
  })

  describe('Loading State', () => {
    it('should show loading spinner initially', () => {
      setMockState({ loading: true })

      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })

      const loading = document.querySelector('.animate-spin')
      expect(loading).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      setMockState({ budget: mockBudget })
    })

    it('should render budget name', () => {
      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })
      expect(screen.getByText('Monthly Budget')).toBeInTheDocument()
    })

    it('should render budget amount', () => {
      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })
      expect(screen.getByText('$2,000.00')).toBeInTheDocument()
    })

    it('should render spent amount', () => {
      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })
      // Spent is 250 + 100 = 350
      expect(screen.getByText('$350.00')).toBeInTheDocument()
    })

    it('should render progress bar', () => {
      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })
      expect(screen.getByRole('progressbar')).toBeInTheDocument()
    })

    it('should show percentage used', () => {
      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })
      // 350/2000 = 17.5% rounded to 18%
      expect(screen.getByText('18%')).toBeInTheDocument()
    })
  })

  describe('No Budget State', () => {
    beforeEach(() => {
      // 404 error indicates no budget
      setMockState({ error: { response: { status: 404 } } as unknown as Error })
    })

    it('should show "no budget" message', () => {
      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })
      expect(screen.getByText(/no active budget/i)).toBeInTheDocument()
    })

    it('should show create budget prompt with correct link', () => {
      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })
      const createBtn = screen.getByRole('link', { name: /create budget/i })
      expect(createBtn).toBeInTheDocument()
      expect(createBtn).toHaveAttribute('href', '/budgets')
    })
  })

  describe('Error State', () => {
    it('should show error message on failure', () => {
      setMockState({ error: new Error('Network error') })

      render(<BudgetOverviewWidget {...defaultProps} />, { wrapper: createWrapper() })

      expect(screen.getByText(/error/i)).toBeInTheDocument()
    })
  })
})
