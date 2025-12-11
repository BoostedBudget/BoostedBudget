/**
 * Budgets Page
 * Issue #30: Budget Dashboard UI
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * Main page for viewing and managing budgets and savings goals.
 */

import { useState, useCallback } from 'react';
import { PlusIcon } from '@heroicons/react/24/outline';
import { useBudgets } from '../hooks/useBudgets';
import { useSavingsGoals } from '../hooks/useSavingsGoals';
import { BudgetCard } from './BudgetCard';
import { SavingsGoalCard } from './SavingsGoalCard';
import { CreateBudgetDialog } from './CreateBudgetDialog';
import { CreateSavingsGoalDialog } from './CreateSavingsGoalDialog';
import type { Budget, SavingsGoal } from '../types';
import './BudgetsPage.css';

export function BudgetsPage() {
  const { budgets, loading: budgetsLoading, error: budgetsError, refetch: refetchBudgets } = useBudgets();
  const { goals, loading: goalsLoading, error: goalsError, refetch: refetchGoals } = useSavingsGoals();

  // Dialog state
  const [isCreateBudgetOpen, setIsCreateBudgetOpen] = useState(false);
  const [isCreateGoalOpen, setIsCreateGoalOpen] = useState(false);

  // Dialog handlers
  const handleBudgetCreated = useCallback((_budget: Budget) => {
    setIsCreateBudgetOpen(false);
    refetchBudgets();
  }, [refetchBudgets]);

  const handleGoalCreated = useCallback((_goal: SavingsGoal) => {
    setIsCreateGoalOpen(false);
    refetchGoals();
  }, [refetchGoals]);

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
          <button
            type="button"
            onClick={() => setIsCreateBudgetOpen(true)}
            className="budgets-page__create-button"
            aria-label="Create budget"
          >
            <PlusIcon className="h-5 w-5" />
            <span>Create Budget</span>
          </button>
        </div>
        {activeBudgets.length === 0 ? (
          <div className="budgets-page__empty">
            <p>No active budgets. Create one to start tracking your spending.</p>
            <button
              type="button"
              onClick={() => setIsCreateBudgetOpen(true)}
              className="budgets-page__empty-button"
            >
              <PlusIcon className="h-5 w-5" />
              Create your first budget
            </button>
          </div>
        ) : (
          <div className="budgets-page__grid">
            {activeBudgets.map((budget) => (
              <BudgetCard key={budget.id} budget={budget} />
            ))}
          </div>
        )}
      </section>

      {inactiveBudgets.length > 0 && (
        <section className="budgets-page__section">
          <h2>Past Budgets</h2>
          <div className="budgets-page__grid">
            {inactiveBudgets.map((budget) => (
              <BudgetCard key={budget.id} budget={budget} />
            ))}
          </div>
        </section>
      )}

      <section className="budgets-page__section">
        <div className="budgets-page__section-header">
          <h2>Savings Goals</h2>
          <button
            type="button"
            onClick={() => setIsCreateGoalOpen(true)}
            className="budgets-page__create-button"
            aria-label="Create savings goal"
          >
            <PlusIcon className="h-5 w-5" />
            <span>Create Goal</span>
          </button>
        </div>
        {activeGoals.length === 0 && completedGoals.length === 0 ? (
          <div className="budgets-page__empty">
            <p>No savings goals yet. Create one to track your progress.</p>
            <button
              type="button"
              onClick={() => setIsCreateGoalOpen(true)}
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
                  <SavingsGoalCard key={goal.id} goal={goal} />
                ))}
              </div>
            )}
            {completedGoals.length > 0 && (
              <>
                <h3>Completed Goals</h3>
                <div className="budgets-page__grid">
                  {completedGoals.map((goal) => (
                    <SavingsGoalCard key={goal.id} goal={goal} />
                  ))}
                </div>
              </>
            )}
          </>
        )}
      </section>

      {/* Creation Dialogs */}
      <CreateBudgetDialog
        isOpen={isCreateBudgetOpen}
        onCancel={() => setIsCreateBudgetOpen(false)}
        onBudgetCreated={handleBudgetCreated}
      />
      <CreateSavingsGoalDialog
        isOpen={isCreateGoalOpen}
        onCancel={() => setIsCreateGoalOpen(false)}
        onGoalCreated={handleGoalCreated}
      />
    </div>
  );
}
