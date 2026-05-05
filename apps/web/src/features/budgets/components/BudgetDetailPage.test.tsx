import { describe, it, expect, beforeEach } from 'bun:test'

import { mockNavigate } from '@/test/mocks/react-router'
import {
  mockUseBudgetDetail,
  mockUseAvailableCategories,
  mockBudgetDetailRefetch,
} from '@/test/mocks/budget-detail-hooks'
import { mockUseTags } from '@/test/mocks/tags-hooks'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BudgetDetailPage } from './BudgetDetailPage'

function createMockBudget(overrides: Record<string, unknown> = {}) {
  return {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'January 2025 Budget',
    description: 'Monthly household budget',
    amount: 5000,
    income_amount: 6000,
    period: 'monthly',
    start_date: '2025-01-01',
    end_date: '2025-01-31',
    is_active: true,
    currency_code: 'USD',
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Groceries'],
        allocated_amount: 500,
        spent_amount: 250,
        tags: [],
        match_logic: 'category_only',
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Bills and Utilities', 'Utilities'],
        allocated_amount: 300,
        spent_amount: 280,
        tags: [],
        match_logic: 'category_only',
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ],
    income_sources: [
      { type: 'manual', description: 'Salary', value: '5000' },
      { type: 'manual', description: 'Side gig', value: '500' },
    ],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

function renderWithRouter(budgetId = 'budget-1') {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/budgets/${budgetId}`]}>
        <Routes>
          <Route path="/budgets/:budgetId" element={<BudgetDetailPage />} />
          <Route path="/budgets" element={<div>Budget List</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('BudgetDetailPage', () => {
  beforeEach(() => {
    mockBudgetDetailRefetch.mockClear()
    mockUseBudgetDetail.mockClear()
    mockUseAvailableCategories.mockClear()
    mockUseTags.mockClear()
    mockNavigate.mockClear()
    // Set default return values
    mockUseBudgetDetail.mockReturnValue({
      budget: null,
      loading: true,
      error: null,
      refetch: mockBudgetDetailRefetch,
    })
    mockUseAvailableCategories.mockReturnValue({ categories: [] })
    mockUseTags.mockReturnValue({ tags: [] })
  })

  describe('Loading State', () => {
    it('should render loading spinner when loading', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: true,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Loading budget...')).toBeInTheDocument()
    })

    it('should show loading animation', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: true,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      const { container } = renderWithRouter()

      expect(container.querySelector('.animate-spin')).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should render error message when error occurs', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: new Error('Network error'),
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Error: Network error')).toBeInTheDocument()
    })

    it('should render retry button on error', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: new Error('Network error'),
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Retry')).toBeInTheDocument()
    })

    it('should call refetch when retry button clicked', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: new Error('Network error'),
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      fireEvent.click(screen.getByText('Retry'))

      expect(mockBudgetDetailRefetch).toHaveBeenCalled()
    })
  })

  describe('Not Found State', () => {
    it('should render not found message when budget is null', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Budget not found')).toBeInTheDocument()
    })

    it('should render back link when budget not found', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Back to Budgets')).toBeInTheDocument()
    })
  })

  describe('Success State - Header', () => {
    it('should render budget name', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('January 2025 Budget')).toBeInTheDocument()
    })

    it('should render active badge when budget is active', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ is_active: true }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Active')).toBeInTheDocument()
    })

    it('should not render active badge when budget is inactive', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ is_active: false }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.queryByText('Active')).not.toBeInTheDocument()
    })

    it('should render budget description', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ description: 'Test description' }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Test description')).toBeInTheDocument()
    })

    it('should not render description when empty', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ description: '' }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.queryByText('Monthly household budget')).not.toBeInTheDocument()
    })

    it('should render capitalized period', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ period: 'monthly' }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Monthly')).toBeInTheDocument()
    })

    it('should render formatted date range', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          start_date: '2025-01-15',
          end_date: '2025-01-31',
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Dates')).toBeInTheDocument()
      expect(screen.getByText(/Jan 1\d, 2025/)).toBeInTheDocument()
    })

    it('should render formatted budget amount', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ amount: 5000 }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #1048: $5,000.00 may appear in both the stat card and income section
      expect(screen.getAllByText('$5,000.00').length).toBeGreaterThan(0)
    })

    it('should render income when greater than zero', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ income_amount: 6000 }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real component shows income in header AND BudgetIncomeSection
      expect(screen.getAllByText('$6,000.00').length).toBeGreaterThan(0)
    })
  })

  describe('Income Metrics Calculation', () => {
    it('should calculate expected income from manual sources', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_sources: [
            { type: 'manual', description: 'Salary', value: '4000' },
            { type: 'manual', description: 'Bonus', value: '1000' },
          ],
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real component shows "Expected" label with amount
      expect(screen.getByText('Expected')).toBeInTheDocument()
      // $5,000.00 may appear multiple times (header + income section)
      expect(screen.getAllByText('$5,000.00').length).toBeGreaterThan(0)
    })

    it('should use income_amount as actual income', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ income_amount: 7500 }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real component shows "Actual" label with amount
      expect(screen.getByText('Actual')).toBeInTheDocument()
      // $7,500.00 may appear multiple times (header + income section)
      expect(screen.getAllByText('$7,500.00').length).toBeGreaterThan(0)
    })

    it('uses expected_income as expectedIncome when set', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          expected_income: 3000,
          income_sources: [],
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // expectedIncome = budget.expected_income = 3000, so income section renders
      // BudgetIncomeSection shows "Expected" label with $3,000.00
      expect(screen.getByText('Expected')).toBeInTheDocument()
      expect(screen.getAllByText('$3,000.00').length).toBeGreaterThan(0)
    })

    it('actualIncome uses income_amount without amount fallback', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_amount: 1500,
          expected_income: 3000,
          amount: 9999,
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // actualIncome = budget.income_amount = 1500 (NOT 9999 from amount)
      expect(screen.getByText('Actual')).toBeInTheDocument()
      expect(screen.getAllByText('$1,500.00').length).toBeGreaterThan(0)
      // $9,999.00 should NOT appear as an income value
      // (it may appear in header as "Budget Amount" stat, but not in income section)
    })

    it('should calculate unallocated funds correctly', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_amount: 5000,
          categories: [
            {
              id: 'cat-1',
              budget_id: 'budget-1',
              category: ['Food and Drink', 'Restaurants'],
              allocated_amount: 2000,
              spent_amount: 0,
              tags: [],
              match_logic: 'category_only',
              created_at: '2025-01-01T00:00:00Z',
              updated_at: '2025-01-01T00:00:00Z',
            },
            {
              id: 'cat-2',
              budget_id: 'budget-1',
              category: ['Transportation', 'Gas'],
              allocated_amount: 1500,
              spent_amount: 0,
              tags: [],
              match_logic: 'category_only',
              created_at: '2025-01-01T00:00:00Z',
              updated_at: '2025-01-01T00:00:00Z',
            },
          ],
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real UnallocatedFundsCard shows formatted currency
      // $1,500.00 may appear multiple times
      expect(screen.getAllByText('$1,500.00').length).toBeGreaterThan(0)
    })

    it('should handle empty categories', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_amount: 5000,
          categories: [],
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real UnallocatedFundsCard shows formatted currency
      // With no categories, all income is unallocated - shows $5,000.00
      expect(screen.getByRole('heading', { name: 'Unallocated Funds' })).toBeInTheDocument()
    })
  })

  describe('Issue #1048: Expected Income header reads expected_income, not amount', () => {
    it('stat card shows expected_income value when it differs from amount', () => {
      // expected_income=5000, amount=3000 — header must show $5,000, NOT $3,000
      // With empty income_sources the only place $3,000 could appear is the buggy stat card
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          expected_income: 5000,
          amount: 3000,
          income_sources: [],
          categories: [],
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // $3,000.00 must NOT appear anywhere — it would only surface via the bug
      expect(screen.queryAllByText('$3,000.00').length).toBe(0)
      // $5,000.00 must appear (from the fixed stat card + income section)
      expect(screen.getAllByText('$5,000.00').length).toBeGreaterThan(0)
    })

    it('falls back to amount when expected_income is absent (legacy budget)', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          expected_income: undefined,
          amount: 4500,
          income_sources: [],
          categories: [],
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Expected Income')).toBeInTheDocument()
      expect(screen.getAllByText('$4,500.00').length).toBeGreaterThan(0)
    })
  })

  describe('Issue #1048: UnallocatedFundsCard percentage uses expectedIncome as basis', () => {
    it('uses expectedIncome (not actualIncome) for the percentage denominator', () => {
      // expected_income = 5000, actualIncome (income_amount) = 0, allocated = 4000
      // unallocated = 5000 - 4000 = 1000 → should show 20% of income (1000/5000)
      // Bug: if actualIncome (0) is used as basis → divide-by-zero or 0%
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          expected_income: 5000,
          income_amount: 0,
          amount: 5000,
          income_sources: [],
          categories: [
            {
              id: 'cat-1',
              budget_id: 'budget-1',
              category: ['Food and Drink', 'Groceries'],
              allocated_amount: 4000,
              spent_amount: 0,
              tags: [],
              match_logic: 'category_only',
              created_at: '2025-01-01T00:00:00Z',
              updated_at: '2025-01-01T00:00:00Z',
            },
          ],
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // unallocated = 5000 - 4000 = 1000, basis = expectedIncome = 5000 → 20%
      expect(screen.getByText('(20% of income)')).toBeInTheDocument()
    })
  })

  describe('Sections Visibility', () => {
    it('should show income section when income sources exist', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_sources: [{ type: 'manual', description: 'Salary', value: '5000' }],
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real BudgetIncomeSection has role="region" with heading "Income"
      expect(screen.getByRole('region', { name: /income/i })).toBeInTheDocument()
    })

    it('shows income section when expected_income is positive (even with no income sources)', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_sources: [],
          // amount = 5000 (default), no expected_income → expectedIncome = amount = 5000
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #1048: expectedIncome = budget.amount = 5000 > 0, so section renders
      expect(screen.getByRole('region', { name: /income/i })).toBeInTheDocument()
    })

    it('should render unallocated funds card', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real UnallocatedFundsCard has role="region" with heading "Unallocated Funds"
      expect(screen.getByRole('region', { name: /unallocated funds/i })).toBeInTheDocument()
    })

    it('should render budget category manager', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real BudgetCategoryManager has heading "Budget Categories"
      expect(screen.getByRole('heading', { name: 'Budget Categories' })).toBeInTheDocument()
    })
  })

  describe('Navigation Handlers', () => {
    it('should navigate to savings page with openCreateDialog state', async () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      // Issue #575: Real UnallocatedFundsCard has "Create Savings Goal" button
      fireEvent.click(screen.getByRole('button', { name: /create savings goal/i }))

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith('/savings', { state: { openCreateDialog: true } })
      })
    })

    it('should have back to budgets link in header', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      const backLinks = screen.getAllByText('Back to Budgets')
      expect(backLinks.length).toBeGreaterThan(0)
    })
  })

  describe('Issue #39: Quick-Create Automation Rules', () => {
    it('should render create alert rule button', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByRole('link', { name: /create.*alert.*rule/i })).toBeInTheDocument()
    })

    it('should render create rollover rule button', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByRole('link', { name: /create.*rollover.*rule/i })).toBeInTheDocument()
    })

    it('should have correct href for alert rule button with budget_id', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter('test-budget-123')

      const alertLink = screen.getByRole('link', { name: /create.*alert.*rule/i })
      expect(alertLink).toHaveAttribute('href', '/rules/new?type=budget_alert&budget_id=test-budget-123')
    })

    it('should have correct href for rollover rule button with budget_id', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter('test-budget-456')

      const rolloverLink = screen.getByRole('link', { name: /create.*rollover.*rule/i })
      expect(rolloverLink).toHaveAttribute('href', '/rules/new?type=category_rollover&budget_id=test-budget-456')
    })
  })

  describe('Edge Cases', () => {
    it('should handle null income_sources', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_sources: undefined,
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('January 2025 Budget')).toBeInTheDocument()
    })

    it('should handle null categories', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          categories: undefined,
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('January 2025 Budget')).toBeInTheDocument()
    })

    it('should handle budget without end_date', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          start_date: '2025-01-15',
          end_date: null,
        }),
        loading: false,
        error: null,
        refetch: mockBudgetDetailRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Dates')).toBeInTheDocument()
      expect(screen.getByText(/Jan 1\d, 2025/)).toBeInTheDocument()
    })
  })
})
