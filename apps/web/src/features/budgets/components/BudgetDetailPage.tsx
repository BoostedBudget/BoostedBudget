/**
 * BudgetDetailPage
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #498: Budget Detail Page for Envelope Budgeting
 *
 * Page component for viewing and managing a single budget's details and categories.
 */

import { lazy, Suspense, useCallback, useEffect, useMemo } from 'react';
import { useParams, Link, useNavigate } from 'react-router';
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { ArrowLeftIcon, BellAlertIcon, ArrowPathIcon } from '@heroicons/react/24/outline';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { formatDate } from '@/shared/transactions/dates';
import { useBudgetDetail } from '../hooks/useBudgetDetail';
import { useBudgetPlanVsActual } from '../hooks/useBudgetPlanVsActual';
import { useAvailableCategories } from '../hooks/useAvailableCategories';
import { useTags } from '../../tags/hooks/useTags';
import {
  useAddBudgetCategory,
  useUpdateBudgetCategory,
  useDeleteBudgetCategory,
} from '@boostedbudget/api-client/endpoints/budgets/budgets';
import type {
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
  BudgetCategory,
} from '../types';
import { filterTopLevel } from '../types';
import { BudgetCategoryManager } from './category-management/BudgetCategoryManager';
import { BudgetIncomeSection } from './budget-detail/BudgetIncomeSection';
import { UnallocatedFundsCard } from './budget-detail/UnallocatedFundsCard';

const BudgetPlanActivityPanel = lazy(() => import('./BudgetPlanActivityPanel'));

function formatCurrency(amount: number, currencyCode = 'USD'): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: currencyCode,
  }).format(amount);
}

function capitalize(str: string): string {
  return str.charAt(0).toUpperCase() + str.slice(1);
}

export function BudgetDetailPage() {
  const trackEvent = useTrackEvent()
  const { budgetId } = useParams<{ budgetId: string }>();
  const navigate = useNavigate();
  const { budget, loading, error, refetch } = useBudgetDetail(budgetId || '');
  const report = useBudgetPlanVsActual(budgetId || '');
  const { categories: availableCategories } = useAvailableCategories();
  const { tags: availableTags } = useTags();

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.BUDGET_DETAIL_VIEWED);
  }, [trackEvent]);

  const addCategoryMutation = useAddBudgetCategory();
  const updateCategoryMutation = useUpdateBudgetCategory();
  const deleteCategoryMutation = useDeleteBudgetCategory();

  // Issue #498 / #1048: Calculate income and allocation metrics
  const incomeMetrics = useMemo(() => {
    if (!budget) return { expectedIncome: 0, actualIncome: 0, unallocated: 0, totalAllocated: 0 };

    // Issue #1048: Use expected_income as the first-class field; fall back to
    // amount for backwards compat with budgets created before this field existed.
    const expectedIncome = budget.expected_income ?? budget.amount ?? 0;

    // Issue #1048: Actual income is what was received — no fallback to amount.
    const actualIncome = budget.income_amount ?? 0;

    // Calculate total allocated from TOP-LEVEL categories only (#1203/#1630) — a
    // sub-allocation's amount is already inside its parent's, so summing every row
    // double-counts.
    const totalAllocated = filterTopLevel(budget.categories || []).reduce(
      (sum, cat) => sum + cat.allocated_amount,
      0
    );

    // Unallocated is relative to the planned target, not actual received income.
    const unallocated = expectedIncome - totalAllocated;

    return { expectedIncome, actualIncome, unallocated, totalAllocated };
  }, [budget]);

  const handleCreateSavingsGoal = useCallback(() => {
    navigate('/savings', { state: { openCreateDialog: true } });
  }, [navigate]);

  const handleAddCategory = useCallback(
    async (req: CreateBudgetCategoryRequest): Promise<BudgetCategory> => {
      const result = await addCategoryMutation.mutateAsync({
        id: budgetId || '',
        data: req,
      });
      await refetch();
      trackEvent(ANALYTICS_EVENTS.BUDGET_CATEGORY_ADDED);
      return result as BudgetCategory;
    },
    [budgetId, refetch, trackEvent, addCategoryMutation]
  );

  const handleUpdateCategory = useCallback(
    async (categoryId: string, req: UpdateBudgetCategoryRequest): Promise<BudgetCategory> => {
      const result = await updateCategoryMutation.mutateAsync({
        id: budgetId || '',
        categoryId,
        data: req,
      });
      trackEvent(ANALYTICS_EVENTS.BUDGET_CATEGORY_UPDATED);
      await refetch();
      return result as BudgetCategory;
    },
    [budgetId, refetch, trackEvent, updateCategoryMutation]
  );

  const handleDeleteCategory = useCallback(
    async (categoryId: string): Promise<void> => {
      await deleteCategoryMutation.mutateAsync({
        id: budgetId || '',
        categoryId,
      });
      await refetch();
      trackEvent(ANALYTICS_EVENTS.BUDGET_CATEGORY_DELETED);
    },
    [budgetId, refetch, trackEvent, deleteCategoryMutation]
  );

  if (loading) {
    return (
      <div className="max-w-[1200px] mx-auto p-6">
        <div className="flex flex-col items-center justify-center min-h-[300px] gap-4 text-muted-foreground">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary" />
          <p>Loading budget...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-[1200px] mx-auto p-6">
        <div className="flex flex-col items-center justify-center gap-4 p-8 bg-destructive/10 rounded-lg text-destructive">
          <span className="flex items-center justify-center w-10 h-10 rounded-full bg-destructive/10 text-destructive font-semibold text-xl">!</span>
          <span>Error: {error.message}</span>
          <Button
            type="button"
            variant="destructive"
            onClick={refetch}
          >
            Retry
          </Button>
        </div>
      </div>
    );
  }

  if (!budget) {
    return (
      <div className="max-w-[1200px] mx-auto p-6">
        <div className="flex flex-col items-center justify-center gap-4 p-8 text-muted-foreground">
          <p>Budget not found</p>
          <Link to="/budgets" className="inline-flex items-center gap-2 mb-4 text-sm text-primary no-underline hover:text-primary/80 transition-colors">
            <ArrowLeftIcon className="h-5 w-5" />
            Back to Budgets
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="w-full min-w-0 max-w-[1200px] mx-auto p-4 sm:p-6">
      {/* Header */}
      <header className="mb-8">
        <Link to="/budgets" className="inline-flex items-center gap-2 mb-4 text-sm text-primary no-underline hover:text-primary/80 transition-colors">
          <ArrowLeftIcon className="h-5 w-5" />
          Back to Budgets
        </Link>

        <div className="flex flex-wrap items-center gap-3 mb-2">
          <h1 className="m-0 min-w-0 break-words text-3xl font-bold text-foreground"><span data-ph-mask>{budget.name}</span></h1>
          {budget.is_active && (
            <Badge variant="success">Active</Badge>
          )}
        </div>

        {budget.description && (
          <p className="m-0 mb-4 text-muted-foreground"><span data-ph-mask>{budget.description}</span></p>
        )}

        <div className="flex flex-wrap gap-6 p-4 bg-muted rounded-lg">
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Period</span>
            <span className="text-sm font-semibold text-foreground">
              {capitalize(budget.period)}
            </span>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Dates</span>
            <span className="text-sm font-semibold text-foreground">
              {formatDate(budget.start_date)}{budget.end_date ? ` - ${formatDate(budget.end_date)}` : ''}
            </span>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Expected Income</span>
            <span className="text-sm font-semibold text-foreground" data-ph-mask>
              {formatCurrency(budget.expected_income ?? budget.amount, budget.currency_code)}
            </span>
          </div>
        </div>

        {/* Issue #39: Quick-create automation rules */}
        <div className="flex flex-wrap gap-3 mt-4">
          <Link
            to={`/boosts/new?type=budget_alert&budget_id=${budgetId}`}
            className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium text-primary bg-primary/10 border border-primary/30 rounded-lg no-underline hover:bg-primary/20 hover:border-primary/50 transition-all"
            aria-label="Create budget alert rule"
          >
            <BellAlertIcon className="h-4 w-4" aria-hidden="true" />
            Create Alert Rule
          </Link>
          <Link
            to={`/boosts/new?type=category_rollover&budget_id=${budgetId}`}
            className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium text-primary bg-primary/10 border border-primary/30 rounded-lg no-underline hover:bg-primary/20 hover:border-primary/50 transition-all"
            aria-label="Create category rollover rule"
          >
            <ArrowPathIcon className="h-4 w-4" aria-hidden="true" />
            Create Rollover Rule
          </Link>
        </div>
      </header>

      {/* Issue #498: Income Section - shows expected vs actual income */}
      {(budget.income_sources?.length > 0 || incomeMetrics.expectedIncome > 0) && (
        <section className="mt-6">
          {report.data?.income.observed != null ? <BudgetIncomeSection
            expectedIncome={incomeMetrics.expectedIncome}
            actualIncome={report.data.income.observed}
            incomeSources={budget.income_sources || []}
            currencyCode={budget.currency_code}
          /> : <div role="region" aria-label="Income" className="rounded-lg border p-4"><p>Expected</p><p data-ph-mask>{formatCurrency(incomeMetrics.expectedIncome, budget.currency_code)}</p><p>Observed income</p><p>Unavailable</p><p className="text-sm text-muted-foreground">Manual declarations are not received income. Posted income appears when the activity report is available.</p></div>}
        </section>
      )}

      {/* Issue #498: Unallocated Funds Card */}
      <section className="mt-6">
        <UnallocatedFundsCard
          unallocatedAmount={incomeMetrics.unallocated}
          totalIncome={incomeMetrics.expectedIncome}
          currencyCode={budget.currency_code}
          onCreateSavingsGoal={handleCreateSavingsGoal}
        />
      </section>

      {/* Category Manager */}
      <section className="mt-8">
        {report.isLoading ? <p role="status">Loading budget plan and activity...</p> : report.error ? (
          <div role="alert"><p>Budget activity could not be refreshed.</p><Button variant="outline" onClick={() => { void report.refetch() }}>Retry budget activity</Button></div>
        ) : report.data ? (
          <Suspense fallback={<p role="status">Loading budget plan and activity...</p>}>
            <BudgetPlanActivityPanel data={report.data} onRefresh={() => { void report.refetch(); void refetch() }} />
          </Suspense>
        ) : null}
      </section>
      <section className="mt-8">
        <BudgetCategoryManager
          currencyCode={budget.currency_code}
          budgetId={budgetId || ''}
          periodStart={report.data?.period_start ?? undefined}
          actualThrough={report.data?.actual_through ?? undefined}
          categories={budget.categories || []}
          totalIncome={budget.expected_income ?? budget.amount}
          onAddCategory={handleAddCategory}
          onUpdateCategory={handleUpdateCategory}
          onDeleteCategory={handleDeleteCategory}
          availableCategories={availableCategories}
          availableTags={availableTags}
        />
      </section>
    </div>
  );
}
