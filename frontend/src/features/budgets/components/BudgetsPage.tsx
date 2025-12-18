/**
 * Budgets Page
 * Issue #30: Budget Dashboard UI
 * Issue #111: Goals Dashboard & Progress Visualization
 * Issue #462: shadcn/ui Migration - Mutual exclusion for dialogs
 * Issue #521: Enable Budget and Savings Goal Deletion in UI
 *
 * Main page for viewing and managing budgets and savings goals.
 */

import { useState, useCallback } from 'react';
import { toast } from 'sonner';
import { PlusIcon } from '@heroicons/react/24/outline';
import { useBudgets } from '../hooks/useBudgets';
import { useSavingsGoals } from '../hooks/useSavingsGoals';
import { BudgetCard } from './BudgetCard';
import { SavingsGoalCard } from './SavingsGoalCard';
import { CreateBudgetDialog } from './CreateBudgetDialog';
import { CreateSavingsGoalDialog } from './CreateSavingsGoalDialog';
import { DeleteBudgetDialog } from './DeleteBudgetDialog';
import { DeleteSavingsGoalDialog } from './DeleteSavingsGoalDialog';
import type { Budget, SavingsGoal } from '../types';
import './BudgetsPage.css';

// Issue #462, #521: Use single state for mutual exclusion - only one dialog can be open at a time
type ActiveDialog = 'budget' | 'goal' | 'delete-budget' | 'delete-goal' | null;

export function BudgetsPage() {
  const { budgets, loading: budgetsLoading, error: budgetsError, refetch: refetchBudgets, deleteBudget } = useBudgets();
  const { goals, loading: goalsLoading, error: goalsError, refetch: refetchGoals, deleteGoal } = useSavingsGoals();

  // Dialog state - single state ensures only one dialog open at a time
  const [activeDialog, setActiveDialog] = useState<ActiveDialog>(null);

  // Issue #521: Delete state
  const [deletingBudget, setDeletingBudget] = useState<Budget | null>(null);
  const [deletingGoal, setDeletingGoal] = useState<SavingsGoal | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);

  // Dialog handlers
  const openBudgetDialog = useCallback(() => setActiveDialog('budget'), []);
  const openGoalDialog = useCallback(() => setActiveDialog('goal'), []);
  const closeDialog = useCallback(() => {
    setActiveDialog(null);
    setDeletingBudget(null);
    setDeletingGoal(null);
  }, []);

  const handleBudgetCreated = useCallback((_budget: Budget) => {
    closeDialog();
    refetchBudgets();
  }, [closeDialog, refetchBudgets]);

  const handleGoalCreated = useCallback((_goal: SavingsGoal) => {
    closeDialog();
    refetchGoals();
  }, [closeDialog, refetchGoals]);

  // Issue #521: Delete budget handlers
  const handleDeleteBudgetClick = useCallback((budgetId: string) => {
    const budget = budgets.find((b) => b.id === budgetId);
    if (budget) {
      setDeletingBudget(budget);
      setActiveDialog('delete-budget');
    }
  }, [budgets]);

  const handleConfirmDeleteBudget = useCallback(async () => {
    if (!deletingBudget) return;

    setIsDeleting(true);
    try {
      await deleteBudget(deletingBudget.id);
      toast.success(`Budget "${deletingBudget.name}" deleted successfully`);
      closeDialog();
    } catch (error) {
      if (error instanceof Error && error.message.includes('404')) {
        toast.error('Budget was already deleted');
        closeDialog();
        refetchBudgets();
      } else if (error instanceof Error && error.message.includes('401')) {
        toast.error('Session expired. Please log in again.');
      } else {
        toast.error('Failed to delete budget. Please try again.');
      }
    } finally {
      setIsDeleting(false);
    }
  }, [deletingBudget, deleteBudget, closeDialog, refetchBudgets]);

  // Issue #521: Delete goal handlers
  const handleDeleteGoalClick = useCallback((goalId: string) => {
    const goal = goals.find((g) => g.id === goalId);
    if (goal) {
      setDeletingGoal(goal);
      setActiveDialog('delete-goal');
    }
  }, [goals]);

  const handleConfirmDeleteGoal = useCallback(async () => {
    if (!deletingGoal) return;

    setIsDeleting(true);
    try {
      await deleteGoal(deletingGoal.id);
      toast.success(`Savings goal "${deletingGoal.name}" deleted successfully`);
      closeDialog();
    } catch (error) {
      if (error instanceof Error && error.message.includes('404')) {
        toast.error('Savings goal was already deleted');
        closeDialog();
        refetchGoals();
      } else if (error instanceof Error && error.message.includes('401')) {
        toast.error('Session expired. Please log in again.');
      } else {
        toast.error('Failed to delete savings goal. Please try again.');
      }
    } finally {
      setIsDeleting(false);
    }
  }, [deletingGoal, deleteGoal, closeDialog, refetchGoals]);

  const loading = budgetsLoading || goalsLoading;
  const error = budgetsError || goalsError;

  if (loading) {
    return (
      <div className="budgets-page">
        <div className="budgets-page__loading">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600" />
          <p>Loading budgets...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="budgets-page">
        <div className="budgets-page__error">
          <span className="budgets-page__error-icon">!</span>
          <span>Error loading budgets: {error.message}</span>
        </div>
      </div>
    );
  }

  const activeBudgets = budgets.filter((b) => b.is_active);
  const inactiveBudgets = budgets.filter((b) => !b.is_active);
  const activeGoals = goals.filter((g) => !g.is_completed);
  const completedGoals = goals.filter((g) => g.is_completed);

  return (
    <div className="budgets-page">
      <header className="budgets-page__header">
        <div>
          <h1>Budgets</h1>
          <p className="budgets-page__subtitle">
            Track your spending and savings goals
          </p>
        </div>
      </header>

      <section className="budgets-page__section">
        <div className="budgets-page__section-header">
          <h2>Active Budgets</h2>
          {activeBudgets.length > 0 && (
            <button
              type="button"
              onClick={openBudgetDialog}
              className="budgets-page__create-button"
              aria-label="Create budget"
            >
              <PlusIcon className="h-5 w-5" />
              <span>Create Budget</span>
            </button>
          )}
        </div>
        {activeBudgets.length === 0 ? (
          <div className="budgets-page__empty">
            <p>No active budgets. Create one to start tracking your spending.</p>
            <button
              type="button"
              onClick={openBudgetDialog}
              className="budgets-page__empty-button"
            >
              <PlusIcon className="h-5 w-5" />
              Create your first budget
            </button>
          </div>
        ) : (
          <div className="budgets-page__grid">
            {activeBudgets.map((budget) => (
              <BudgetCard key={budget.id} budget={budget} onDelete={handleDeleteBudgetClick} />
            ))}
          </div>
        )}
      </section>

      {inactiveBudgets.length > 0 && (
        <section className="budgets-page__section">
          <h2>Past Budgets</h2>
          <div className="budgets-page__grid">
            {inactiveBudgets.map((budget) => (
              <BudgetCard key={budget.id} budget={budget} onDelete={handleDeleteBudgetClick} />
            ))}
          </div>
        </section>
      )}

      <section className="budgets-page__section">
        <div className="budgets-page__section-header">
          <h2>Savings Goals</h2>
          {(activeGoals.length > 0 || completedGoals.length > 0) && (
            <button
              type="button"
              onClick={openGoalDialog}
              className="budgets-page__create-button"
              aria-label="Create savings goal"
            >
              <PlusIcon className="h-5 w-5" />
              <span>Create Goal</span>
            </button>
          )}
        </div>
        {activeGoals.length === 0 && completedGoals.length === 0 ? (
          <div className="budgets-page__empty">
            <p>No savings goals yet. Create one to track your progress.</p>
            <button
              type="button"
              onClick={openGoalDialog}
              className="budgets-page__empty-button"
            >
              <PlusIcon className="h-5 w-5" />
              Create your first goal
            </button>
          </div>
        ) : (
          <>
            {activeGoals.length > 0 && (
              <div className="budgets-page__grid">
                {activeGoals.map((goal) => (
                  <SavingsGoalCard key={goal.id} goal={goal} onDelete={handleDeleteGoalClick} />
                ))}
              </div>
            )}
            {completedGoals.length > 0 && (
              <>
                <h3>Completed Goals</h3>
                <div className="budgets-page__grid">
                  {completedGoals.map((goal) => (
                    <SavingsGoalCard key={goal.id} goal={goal} onDelete={handleDeleteGoalClick} />
                  ))}
                </div>
              </>
            )}
          </>
        )}
      </section>

      {/* Creation Dialogs - Only one can be open at a time (Issue #462) */}
      <CreateBudgetDialog
        isOpen={activeDialog === 'budget'}
        onCancel={closeDialog}
        onBudgetCreated={handleBudgetCreated}
      />
      <CreateSavingsGoalDialog
        isOpen={activeDialog === 'goal'}
        onCancel={closeDialog}
        onGoalCreated={handleGoalCreated}
      />

      {/* Deletion Dialogs (Issue #521) */}
      <DeleteBudgetDialog
        isOpen={activeDialog === 'delete-budget'}
        budgetName={deletingBudget?.name ?? ''}
        categoryCount={deletingBudget?.categories?.length ?? 0}
        onConfirm={handleConfirmDeleteBudget}
        onCancel={closeDialog}
        isDeleting={isDeleting}
      />
      <DeleteSavingsGoalDialog
        isOpen={activeDialog === 'delete-goal'}
        goalName={deletingGoal?.name ?? ''}
        currentAmount={deletingGoal?.current_amount ?? 0}
        targetAmount={deletingGoal?.target_amount ?? 0}
        currencyCode={deletingGoal?.currency_code ?? 'USD'}
        onConfirm={handleConfirmDeleteGoal}
        onCancel={closeDialog}
        isDeleting={isDeleting}
      />
    </div>
  );
}
