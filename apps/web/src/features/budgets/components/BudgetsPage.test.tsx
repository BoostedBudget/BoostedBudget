import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { mockNavigate } from '@/test/mocks/react-router'
import { STORAGE_KEY_LIST_VIEW } from '../constants'
import type { Budget } from '../types'
import type { SavingsGoal } from '../../../features/savings/types'

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
  mockUseSavingsGoals,
  mockRefetchGoals,
  mockCreateGoal,
  mockUpdateGoal,
  mockDeleteGoal,
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

mock.module('../../../features/savings/components/SavingsGoalCard', () => ({
  SavingsGoalCard: ({
    goal,
    onDelete,
  }: {
    goal: SavingsGoal
    onDelete?: (goalId: string) => void
  }) => (
    <div data-testid={`goal-card-${goal.id}`}>
      {goal.name}
      {onDelete && (
        <button data-testid={`delete-goal-${goal.id}`} onClick={() => onDelete(goal.id)}>
          Delete Goal
        </button>
      )}
    </div>
  ),
}))

// Issue #960: Both DeleteBudgetDialog and DeleteSavingsGoalDialog replaced by
// ConfirmationDialog — using the real component in tests (no mock needed).

// Issue #575: DO NOT mock CreateBudgetDialog - it causes pollution to
// CreateBudgetDialog.test.tsx via Bun's global mock.module() persistence.
// The real component works in happy-dom with RadixUI portals.

// Note: CreateSavingsGoalDialog mock is kept because removing it requires
// QueryClientProvider wrapper, which would need significant test refactoring.
// The mock pollution for CreateSavingsGoalDialog.test.tsx is a lower priority.
mock.module('./CreateSavingsGoalDialog', () => ({
  CreateSavingsGoalDialog: ({
    isOpen,
    onCancel,
    onGoalCreated,
  }: {
    isOpen: boolean
    onCancel: () => void
    onGoalCreated: (goal: SavingsGoal) => void
  }) => {
    if (!isOpen) return null
    return (
      <div data-testid="create-goal-dialog">
        <button onClick={onCancel}>Cancel Goal</button>
        <button
          onClick={() =>
            onGoalCreated({
              id: 'new-goal',
              user_id: 'user-1',
              name: 'New Goal',
              target_amount: 5000,
              current_amount: 0,
              currency_code: 'USD',
              account_id: 'acc-1',
              is_completed: false,
              created_at: '2025-01-01T00:00:00Z',
              updated_at: '2025-01-01T00:00:00Z',
            })
          }
        >
          Submit Goal
        </button>
      </div>
    )
  },
}))

// Mock wrapper hooks (not globally preloaded to allow hook tests to work)
// This must be BEFORE importing BudgetsPage
import { mockUseBudgetFilters } from '@/test/mocks/budgets-wrapper'

mock.module('@/features/budgets/hooks/useBudgets', () => ({
  useBudgets: () => mockUseBudgets(),
}))

mock.module('../hooks/useBudgets', () => ({
  useBudgets: () => mockUseBudgets(),
}))

mock.module('@/features/savings/hooks/useSavingsGoals', () => ({
  useSavingsGoals: () => mockUseSavingsGoals(),
}))

mock.module('../../../features/savings/hooks/useSavingsGoals', () => ({
  useSavingsGoals: () => mockUseSavingsGoals(),
}))

mock.module('@/features/budgets/hooks/useBudgetFilters', () => ({
  useBudgetFilters: () => mockUseBudgetFilters(),
}))

mock.module('../hooks/useBudgetFilters', () => ({
  useBudgetFilters: () => mockUseBudgetFilters(),
}))

// NOW import testing utilities and component AFTER mocks
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

  const mockActiveGoal: SavingsGoal = {
    id: 'goal-1',
    user_id: 'user-1',
    name: 'Vacation Fund',
    target_amount: 5000,
    current_amount: 2000,
    currency_code: 'USD',
    account_id: 'acc-1',
    is_completed: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  }

  const mockCompletedGoal: SavingsGoal = {
    id: 'goal-2',
    user_id: 'user-1',
    name: 'Emergency Fund',
    target_amount: 10000,
    current_amount: 10000,
    currency_code: 'USD',
    account_id: 'acc-1',
    is_completed: true,
    completed_at: '2025-01-15T00:00:00Z',
    created_at: '2024-06-01T00:00:00Z',
    updated_at: '2025-01-15T00:00:00Z',
  }

  function resetMocks() {
    // Clear all centralized mocks
    mockRefetchBudgets.mockClear()
    mockCreateBudget.mockClear()
    mockUpdateBudget.mockClear()
    mockDeleteBudget.mockClear()
    mockGetAvailableCategories.mockClear()
    mockAddCategory.mockClear()
    mockUpdateCategory.mockClear()
    mockDeleteCategory.mockClear()
    mockRefetchGoals.mockClear()
    mockCreateGoal.mockClear()
    mockUpdateGoal.mockClear()
    mockDeleteGoal.mockClear()
    mockUseBudgets.mockClear()
    mockUseSavingsGoals.mockClear()

    // Reset to default state
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
    mockUseSavingsGoals.mockReturnValue({
      goals: [],
      goalsWithProgress: [],
      loading: false,
      error: null,
      totalTarget: 0,
      totalSaved: 0,
      refetch: mockRefetchGoals,
      createGoal: mockCreateGoal,
      updateGoal: mockUpdateGoal,
      deleteGoal: mockDeleteGoal,
    })
  }

  beforeEach(resetMocks)
  afterEach(resetMocks)

  function setupMocks(overrides?: {
    budgets?: Budget[]
    budgetsLoading?: boolean
    budgetsError?: Error | null
    goals?: SavingsGoal[]
    goalsLoading?: boolean
    goalsError?: Error | null
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

    mockUseSavingsGoals.mockReturnValue({
      goals: overrides?.goals ?? [],
      goalsWithProgress: [],
      loading: overrides?.goalsLoading ?? false,
      error: overrides?.goalsError ?? null,
      totalTarget: 0,
      totalSaved: 0,
      refetch: mockRefetchGoals,
      createGoal: mockCreateGoal,
      updateGoal: mockUpdateGoal,
      deleteGoal: mockDeleteGoal,
    })
  }

  describe('Loading & Error States', () => {
    it('shows loading state while budgets are fetching', () => {
      setupMocks({ budgetsLoading: true })
      render(<BudgetsPage />)
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('shows loading state while goals are fetching', () => {
      setupMocks({ goalsLoading: true })
      render(<BudgetsPage />)
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('shows error state when budgets fetch fails', () => {
      setupMocks({ budgetsError: new Error('Failed to fetch budgets') })
      render(<BudgetsPage />)
      expect(screen.getByText(/error loading budgets/i)).toBeInTheDocument()
      expect(screen.getByText(/failed to fetch budgets/i)).toBeInTheDocument()
    })

    it('shows error state when goals fetch fails', () => {
      setupMocks({ goalsError: new Error('Failed to fetch goals') })
      render(<BudgetsPage />)
      expect(screen.getByText(/error loading budgets/i)).toBeInTheDocument()
      expect(screen.getByText(/failed to fetch goals/i)).toBeInTheDocument()
    })
  })

  describe('Empty States', () => {
    it('shows empty state for budgets when none exist', () => {
      setupMocks({ budgets: [], goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      expect(screen.getByText(/no active budgets/i)).toBeInTheDocument()
    })

    it('shows empty state for goals when none exist', () => {
      setupMocks({ budgets: [mockActiveBudget], goals: [] })
      render(<BudgetsPage />)
      expect(screen.getByText(/no savings goals/i)).toBeInTheDocument()
    })

    it('renders create budget button in empty state', () => {
      setupMocks({ budgets: [] })
      render(<BudgetsPage />)
      expect(screen.getByText(/create your first budget/i)).toBeInTheDocument()
    })

    it('renders create goal button in empty state', () => {
      setupMocks({ goals: [] })
      render(<BudgetsPage />)
      expect(screen.getByText(/create your first goal/i)).toBeInTheDocument()
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

    it('renders active savings goals', () => {
      setupMocks({ goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      expect(screen.getByTestId('goal-card-goal-1')).toBeInTheDocument()
      expect(screen.getByText('Vacation Fund')).toBeInTheDocument()
    })

    it('renders completed savings goals in separate section', () => {
      setupMocks({ goals: [mockActiveGoal, mockCompletedGoal] })
      render(<BudgetsPage />)
      expect(screen.getByText(/completed goals/i)).toBeInTheDocument()
      expect(screen.getByTestId('goal-card-goal-2')).toBeInTheDocument()
    })

    it('renders both active and completed goals correctly', () => {
      setupMocks({ goals: [mockActiveGoal, mockCompletedGoal] })
      render(<BudgetsPage />)
      expect(screen.getByText('Vacation Fund')).toBeInTheDocument()
      expect(screen.getByText('Emergency Fund')).toBeInTheDocument()
    })
  })

  describe('Create Budget Dialog', () => {
    it('opens create budget dialog when header button clicked', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create budget/i })
      await user.click(createButton)
      // Real CreateBudgetDialog renders a dialog role
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /create budget/i })).toBeInTheDocument()
    })

    it('opens create budget dialog when empty state button clicked', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [] })
      render(<BudgetsPage />)
      const emptyStateButton = screen.getByText(/create your first budget/i)
      await user.click(emptyStateButton)
      // Real CreateBudgetDialog renders a dialog role
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('closes create budget dialog on cancel', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create budget/i })
      await user.click(createButton)
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      // Real dialog has a Cancel button
      const cancelButton = screen.getByRole('button', { name: /^cancel$/i })
      await user.click(cancelButton)
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    // Issue #575: Skip this test - it requires filling in the form and submitting,
    // which is better tested in CreateBudgetDialog.test.tsx integration tests.
    // The BudgetsPage component test should focus on dialog open/close behavior.
    it.skip('closes dialog and refetches on budget created', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create budget/i })
      await user.click(createButton)
      // Would need to fill in form fields and submit
      expect(mockRefetchBudgets).toHaveBeenCalled()
    })
  })

  describe('Create Goal Dialog', () => {
    it('opens create goal dialog when header button clicked', async () => {
      const user = userEvent.setup()
      setupMocks({ goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create savings goal/i })
      await user.click(createButton)
      expect(screen.getByTestId('create-goal-dialog')).toBeInTheDocument()
    })

    it('opens create goal dialog when empty state button clicked', async () => {
      const user = userEvent.setup()
      setupMocks({ goals: [] })
      render(<BudgetsPage />)
      const emptyStateButton = screen.getByText(/create your first goal/i)
      await user.click(emptyStateButton)
      expect(screen.getByTestId('create-goal-dialog')).toBeInTheDocument()
    })

    it('closes create goal dialog on cancel', async () => {
      const user = userEvent.setup()
      setupMocks({ goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create savings goal/i })
      await user.click(createButton)
      expect(screen.getByTestId('create-goal-dialog')).toBeInTheDocument()
      await user.click(screen.getByText('Cancel Goal'))
      expect(screen.queryByTestId('create-goal-dialog')).not.toBeInTheDocument()
    })

    it('closes dialog and refetches on goal created', async () => {
      const user = userEvent.setup()
      setupMocks({ goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create savings goal/i })
      await user.click(createButton)
      await user.click(screen.getByText('Submit Goal'))
      expect(screen.queryByTestId('create-goal-dialog')).not.toBeInTheDocument()
      expect(mockRefetchGoals).toHaveBeenCalled()
    })
  })

  describe('Header Buttons', () => {
    it('renders create budget button in section header when budgets exist', () => {
      setupMocks({ budgets: [mockActiveBudget] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create budget/i })
      expect(createButton).toBeInTheDocument()
    })

    it('renders create goal button in savings goals section when goals exist', () => {
      setupMocks({ goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      const createButton = screen.getByRole('button', { name: /create savings goal/i })
      expect(createButton).toBeInTheDocument()
    })
  })

  describe('Conditional Header Button Visibility (Issue #460)', () => {
    it('hides Create Budget header button when no active budgets exist', () => {
      setupMocks({ budgets: [], goals: [] })
      render(<BudgetsPage />)
      expect(screen.getByText(/create your first budget/i)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /^create budget$/i })).not.toBeInTheDocument()
    })

    it('shows Create Budget header button when active budgets exist', () => {
      setupMocks({ budgets: [mockActiveBudget], goals: [] })
      render(<BudgetsPage />)
      expect(screen.getByRole('button', { name: /create budget/i })).toBeInTheDocument()
      expect(screen.queryByText(/create your first budget/i)).not.toBeInTheDocument()
    })

    it('hides Create Goal header button when no goals exist', () => {
      setupMocks({ budgets: [], goals: [] })
      render(<BudgetsPage />)
      expect(screen.getByText(/create your first goal/i)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /^create savings goal$/i })).not.toBeInTheDocument()
    })

    it('shows Create Goal header button when active goals exist', () => {
      setupMocks({ budgets: [], goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      expect(screen.getByRole('button', { name: /create savings goal/i })).toBeInTheDocument()
      expect(screen.queryByText(/create your first goal/i)).not.toBeInTheDocument()
    })

    it('shows Create Goal header button when only completed goals exist', () => {
      setupMocks({ budgets: [], goals: [mockCompletedGoal] })
      render(<BudgetsPage />)
      expect(screen.getByRole('button', { name: /create savings goal/i })).toBeInTheDocument()
      expect(screen.queryByText(/create your first goal/i)).not.toBeInTheDocument()
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

      // Click the budget name cell (not an action button)
      const budgetName = screen.getByText('Monthly Budget')
      await user.click(budgetName)

      expect(mockNavigate).toHaveBeenCalledWith('/budgets/budget-1')
    })

    it('inactive budget table also navigates to /budgets/<id>', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget, mockInactiveBudget] })
      render(<BudgetsPage />)

      // Both active and inactive BudgetTable instances must wire onManage
      const manageButtons = screen.getAllByRole('button', { name: /manage budget/i })
      // Second button belongs to the inactive budgets table
      await user.click(manageButtons[1])

      expect(mockNavigate).toHaveBeenCalledWith('/budgets/budget-2')
    })

    it('clicking a row in the inactive budget table navigates to /budgets/<id>', async () => {
      const user = userEvent.setup()
      setupMocks({ budgets: [mockActiveBudget, mockInactiveBudget] })
      render(<BudgetsPage />)

      // The inactive table renders mockInactiveBudget ('Past Budget', id: budget-2).
      // Click its name cell to exercise the onRowClick handler on the inactive table.
      const inactiveBudgetName = screen.getByText('Past Budget')
      await user.click(inactiveBudgetName)

      expect(mockNavigate).toHaveBeenCalledWith('/budgets/budget-2')
    })
  })

  describe('Delete Savings Goal (Issue #521, #960)', () => {
    it('opens delete goal dialog when delete button clicked', async () => {
      const user = userEvent.setup()
      setupMocks({ goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      await user.click(screen.getByTestId('delete-goal-goal-1'))
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /delete savings goal/i })).toBeInTheDocument()
    })

    it('closes delete goal dialog on cancel', async () => {
      const user = userEvent.setup()
      setupMocks({ goals: [mockActiveGoal] })
      render(<BudgetsPage />)
      await user.click(screen.getByTestId('delete-goal-goal-1'))
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: /^cancel$/i }))
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })

    it('calls deleteGoal when confirm clicked after typing goal name', async () => {
      const user = userEvent.setup()
      const localMockDeleteGoal = mock(() => Promise.resolve())
      mockUseSavingsGoals.mockReturnValue({
        goals: [mockActiveGoal],
        goalsWithProgress: [],
        loading: false,
        error: null,
        totalTarget: 0,
        totalSaved: 0,
        refetch: mockRefetchGoals,
        createGoal: mockCreateGoal,
        updateGoal: mockUpdateGoal,
        deleteGoal: localMockDeleteGoal,
      })
      render(<BudgetsPage />)
      await user.click(screen.getByTestId('delete-goal-goal-1'))
      await user.type(screen.getByRole('textbox'), 'Vacation Fund')
      await user.click(screen.getByRole('button', { name: /^delete$/i }))
      expect(localMockDeleteGoal).toHaveBeenCalledWith('goal-1')
    })

    it('can delete completed goal', async () => {
      const user = userEvent.setup()
      const localMockDeleteGoal = mock(() => Promise.resolve())
      mockUseSavingsGoals.mockReturnValue({
        goals: [mockActiveGoal, mockCompletedGoal],
        goalsWithProgress: [],
        loading: false,
        error: null,
        totalTarget: 0,
        totalSaved: 0,
        refetch: mockRefetchGoals,
        createGoal: mockCreateGoal,
        updateGoal: mockUpdateGoal,
        deleteGoal: localMockDeleteGoal,
      })
      render(<BudgetsPage />)
      await user.click(screen.getByTestId('delete-goal-goal-2'))
      expect(screen.getByRole('heading', { name: /delete savings goal/i })).toBeInTheDocument()
      await user.type(screen.getByRole('textbox'), 'Emergency Fund')
      await user.click(screen.getByRole('button', { name: /^delete$/i }))
      expect(localMockDeleteGoal).toHaveBeenCalledWith('goal-2')
    })
  })
})
