/**
 * BudgetDetailPage Component Tests
 * Issue #564: Frontend Test Coverage Improvement
 *
 * Tests the budget detail page component with loading, error, and success states.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BudgetDetailPage } from './BudgetDetailPage'

// Mock hooks
const mockRefetch = vi.fn()
const mockUseBudgetDetail = vi.fn()
const mockUseAvailableCategories = vi.fn()
const mockUseTags = vi.fn()
const mockNavigate = vi.fn()

vi.mock('../hooks/useBudgetDetail', () => ({
  useBudgetDetail: (...args: unknown[]) => mockUseBudgetDetail(...args),
}))

vi.mock('../hooks/useAvailableCategories', () => ({
  useAvailableCategories: () => mockUseAvailableCategories(),
}))

vi.mock('../../tags/hooks/useTags', () => ({
  useTags: () => mockUseTags(),
}))

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

// Mock mutation hooks
const mockAddMutateAsync = vi.fn()
const mockUpdateMutateAsync = vi.fn()
const mockDeleteMutateAsync = vi.fn()

vi.mock('@/api/generated/endpoints/budgets/budgets', () => ({
  useAddBudgetCategory: () => ({ mutateAsync: mockAddMutateAsync }),
  useUpdateBudgetCategory: () => ({ mutateAsync: mockUpdateMutateAsync }),
  useDeleteBudgetCategory: () => ({ mutateAsync: mockDeleteMutateAsync }),
}))

// Mock child components
vi.mock('./category-management/BudgetCategoryManager', () => ({
  BudgetCategoryManager: () => <div data-testid="budget-category-manager">Budget Category Manager</div>,
}))

vi.mock('./budget-detail/BudgetIncomeSection', () => ({
  BudgetIncomeSection: ({ expectedIncome, actualIncome }: { expectedIncome: number; actualIncome: number }) => (
    <div data-testid="budget-income-section">
      <span data-testid="expected-income">{expectedIncome}</span>
      <span data-testid="actual-income">{actualIncome}</span>
    </div>
  ),
}))

vi.mock('./budget-detail/UnallocatedFundsCard', () => ({
  UnallocatedFundsCard: ({ unallocatedAmount, onCreateSavingsGoal }: { unallocatedAmount: number; onCreateSavingsGoal: () => void }) => (
    <div data-testid="unallocated-funds-card">
      <span data-testid="unallocated-amount">{unallocatedAmount}</span>
      <button data-testid="create-savings-goal-btn" onClick={onCreateSavingsGoal}>Create Goal</button>
    </div>
  ),
}))

// Mock budget data
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
      { id: 'cat-1', name: 'Groceries', allocated_amount: 500, spent_amount: 250 },
      { id: 'cat-2', name: 'Utilities', allocated_amount: 300, spent_amount: 280 },
    ],
    income_sources: [
      { type: 'manual', name: 'Salary', value: '5000' },
      { type: 'manual', name: 'Side gig', value: '500' },
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
    vi.clearAllMocks()
    mockUseAvailableCategories.mockReturnValue({ categories: [] })
    mockUseTags.mockReturnValue({ tags: [] })
  })

  describe('Loading State', () => {
    it('should render loading spinner when loading', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: true,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Loading budget...')).toBeInTheDocument()
    })

    it('should show loading animation', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: true,
        error: null,
        refetch: mockRefetch,
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
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Error: Network error')).toBeInTheDocument()
    })

    it('should render retry button on error', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: new Error('Network error'),
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Retry')).toBeInTheDocument()
    })

    it('should call refetch when retry button clicked', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: new Error('Network error'),
        refetch: mockRefetch,
      })

      renderWithRouter()

      fireEvent.click(screen.getByText('Retry'))

      expect(mockRefetch).toHaveBeenCalled()
    })
  })

  describe('Not Found State', () => {
    it('should render not found message when budget is null', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Budget not found')).toBeInTheDocument()
    })

    it('should render back link when budget not found', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
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
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('January 2025 Budget')).toBeInTheDocument()
    })

    it('should render active badge when budget is active', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ is_active: true }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Active')).toBeInTheDocument()
    })

    it('should not render active badge when budget is inactive', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ is_active: false }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.queryByText('Active')).not.toBeInTheDocument()
    })

    it('should render budget description', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ description: 'Test description' }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('Test description')).toBeInTheDocument()
    })

    it('should not render description when empty', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ description: '' }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.queryByText('Monthly household budget')).not.toBeInTheDocument()
    })

    it('should render capitalized period', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ period: 'monthly' }),
        loading: false,
        error: null,
        refetch: mockRefetch,
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
        refetch: mockRefetch,
      })

      renderWithRouter()

      // Check for the Dates label and date pattern (handles timezone variations)
      expect(screen.getByText('Dates')).toBeInTheDocument()
      expect(screen.getByText(/Jan 1\d, 2025/)).toBeInTheDocument()
    })

    it('should render formatted budget amount', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ amount: 5000 }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('$5,000.00')).toBeInTheDocument()
    })

    it('should render income when greater than zero', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ income_amount: 6000 }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByText('$6,000.00')).toBeInTheDocument()
    })
  })

  describe('Income Metrics Calculation', () => {
    it('should calculate expected income from manual sources', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_sources: [
            { type: 'manual', name: 'Salary', value: '4000' },
            { type: 'manual', name: 'Bonus', value: '1000' },
          ],
        }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByTestId('expected-income').textContent).toBe('5000')
    })

    it('should use income_amount as actual income', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({ income_amount: 7500 }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByTestId('actual-income').textContent).toBe('7500')
    })

    it('should calculate unallocated funds correctly', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_amount: 5000,
          categories: [
            { id: 'cat-1', allocated_amount: 2000, spent_amount: 0 },
            { id: 'cat-2', allocated_amount: 1500, spent_amount: 0 },
          ],
        }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      // 5000 - 2000 - 1500 = 1500
      expect(screen.getByTestId('unallocated-amount').textContent).toBe('1500')
    })

    it('should handle empty categories', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_amount: 5000,
          categories: [],
        }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByTestId('unallocated-amount').textContent).toBe('5000')
    })
  })

  describe('Sections Visibility', () => {
    it('should show income section when income sources exist', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_sources: [{ type: 'manual', name: 'Salary', value: '5000' }],
        }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByTestId('budget-income-section')).toBeInTheDocument()
    })

    it('should not show income section when no income sources', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget({
          income_sources: [],
        }),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.queryByTestId('budget-income-section')).not.toBeInTheDocument()
    })

    it('should render unallocated funds card', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByTestId('unallocated-funds-card')).toBeInTheDocument()
    })

    it('should render budget category manager', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByTestId('budget-category-manager')).toBeInTheDocument()
    })
  })

  describe('Navigation Handlers', () => {
    it('should navigate to budgets page with openSavingsGoal state', async () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      fireEvent.click(screen.getByTestId('create-savings-goal-btn'))

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith('/budgets', { state: { openSavingsGoal: true } })
      })
    })

    it('should have back to budgets link in header', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockRefetch,
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
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByRole('link', { name: /create.*alert.*rule/i })).toBeInTheDocument()
    })

    it('should render create rollover rule button', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      renderWithRouter()

      expect(screen.getByRole('link', { name: /create.*rollover.*rule/i })).toBeInTheDocument()
    })

    it('should have correct href for alert rule button with budget_id', () => {
      mockUseBudgetDetail.mockReturnValue({
        budget: createMockBudget(),
        loading: false,
        error: null,
        refetch: mockRefetch,
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
        refetch: mockRefetch,
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
        refetch: mockRefetch,
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
        refetch: mockRefetch,
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
        refetch: mockRefetch,
      })

      renderWithRouter()

      // Should render without crashing - check that Dates label exists
      expect(screen.getByText('Dates')).toBeInTheDocument()
      // Should show a date (handles timezone variations)
      expect(screen.getByText(/Jan 1\d, 2025/)).toBeInTheDocument()
    })
  })
})
