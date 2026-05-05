/**
 * Budgets Page
 * Issue #30: Budget Dashboard UI
 * Issue #111: Goals Dashboard & Progress Visualization
 * Issue #462: shadcn/ui Migration - Mutual exclusion for dialogs
 * Issue #521: Enable Budget Deletion in UI
 *
 * Main page for viewing and managing budgets.
 */

import { useCallback, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { PieChart, Plus } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { useBudgets } from '../hooks/useBudgets'
import { useBudgetFilters } from '../hooks/useBudgetFilters'
import { BudgetCard } from './BudgetCard'
import { BudgetTable, type SortColumn as BudgetSortColumn, type SortDirection } from './BudgetTable'
import { CreateBudgetDialog } from './CreateBudgetDialog'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { ListViewToggle } from '@/shared/components/ListViewToggle'
import type { ListView } from '@/shared/components/ListViewToggle'
import type { FilterOption } from '@/shared/components/ColumnFilterDropdown'
import { STORAGE_KEY_LIST_VIEW, getBudgetDetailPath } from '../constants'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { computeBudgetMetrics, type Budget, type BudgetWithMetrics } from '../types'

type ActiveDialog = 'budget' | 'delete-budget' | null

function formatCurrency(amount: number, currencyCode: string): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currencyCode,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount)
  } catch {
    return `${currencyCode} ${amount.toFixed(2)}`
  }
}

function getInitialView(): ListView {
  try {
    const stored = localStorage.getItem(STORAGE_KEY_LIST_VIEW)
    if (stored === 'cards' || stored === 'table') return stored
  } catch {
  }
  return 'cards'
}

export function BudgetsPage() {
  const navigate = useNavigate()
  const trackEvent = useTrackEvent()
  const { budgets, loading: budgetsLoading, error: budgetsError, refetch: refetchBudgets, deleteBudget } = useBudgets()

  const [activeDialog, setActiveDialog] = useState<ActiveDialog>(null)
  const [view, setView] = useState<ListView>(getInitialView)
  const [deletingBudget, setDeletingBudget] = useState<Budget | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)
  const [sortColumn, setSortColumn] = useState<BudgetSortColumn | undefined>(undefined)
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  const { periodFilter, progressRange, setPeriodFilter, setProgressRange } = useBudgetFilters()

  const openBudgetDialog = useCallback(() => setActiveDialog('budget'), [])
  const closeDialog = useCallback(() => {
    setActiveDialog(null)
    setDeletingBudget(null)
  }, [])

  const handleBudgetCreated = useCallback((_budget: Budget) => {
    trackEvent(ANALYTICS_EVENTS.BUDGET_CREATED)
    if (!budgetsLoading && budgets.length === 0) {
      trackEvent(ANALYTICS_EVENTS.FIRST_BUDGET_CREATED)
    }
    closeDialog()
    refetchBudgets()
  }, [trackEvent, closeDialog, refetchBudgets, budgets, budgetsLoading])

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

  const handleSort = useCallback((column: BudgetSortColumn, direction: SortDirection) => {
    setSortColumn(column);
    setSortDirection(direction);
  }, []);

  const handleOpenBudget = useCallback((budget: BudgetWithMetrics) => {
    navigate(getBudgetDetailPath(budget.id));
  }, [navigate]);

  const periodOptions = useMemo<FilterOption[]>(() => {
    const periods = new Set<string>();
    budgets.forEach((budget) => {
      if (budget.period) {
        periods.add(budget.period);
      }
    });
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

  const filterAndSortBudgets = useCallback((budgetsToProcess: Budget[]): BudgetWithMetrics[] => {
    let withMetrics = budgetsToProcess.map(computeBudgetMetrics);

    if (periodFilter.length > 0) {
      withMetrics = withMetrics.filter((budget) =>
        periodFilter.includes(budget.period)
      );
    }

    if (progressRange) {
      withMetrics = withMetrics.filter((budget) => {
        const progress = budget.percent_used;
        if (progressRange.min !== undefined && progress < progressRange.min) return false;
        if (progressRange.max !== undefined && progress > progressRange.max) return false;
        return true;
      });
    }

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
          comparison = (a.expected_income ?? a.amount) - (b.expected_income ?? b.amount);
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

  const loading = budgetsLoading
  const error = budgetsError

  if (loading) {
    return (
      <div className="p-6 max-w-[1200px] mx-auto">
        <div className="flex flex-col items-center justify-center h-[300px] gap-4 text-muted-foreground">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600" />
          <p>Loading budgets...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 max-w-[1200px] mx-auto">
        <div className="flex items-center justify-center h-[200px] gap-2 text-destructive">
          <span className="flex items-center justify-center w-6 h-6 bg-destructive text-white rounded-full text-xs font-bold">!</span>
          <span>Error loading budgets: {error.message}</span>
        </div>
      </div>
    )
  }

  const activeBudgets = budgets.filter((b) => b.is_active)
  const inactiveBudgets = budgets.filter((b) => !b.is_active)

  return (
    <div className="p-6 max-w-[1200px] mx-auto">
      <header className="mb-8 flex items-center justify-between">
        <h1 className="m-0 text-[1.75rem] font-bold text-foreground">Budgets</h1>
        <ListViewToggle
          view={view}
          onChange={setView}
          storageKey={STORAGE_KEY_LIST_VIEW}
        />
      </header>

      <section className="mb-8">
        <div className="flex items-center justify-between mb-4">
          <h2 className="m-0 text-xl font-semibold text-foreground">Active Budgets</h2>
          {activeBudgets.length > 0 && (
            <ReadOnlyGuard>
              <Button type="button" onClick={openBudgetDialog}>
                <Plus />
                Create Budget
              </Button>
            </ReadOnlyGuard>
          )}
        </div>
        {activeBudgets.length === 0 ? (
          <EmptyState
            icon={<PieChart />}
            title="No active budgets"
            description="Create one to start tracking your spending."
            action={
              <ReadOnlyGuard>
                <Button onClick={openBudgetDialog}>
                  <Plus />
                  Create your first budget
                </Button>
              </ReadOnlyGuard>
            }
          />
        ) : view === 'table' ? (
          <BudgetTable
            budgets={filterAndSortBudgets(activeBudgets)}
            onManage={handleOpenBudget}
            onDelete={handleDeleteBudgetClick}
            onRowClick={handleOpenBudget}
            sortable
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={handleSort}
            periodOptions={periodOptions}
            periodFilter={periodFilter}
            onPeriodFilterChange={setPeriodFilter}
            progressRange={progressRange}
            onProgressRangeChange={setProgressRange}
          />
        ) : (
          <div className="grid gap-4 [grid-template-columns:repeat(auto-fill,minmax(320px,1fr))]">
            {activeBudgets.map((budget) => (
              <BudgetCard key={budget.id} budget={budget} onDelete={handleDeleteBudgetClick} />
            ))}
          </div>
        )}
      </section>

      {inactiveBudgets.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-4 text-xl font-semibold text-foreground">Past Budgets</h2>
          {view === 'table' ? (
            <BudgetTable
              budgets={filterAndSortBudgets(inactiveBudgets)}
              onManage={handleOpenBudget}
              onDelete={handleDeleteBudgetClick}
              onRowClick={handleOpenBudget}
              sortable
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              periodOptions={periodOptions}
              periodFilter={periodFilter}
              onPeriodFilterChange={setPeriodFilter}
              progressRange={progressRange}
              onProgressRangeChange={setProgressRange}
            />
          ) : (
            <div className="grid gap-4 [grid-template-columns:repeat(auto-fill,minmax(320px,1fr))]">
              {inactiveBudgets.map((budget) => (
                <BudgetCard key={budget.id} budget={budget} onDelete={handleDeleteBudgetClick} />
              ))}
            </div>
          )}
        </section>
      )}

      <CreateBudgetDialog
        isOpen={activeDialog === 'budget'}
        onCancel={closeDialog}
        onBudgetCreated={handleBudgetCreated}
      />

      <ConfirmationDialog
        open={activeDialog === 'delete-budget'}
        onOpenChange={(open) => !open && closeDialog()}
        onConfirm={handleConfirmDeleteBudget}
        title="Delete Budget"
        description={deletingBudget ? (
          <>
            Are you sure you want to delete &ldquo;{deletingBudget.name}&rdquo;? This budget and its{' '}
            {deletingBudget.categories?.length === 1 ? '1 category' : `${deletingBudget.categories?.length ?? 0} categories`}{' '}
            will be permanently deleted. This action cannot be undone.
          </>
        ) : ''}
        confirmText={deletingBudget?.name ?? ''}
        loading={isDeleting}
      />
    </div>
  );
}
