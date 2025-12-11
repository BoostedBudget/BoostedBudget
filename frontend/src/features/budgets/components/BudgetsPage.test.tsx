/**
 * BudgetsPage Tests
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * TDD tests for the main budgets page component.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { BudgetsPage } from './BudgetsPage';
import * as useBudgetsHook from '../hooks/useBudgets';
import * as useSavingsGoalsHook from '../hooks/useSavingsGoals';
import type { Budget, SavingsGoal } from '../types';

// Mock the hooks
vi.mock('../hooks/useBudgets');
vi.mock('../hooks/useSavingsGoals');

// Mock child components to simplify testing
vi.mock('./BudgetCard', () => ({
  BudgetCard: ({ budget }: { budget: Budget }) => (
    <div data-testid={`budget-card-${budget.id}`}>{budget.name}</div>
  ),
}));

vi.mock('./SavingsGoalCard', () => ({
  SavingsGoalCard: ({ goal }: { goal: SavingsGoal }) => (
    <div data-testid={`goal-card-${goal.id}`}>{goal.name}</div>
  ),
}));

vi.mock('./CreateBudgetDialog', () => ({
  CreateBudgetDialog: ({
    isOpen,
    onCancel,
    onBudgetCreated,
  }: {
    isOpen: boolean;
    onCancel: () => void;
    onBudgetCreated: (budget: Budget) => void;
  }) => {
    if (!isOpen) return null;
    return (
      <div data-testid="create-budget-dialog">
        <button onClick={onCancel}>Cancel Budget</button>
        <button
          onClick={() =>
            onBudgetCreated({
              id: 'new-budget',
              user_id: 'user-1',
              name: 'New Budget',
              period: 'monthly',
              start_date: '2025-01-01',
              amount: 1000,
              currency_code: 'USD',
              is_active: true,
              created_at: '2025-01-01T00:00:00Z',
              updated_at: '2025-01-01T00:00:00Z',
            })
          }
        >
          Submit Budget
        </button>
      </div>
    );
  },
}));

vi.mock('./CreateSavingsGoalDialog', () => ({
  CreateSavingsGoalDialog: ({
    isOpen,
    onCancel,
    onGoalCreated,
  }: {
    isOpen: boolean;
    onCancel: () => void;
    onGoalCreated: (goal: SavingsGoal) => void;
  }) => {
    if (!isOpen) return null;
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
    );
  },
}));

describe('BudgetsPage', () => {
  // Mock data
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
  };

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
  };

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
  };

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
  };

  const mockRefetchBudgets = vi.fn();
  const mockRefetchGoals = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Helper to set up default mocks
  function setupMocks(overrides?: {
    budgets?: Budget[];
    budgetsLoading?: boolean;
    budgetsError?: Error | null;
    goals?: SavingsGoal[];
    goalsLoading?: boolean;
    goalsError?: Error | null;
  }) {
    vi.mocked(useBudgetsHook.useBudgets).mockReturnValue({
      budgets: overrides?.budgets ?? [],
      loading: overrides?.budgetsLoading ?? false,
      error: overrides?.budgetsError ?? null,
      refetch: mockRefetchBudgets,
      createBudget: vi.fn(),
      updateBudget: vi.fn(),
      deleteBudget: vi.fn(),
    });

    vi.mocked(useSavingsGoalsHook.useSavingsGoals).mockReturnValue({
      goals: overrides?.goals ?? [],
      loading: overrides?.goalsLoading ?? false,
      error: overrides?.goalsError ?? null,
      refetch: mockRefetchGoals,
      createGoal: vi.fn(),
      updateGoal: vi.fn(),
      deleteGoal: vi.fn(),
    });
  }

  // ==========================================================================
  // Loading & Error States
  // ==========================================================================

  describe('Loading & Error States', () => {
    it('shows loading state while budgets are fetching', () => {
      setupMocks({ budgetsLoading: true });

      render(<BudgetsPage />);

      expect(screen.getByText(/loading/i)).toBeInTheDocument();
    });

    it('shows loading state while goals are fetching', () => {
      setupMocks({ goalsLoading: true });

      render(<BudgetsPage />);

      expect(screen.getByText(/loading/i)).toBeInTheDocument();
    });

    it('shows error state when budgets fetch fails', () => {
      setupMocks({ budgetsError: new Error('Failed to fetch budgets') });

      render(<BudgetsPage />);

      expect(screen.getByText(/error loading budgets/i)).toBeInTheDocument();
      expect(screen.getByText(/failed to fetch budgets/i)).toBeInTheDocument();
    });

    it('shows error state when goals fetch fails', () => {
      setupMocks({ goalsError: new Error('Failed to fetch goals') });

      render(<BudgetsPage />);

      expect(screen.getByText(/error loading budgets/i)).toBeInTheDocument();
      expect(screen.getByText(/failed to fetch goals/i)).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Empty States
  // ==========================================================================

  describe('Empty States', () => {
    it('shows empty state for budgets when none exist', () => {
      setupMocks({ budgets: [], goals: [mockActiveGoal] });

      render(<BudgetsPage />);

      expect(screen.getByText(/no active budgets/i)).toBeInTheDocument();
    });

    it('shows empty state for goals when none exist', () => {
      setupMocks({ budgets: [mockActiveBudget], goals: [] });

      render(<BudgetsPage />);

      expect(screen.getByText(/no savings goals/i)).toBeInTheDocument();
    });

    it('renders create budget button in empty state', () => {
      setupMocks({ budgets: [] });

      render(<BudgetsPage />);

      expect(screen.getByText(/create your first budget/i)).toBeInTheDocument();
    });

    it('renders create goal button in empty state', () => {
      setupMocks({ goals: [] });

      render(<BudgetsPage />);

      expect(screen.getByText(/create your first goal/i)).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Data Display
  // ==========================================================================

  describe('Data Display', () => {
    it('renders page header', () => {
      setupMocks();

      render(<BudgetsPage />);

      // The main h1 heading
      expect(screen.getByRole('heading', { level: 1, name: /budgets/i })).toBeInTheDocument();
    });

    it('renders active budgets in grid', () => {
      setupMocks({ budgets: [mockActiveBudget] });

      render(<BudgetsPage />);

      expect(screen.getByTestId('budget-card-budget-1')).toBeInTheDocument();
      expect(screen.getByText('Monthly Budget')).toBeInTheDocument();
    });

    it('renders inactive/past budgets in separate section', () => {
      setupMocks({ budgets: [mockActiveBudget, mockInactiveBudget] });

      render(<BudgetsPage />);

      expect(screen.getByText(/past budgets/i)).toBeInTheDocument();
      expect(screen.getByTestId('budget-card-budget-2')).toBeInTheDocument();
    });

    it('does not render past budgets section when none exist', () => {
      setupMocks({ budgets: [mockActiveBudget] });

      render(<BudgetsPage />);

      expect(screen.queryByText(/past budgets/i)).not.toBeInTheDocument();
    });

    it('renders active savings goals', () => {
      setupMocks({ goals: [mockActiveGoal] });

      render(<BudgetsPage />);

      expect(screen.getByTestId('goal-card-goal-1')).toBeInTheDocument();
      expect(screen.getByText('Vacation Fund')).toBeInTheDocument();
    });

    it('renders completed savings goals in separate section', () => {
      setupMocks({ goals: [mockActiveGoal, mockCompletedGoal] });

      render(<BudgetsPage />);

      expect(screen.getByText(/completed goals/i)).toBeInTheDocument();
      expect(screen.getByTestId('goal-card-goal-2')).toBeInTheDocument();
    });

    it('renders both active and completed goals correctly', () => {
      setupMocks({ goals: [mockActiveGoal, mockCompletedGoal] });

      render(<BudgetsPage />);

      expect(screen.getByText('Vacation Fund')).toBeInTheDocument();
      expect(screen.getByText('Emergency Fund')).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Create Budget Dialog
  // ==========================================================================

  describe('Create Budget Dialog', () => {
    it('opens create budget dialog when header button clicked', async () => {
      const user = userEvent.setup();
      setupMocks();

      render(<BudgetsPage />);

      // Click the Create Budget button in the header
      const createButton = screen.getByRole('button', { name: /create budget/i });
      await user.click(createButton);

      expect(screen.getByTestId('create-budget-dialog')).toBeInTheDocument();
    });

    it('opens create budget dialog when empty state button clicked', async () => {
      const user = userEvent.setup();
      setupMocks({ budgets: [] });

      render(<BudgetsPage />);

      // Click the empty state button
      const emptyStateButton = screen.getByText(/create your first budget/i);
      await user.click(emptyStateButton);

      expect(screen.getByTestId('create-budget-dialog')).toBeInTheDocument();
    });

    it('closes create budget dialog on cancel', async () => {
      const user = userEvent.setup();
      setupMocks();

      render(<BudgetsPage />);

      // Open dialog
      const createButton = screen.getByRole('button', { name: /create budget/i });
      await user.click(createButton);

      expect(screen.getByTestId('create-budget-dialog')).toBeInTheDocument();

      // Cancel dialog
      await user.click(screen.getByText('Cancel Budget'));

      expect(screen.queryByTestId('create-budget-dialog')).not.toBeInTheDocument();
    });

    it('closes dialog and refetches on budget created', async () => {
      const user = userEvent.setup();
      setupMocks();

      render(<BudgetsPage />);

      // Open dialog
      const createButton = screen.getByRole('button', { name: /create budget/i });
      await user.click(createButton);

      // Submit
      await user.click(screen.getByText('Submit Budget'));

      // Dialog should close
      expect(screen.queryByTestId('create-budget-dialog')).not.toBeInTheDocument();

      // Refetch should be called
      expect(mockRefetchBudgets).toHaveBeenCalled();
    });
  });

  // ==========================================================================
  // Create Goal Dialog
  // ==========================================================================

  describe('Create Goal Dialog', () => {
    it('opens create goal dialog when header button clicked', async () => {
      const user = userEvent.setup();
      setupMocks();

      render(<BudgetsPage />);

      // Click the Create Goal button in the header (has aria-label="Create savings goal")
      const createButton = screen.getByRole('button', { name: /create savings goal/i });
      await user.click(createButton);

      expect(screen.getByTestId('create-goal-dialog')).toBeInTheDocument();
    });

    it('opens create goal dialog when empty state button clicked', async () => {
      const user = userEvent.setup();
      setupMocks({ goals: [] });

      render(<BudgetsPage />);

      // Click the empty state button
      const emptyStateButton = screen.getByText(/create your first goal/i);
      await user.click(emptyStateButton);

      expect(screen.getByTestId('create-goal-dialog')).toBeInTheDocument();
    });

    it('closes create goal dialog on cancel', async () => {
      const user = userEvent.setup();
      setupMocks();

      render(<BudgetsPage />);

      // Open dialog
      const createButton = screen.getByRole('button', { name: /create savings goal/i });
      await user.click(createButton);

      expect(screen.getByTestId('create-goal-dialog')).toBeInTheDocument();

      // Cancel dialog
      await user.click(screen.getByText('Cancel Goal'));

      expect(screen.queryByTestId('create-goal-dialog')).not.toBeInTheDocument();
    });

    it('closes dialog and refetches on goal created', async () => {
      const user = userEvent.setup();
      setupMocks();

      render(<BudgetsPage />);

      // Open dialog
      const createButton = screen.getByRole('button', { name: /create savings goal/i });
      await user.click(createButton);

      // Submit
      await user.click(screen.getByText('Submit Goal'));

      // Dialog should close
      expect(screen.queryByTestId('create-goal-dialog')).not.toBeInTheDocument();

      // Refetch should be called
      expect(mockRefetchGoals).toHaveBeenCalled();
    });
  });

  // ==========================================================================
  // Header Buttons
  // ==========================================================================

  describe('Header Buttons', () => {
    it('renders create budget button in section header', () => {
      setupMocks();

      render(<BudgetsPage />);

      const createButton = screen.getByRole('button', { name: /create budget/i });
      expect(createButton).toBeInTheDocument();
    });

    it('renders create goal button in savings goals section', () => {
      setupMocks();

      render(<BudgetsPage />);

      const createButton = screen.getByRole('button', { name: /create savings goal/i });
      expect(createButton).toBeInTheDocument();
    });
  });
});
