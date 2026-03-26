/**
 * Budgets Page
 * Issue #30: Budget Dashboard UI
 * Issue #111: Goals Dashboard & Progress Visualization
 * Issue #462: shadcn/ui Migration - Mutual exclusion for dialogs
 * Issue #521: Enable Budget and Savings Goal Deletion in UI
 *
 * Main page for viewing and managing budgets and savings goals.
 */

import { useCallback, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { PlusIcon } from '@heroicons/react/24/outline'
import { PieChart, Target } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { useBudgets } from '../hooks/useBudgets'
import { useSavingsGoals } from '../hooks/useSavingsGoals'
import { useBudgetFilters } from '../hooks/useBudgetFilters'
import { BudgetCard } from './BudgetCard'
import { BudgetTable, type SortColumn as BudgetSortColumn, type SortDirection } from './BudgetTable'
import { SavingsGoalCard } from './SavingsGoalCard'
import { CreateBudgetDialog } from './CreateBudgetDialog'
import { CreateSavingsGoalDialog } from './CreateSavingsGoalDialog'
import { DeleteBudgetDialog } from './DeleteBudgetDialog'
import { DeleteSavingsGoalDialog } from './DeleteSavingsGoalDialog'
import { ListViewToggle } from '@/shared/components/ListViewToggle'
import type { ListView } from '@/shared/components/ListViewToggle'
import type { FilterOption } from '@/shared/components/ColumnFilterDropdown'
import { STORAGE_KEY_LIST_VIEW } from '../constants'
import { computeBudgetMetrics, type Budget, type BudgetWithMetrics, type SavingsGoal } from '../types'
import './BudgetsPage.css'

// Issue #462, #521: Use single state for mutual exclusion - only one dialog can be open at a time
type ActiveDialog = 'budget' | 'goal' | 'delete-budget' | 'delete-goal' | null

// Issue #584: Get initial view from localStorage
function getInitialView(): ListView {
  try {
    const stored = localStorage.getItem(STORAGE_KEY_LIST_VIEW)
    if (stored === 'cards' || stored === 'table') return stored
  } catch {
    /* ignore localStorage errors */
  }
  return 'cards'
}

export function BudgetsPage() {
  const trackEvent = useTrackEvent()
  const { budgets, loading: budgetsLoading, error: budgetsError, refetch: refetchBudgets, deleteBudget } = useBudgets()
  const { goals, loading: goalsLoading, error: goalsError, refetch: refetchGoals, deleteGoal } = useSavingsGoals()

  const [activeDialog, setActiveDialog] = useState<ActiveDialog>(null)
  const [view, setView] = useState<ListView>(getInitialView)
  const [deletingBudget, setDeletingBudget] = useState<Budget | null>(null)
  const [deletingGoal, setDeletingGoal] = useState<SavingsGoal | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)
  const [sortColumn, setSortColumn] = useState<BudgetSortColumn | undefined>(undefined)
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  const { periodFilter, progressRange, setPeriodFilter, setProgressRange } = useBudgetFilters()

  const openBudgetDialog = useCallback(() => setActiveDialog('budget'), [])
  const openGoalDialog = useCallback(() => setActiveDialog('goal'), [])
  const closeDialog = useCallback(() => {
    setActiveDialog(null)
    setDeletingBudget(null)
    setDeletingGoal(null)
  }, [])

  const handleBudgetCreated = useCallback((_budget: Budget) => {
    trackEvent(ANALYTICS_EVENTS.BUDGET_CREATED)
    closeDialog()
    refetchBudgets()
  }, [trackEvent, closeDialog, refetchBudgets])

  const handleGoalCreated = useCallback((_goal: SavingsGoal) => {
    closeDialog()
    refetchGoals()
  }, [closeDialog, refetchGoals])

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
      trackEvent(ANALYTICS_EVENTS.BUDGET_DELETED)
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
  }, [deletingBudget, deleteBudget, trackEvent, closeDialog, refetchBudgets]);

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

  // Issue #590: Handle sort change
  const handleSort = useCallback((column: BudgetSortColumn, direction: SortDirection) => {
    setSortColumn(column);
    setSortDirection(direction);
  }, []);

  // Issue #590: Create period options from budgets data
  const periodOptions = useMemo<FilterOption[]>(() => {
    const periods = new Set<string>();
    budgets.forEach((budget) => {
      if (budget.period) {
        periods.add(budget.period);
      }
    });
    // Standard period order
    const periodOrder = ['weekly', 'monthly', 'quarterly', 'yearly', 'custom'];
    return Array.from(periods)
      .sort((a, b) => {
        const indexA = periodOrder.indexOf(a.toLowerCase());
        const indexB = periodOrder.indexOf(b.toLowerCase());
        return (indexA === -1 ? 999 : indexA) - (indexB === -1 ? 999 : indexB);
      })
      .map((period) => ({
        value: period,
        label: period.charAt(0).toUpperCase() + period.slice(1),
      }));
  }, [budgets]);

  // Issue #590: Filter handlers from useBudgetFilters hook (URL-synced)
  // setPeriodFilter, setProgressRange come from the hook

  // Issue #590: Filter and sort budgets client-side
  const filterAndSortBudgets = useCallback((budgetsToProcess: Budget[]): BudgetWithMetrics[] => {
    let withMetrics = budgetsToProcess.map(computeBudgetMetrics);

    // Apply period filter
    if (periodFilter.length > 0) {
      withMetrics = withMetrics.filter((budget) =>
        periodFilter.includes(budget.period)
      );
    }

    // Apply progress range filter
    if (progressRange) {
      withMetrics = withMetrics.filter((budget) => {
        const progress = budget.percent_used;
        if (progressRange.min !== undefined && progress < progressRange.min) return false;
        if (progressRange.max !== undefined && progress > progressRange.max) return false;
        return true;
      });
    }

    // Apply sorting if sortColumn is set
    if (!sortColumn) return withMetrics;

    return [...withMetrics].sort((a, b) => {
      let comparison = 0;

      switch (sortColumn) {
        case 'name': {
          comparison = a.name.localeCompare(b.name);
          break;
        }
        case 'period': {
          comparison = a.period.localeCompare(b.period);
          break;
        }
        case 'amount': {
          comparison = a.amount - b.amount;
          break;
        }
        case 'progress': {
          comparison = a.percent_used - b.percent_used;
          break;
        }
        case 'remaining': {
          comparison = a.remaining_amount - b.remaining_amount;
          break;
        }
      }

      return sortDirection === 'asc' ? comparison : -comparison;
    });
  }, [sortColumn, sortDirection, periodFilter, progressRange]);

  const loading = budgetsLoading || goalsLoading
  const error = budgetsError || goalsError

  if (loading) {
    return (
      <div className="budgets-page">
        <div className="budgets-page__loading">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600" />
          <p>Loading budgets...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="budgets-page">
        <div className="budgets-page__error">
          <span className="budgets-page__error-icon">!</span>
          <span>Error loading budgets: {error.message}</span>
        </div>
      </div>
    )
  }

  const activeBudgets = budgets.filter((b) => b.is_active)
  const inactiveBudgets = budgets.filter((b) => !b.is_active)
  const activeGoals = goals.filter((g) => !g.is_completed)
  const completedGoals = goals.filter((g) => g.is_completed)

  return (
    <div className="budgets-page">
      <header className="budgets-page__header">
        <h1>Budgets</h1>
        <ListViewToggle
          view={view}
          onChange={setView}
          storageKey={STORAGE_KEY_LIST_VIEW}
        />
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
          <EmptyState
            icon={<PieChart />}
            title="No active budgets"
            description="Create one to start tracking your spending."
            action={
              <Button onClick={openBudgetDialog}>
                <PlusIcon className="h-5 w-5" />
                Create your first budget
              </Button>
            }
          />
        ) : view === 'table' ? (
          <BudgetTable
            budgets={filterAndSortBudgets(activeBudgets)}
            onDelete={handleDeleteBudgetClick}
            sortable
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={handleSort}
            // Issue #590: Inline filter props (URL-synced via useBudgetFilters)
            periodOptions={periodOptions}
            periodFilter={periodFilter}
            onPeriodFilterChange={setPeriodFilter}
            progressRange={progressRange}
            onProgressRangeChange={setProgressRange}
          />
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
          {view === 'table' ? (
            <BudgetTable
              budgets={filterAndSortBudgets(inactiveBudgets)}
              onDelete={handleDeleteBudgetClick}
              sortable
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              // Issue #590: Inline filter props (URL-synced via useBudgetFilters)
              periodOptions={periodOptions}
              periodFilter={periodFilter}
              onPeriodFilterChange={setPeriodFilter}
              progressRange={progressRange}
              onProgressRangeChange={setProgressRange}
            />
          ) : (
            <div className="budgets-page__grid">
              {inactiveBudgets.map((budget) => (
                <BudgetCard key={budget.id} budget={budget} onDelete={handleDeleteBudgetClick} />
              ))}
            </div>
          )}
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
          <EmptyState
            icon={<Target />}
            title="No savings goals yet"
            description="Create one to track your progress."
            action={
              <Button onClick={openGoalDialog}>
                <PlusIcon className="h-5 w-5" />
                Create your first goal
              </Button>
            }
          />
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
