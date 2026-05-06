import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { mockNavigate } from '@/test/mocks/react-router'
import { STORAGE_KEY_LIST_VIEW } from '../constants'
import type { Budget } from '../types'

import {
  mockUseBudgets,
  mockRefetchBudgets,
  mockCreateBudget,
  mockUpdateBudget,
  mockDeleteBudget,
  mockGetAvailableCategories,
  mockAddCategory,
  mockUpdateCategory,
  mockDeleteCategory,
} from '@/test/mocks/budgets-wrapper'
mock.module('./BudgetCard', () => ({
  BudgetCard: ({
    budget,
    onDelete,
  }: {
    budget: Budget
    onDelete?: (budgetId: string) => void
  }) => (
    <div data-testid={`budget-card-${budget.id}`}>
      {budget.name}
      {onDelete && (
        <button data-testid={`delete-budget-${budget.id}`} onClick={() => onDelete(budget.id)}>
          Delete Budget
        </button>
      )}
    </div>
  ),
}))

import { mockUseBudgetFilters } from '@/test/mocks/budgets-wrapper'

mock.module('@/features/budgets/hooks/useBudgets', () => ({
  useBudgets: () => mockUseBudgets(),
}))

mock.module('../hooks/useBudgets', () => ({
  useBudgets: () => mockUseBudgets(),
}))

mock.module('@/features/budgets/hooks/useBudgetFilters', () => ({
  useBudgetFilters: () => mockUseBudgetFilters(),
}))

mock.module('../hooks/useBudgetFilters', () => ({
  useBudgetFilters: () => mockUseBudgetFilters(),
}))

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BudgetsPage } from './BudgetsPage'

describe('BudgetsPage', () => {
  const mockActiveBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'Monthly Budget',
    period: 'monthly',
    start_date: '2025-01-01',
    end_date: '2025-01-31',
    amount: 2000,
    currency_code: 'USD',
    is_active: true,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    income_amount: 0,
    income_sources: [],
  }

  const mockInactiveBudget: Budget = {
    id: 'budget-2',
    user_id: 'user-1',
    name: 'Past Budget',
    period: 'monthly',
    start_date: '2024-12-01',
    end_date: '2024-12-31',
    amount: 1500,
    currency_code: 'USD',
    is_active: false,
    created_at: '2024-12-01T00:00:00Z',
    updated_at: '2024-12-01T00:00:00Z',
    income_amount: 0,
    income_sources: [],
  }

  function resetMocks() {
    mockRefetchBudgets.mockClear()
    mockCreateBudget.mockClear()
    mockUpdateBudget.mockClear()
    mockDeleteBudget.mockClear()
    mockGetAvailableCategories.mockClear()
    mockAddCategory.mockClear()
    mockUpdateCategory.mockClear()
    mockDeleteCategory.mockClear()
    mockUseBudgets.mockClear()

    mockUseBudgets.mockReturnValue({
      budgets: [],
      loading: false,
      error: null,
      refetch: mockRefetchBudgets,
      createBudget: mockCreateBudget,
      updateBudget: mockUpdateBudget,
      deleteBudget: mockDeleteBudget,
      getAvailableCategories: mockGetAvailableCategories,
      addCategory: mockAddCategory,
      updateCategory: mockUpdateCategory,
      deleteCategory: mockDeleteCategory,
    })
  }

  beforeEach(resetMocks)
  afterEach(resetMocks)

  function setupMocks(overrides?: {
    budgets?: Budget[]
    budgetsLoading?: boolean
    budgetsError?: Error | null
  }) {
    mockUseBudgets.mockReturnValue({
      budgets: overrides?.budgets ?? [],
      loading: overrides?.budgetsLoading ?? false,
      error: overrides?.budgetsError ?? null,
      refetch: mockRefetchBudgets,
      createBudget: mockCreateBudget,
      updateBudget: mockUpdateBudget,
      deleteBudget: mockDeleteBudget,
      getAvailableCategories: mockGetAvailableCategories,
      addCategory: mockAddCategory,
      updateCategory: mockUpdateCategory,
      deleteCategory: mockDeleteCategory,
    })
  }

  describe('Loading & Error States', () => {
    it('shows loading state while budgets are fetching', () => {
      setupMocks({ budgetsLoading: true })
      render(<BudgetsPage />)
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('shows error state when budgets fetch fails', () => {
      setupMocks({ budgetsError: new Error('Failed to fetch budgets') })
      render(<BudgetsPage />)
      expect(screen.getByText(/error loading budgets/i)).toBeInTheDocument()
      expect(screen.getByText(/failed to fetch budgets/i)).toBeInTheDocument()
    })
  })

  describe('Empty States', () => {
    it('shows empty state for budgets when none exist', () => {
      setupMocks({ budgets: [] })
      render(<BudgetsPage />)
      expect(screen.getByText(/no active budgets/i)).toBeInTheDocument()
    })

    it('renders create budget button in empty state', () => {
      setupMocks({ budgets: [] })
      render(<BudgetsPage />)
      expect(screen.getByText(/create your first budget/i)).toBeInTheDocument()
    })
  })

  describe('Savings Section Removed', () => {
    it('does not render savings goals section', () => {
      setupMocks({ budgets: [] })
      render(<BudgetsPage />)
      expect(screen.queryByText(/savings goals/i)).toBeNull()
    })
  })

  describe('Data Display', () => {
    it('renders page header', () => {
      setupMocks()
      render(<BudgetsPage />)
      expect(screen.getByRole('heading', { level: 1, name: /budgets/i })).toBeInTheDocument()
    })

    it('renders active budgets in grid', () => {
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      expect(screen.getByTestId('budget-card-budget-1')).toBeInTheDocument()
      expect(screen.getByText('Monthly Budget')).toBeInTheDocument()
    })

    it('renders inactive/past budgets in separate section', () => {
      setupMocks({ budgets: [mockActiveBudget, mockInactiveBudget] })
      render(<BudgetsPage />)
      expect(screen.getByText(/past budgets/i)).toBeInTheDocument()
      expect(screen.getByTestId('budget-card-budget-2')).toBeInTheDocument()
    })

    it('does not render past budgets section when none exist', () => {
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      expect(screen.queryByText(/past budgets/i)).not.toBeInTheDocument()
    })
  })

  describe('Create Budget Dialog', () => {
    it('opens create budget dialog when header button clicked', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create budget/i })
      await user.click(createButton)
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /create budget/i })).toBeInTheDocument()
    })

    it('opens create budget dialog when empty state button clicked', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [] })
      render(<BudgetsPage />)
      const emptyStateButton = screen.getByText(/create your first budget/i)
      await user.click(emptyStateButton)
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('closes create budget dialog on cancel', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create budget/i })
      await user.click(createButton)
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      const cancelButton = screen.getByRole('button', { name: /^cancel$/i })
      await user.click(cancelButton)
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    it.skip('closes dialog and refetches on budget created', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create budget/i })
      await user.click(createButton)
      expect(mockRefetchBudgets).toHaveBeenCalled()
    })
  })

  describe('Header Buttons', () => {
    it('renders create budget button in section header when budgets exist', () => {
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create budget/i })
      expect(createButton).toBeInTheDocument()
    })
  })

  describe('Conditional Header Button Visibility (Issue #460)', () => {
    it('hides Create Budget header button when no active budgets exist', () => {
      setupMocks({ budgets: [], })
      render(<BudgetsPage />)
      expect(screen.getByText(/create your first budget/i)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /^create budget$/i })).not.toBeInTheDocument()
    })

    it('shows Create Budget header button when active budgets exist', () => {
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      expect(screen.getByRole('button', { name: /create budget/i })).toBeInTheDocument()
      expect(screen.queryByText(/create your first budget/i)).not.toBeInTheDocument()
    })
  })

  describe('Delete Budget (Issue #521, #960)', () => {
    it('opens delete budget dialog when delete button clicked', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      await user.click(screen.getByTestId('delete-budget-budget-1'))
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /delete budget/i })).toBeInTheDocument()
    })

    it('closes delete budget dialog on cancel', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      await user.click(screen.getByTestId('delete-budget-budget-1'))
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: /^cancel$/i }))
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })

    it('calls deleteBudget when confirm clicked after typing budget name', async () => {
      const user = userEvent.setup()
      const localMockDeleteBudget = mock(() => Promise.resolve())
      mockUseBudgets.mockReturnValue({
        budgets: [mockActiveBudget],
        loading: false,
        error: null,
        refetch: mockRefetchBudgets,
        createBudget: mockCreateBudget,
        updateBudget: mockUpdateBudget,
        deleteBudget: localMockDeleteBudget,
        getAvailableCategories: mockGetAvailableCategories,
        addCategory: mockAddCategory,
        updateCategory: mockUpdateCategory,
        deleteCategory: mockDeleteCategory,
      })
      render(<BudgetsPage />)
      await user.click(screen.getByTestId('delete-budget-budget-1'))
      await user.type(screen.getByRole('textbox'), 'Monthly Budget')
      await user.click(screen.getByRole('button', { name: /^delete$/i }))
      expect(localMockDeleteBudget).toHaveBeenCalledWith('budget-1')
    })

    it('can delete past/inactive budget', async () => {
      const user = userEvent.setup()
      const localMockDeleteBudget = mock(() => Promise.resolve())
      mockUseBudgets.mockReturnValue({
        budgets: [mockActiveBudget, mockInactiveBudget],
        loading: false,
        error: null,
        refetch: mockRefetchBudgets,
        createBudget: mockCreateBudget,
        updateBudget: mockUpdateBudget,
        deleteBudget: localMockDeleteBudget,
        getAvailableCategories: mockGetAvailableCategories,
        addCategory: mockAddCategory,
        updateCategory: mockUpdateCategory,
        deleteCategory: mockDeleteCategory,
      })
      render(<BudgetsPage />)
      await user.click(screen.getByTestId('delete-budget-budget-2'))
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      await user.type(screen.getByRole('textbox'), 'Past Budget')
      await user.click(screen.getByRole('button', { name: /^delete$/i }))
      expect(localMockDeleteBudget).toHaveBeenCalledWith('budget-2')
    })
  })

  describe('Table Navigation (Issue #1051)', () => {
    beforeEach(() => {
      mockNavigate.mockClear()
      localStorage.setItem(STORAGE_KEY_LIST_VIEW, 'table')
    })

    afterEach(() => {
      localStorage.removeItem(STORAGE_KEY_LIST_VIEW)
    })

    it('clicking the Manage budget button navigates to /budgets/<id>', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)

      const manageButton = screen.getByRole('button', { name: /manage budget/i })
      await user.click(manageButton)

      expect(mockNavigate).toHaveBeenCalledWith('/budgets/budget-1')
    })

    it('clicking a budget row navigates to /budgets/<id>', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)

      const budgetName = screen.getByText('Monthly Budget')
      await user.click(budgetName)

      expect(mockNavigate).toHaveBeenCalledWith('/budgets/budget-1')
    })

    it('inactive budget table also navigates to /budgets/<id>', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget, mockInactiveBudget] })
      render(<BudgetsPage />)

      const manageButtons = screen.getAllByRole('button', { name: /manage budget/i })
      await user.click(manageButtons[1])

      expect(mockNavigate).toHaveBeenCalledWith('/budgets/budget-2')
    })

    it('clicking a row in the inactive budget table navigates to /budgets/<id>', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget, mockInactiveBudget] })
      render(<BudgetsPage />)

      const inactiveBudgetName = screen.getByText('Past Budget')
      await user.click(inactiveBudgetName)

      expect(mockNavigate).toHaveBeenCalledWith('/budgets/budget-2')
    })
  })
})
