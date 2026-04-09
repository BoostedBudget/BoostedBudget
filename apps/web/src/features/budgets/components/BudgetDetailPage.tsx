/**
 * BudgetDetailPage
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #498: Budget Detail Page for Envelope Budgeting
 *
 * Page component for viewing and managing a single budget's details and categories.
 */

import { useCallback, useEffect, useMemo } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { ArrowLeftIcon, BellAlertIcon, ArrowPathIcon } from '@heroicons/react/24/outline';
import { useBudgetDetail } from '../hooks/useBudgetDetail';
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
import { BudgetCategoryManager } from './category-management/BudgetCategoryManager';
import { BudgetIncomeSection } from './budget-detail/BudgetIncomeSection';
import { UnallocatedFundsCard } from './budget-detail/UnallocatedFundsCard';

/**
 * Format a date string for display
 */
function formatDate(dateString: string): string {
  return new Date(dateString).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  });
}

/**
 * Format currency for display
 */
function formatCurrency(amount: number, currencyCode = 'USD'): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: currencyCode,
  }).format(amount);
}

/**
 * Capitalize the first letter of a string
 */
function capitalize(str: string): string {
  return str.charAt(0).toUpperCase() + str.slice(1);
}

export function BudgetDetailPage() {
  const trackEvent = useTrackEvent()
  const { budgetId } = useParams<{ budgetId: string }>();
  const navigate = useNavigate();
  const { budget, loading, error, refetch } = useBudgetDetail(budgetId || '');
  const { categories: availableCategories } = useAvailableCategories();
  const { tags: availableTags } = useTags();

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.BUDGET_DETAIL_VIEWED)
  }, [trackEvent])

  // Use generated mutation hooks for budget category operations
  const addCategoryMutation = useAddBudgetCategory();
  const updateCategoryMutation = useUpdateBudgetCategory();
  const deleteCategoryMutation = useDeleteBudgetCategory();

  // Issue #498: Calculate income and allocation metrics
  const incomeMetrics = useMemo(() => {
    if (!budget) return { expectedIncome: 0, actualIncome: 0, unallocated: 0, totalAllocated: 0 };

    // Calculate expected income from manual income sources
    const expectedIncome = (budget.income_sources || [])
      .filter((src) => src.type === 'manual')
      .reduce((sum, src) => sum + parseFloat(src.value || '0'), 0);

    // Use budget.income_amount as actual income (or fall back to expected)
    const actualIncome = budget.income_amount || expectedIncome || budget.amount;

    // Calculate total allocated from categories
    const totalAllocated = (budget.categories || []).reduce(
      (sum, cat) => sum + cat.allocated_amount,
      0
    );

    // Calculate unallocated funds
    const unallocated = actualIncome - totalAllocated;

    return { expectedIncome, actualIncome, unallocated, totalAllocated };
  }, [budget]);

  // Handler for navigating to create savings goal
  const handleCreateSavingsGoal = useCallback(() => {
    navigate('/budgets', { state: { openSavingsGoal: true } });
  }, [navigate]);

  // Handler for adding a new category
  const handleAddCategory = useCallback(
    async (req: CreateBudgetCategoryRequest): Promise<BudgetCategory> => {
      const result = await addCategoryMutation.mutateAsync({
        id: budgetId || '',
        data: req,
      });
      await refetch();
      return result as BudgetCategory;
    },
    [budgetId, refetch, addCategoryMutation]
  );

  // Handler for updating a category
  const handleUpdateCategory = useCallback(
    async (categoryId: string, req: UpdateBudgetCategoryRequest): Promise<BudgetCategory> => {
      const result = await updateCategoryMutation.mutateAsync({
        id: budgetId || '',
        categoryId,
        data: req,
      });
      trackEvent(ANALYTICS_EVENTS.BUDGET_EDITED)
      await refetch();
      return result as BudgetCategory;
    },
    [budgetId, refetch, trackEvent, updateCategoryMutation]
  );

  // Handler for deleting a category
  const handleDeleteCategory = useCallback(
    async (categoryId: string): Promise<void> => {
      await deleteCategoryMutation.mutateAsync({
        id: budgetId || '',
        categoryId,
      });
      await refetch();
    },
    [budgetId, refetch, deleteCategoryMutation]
  );

  // Loading state
  if (loading) {
    return (
      <div className="max-w-[1200px] mx-auto p-6">
        <div className="flex flex-col items-center justify-center min-h-[300px] gap-4 text-muted-foreground">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600" />
          <p>Loading budget...</p>
        </div>
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="max-w-[1200px] mx-auto p-6">
        <div className="flex flex-col items-center justify-center gap-4 p-8 bg-destructive/10 rounded-lg text-destructive">
          <span className="flex items-center justify-center w-10 h-10 rounded-full bg-red-100 dark:bg-red-900/30 text-red-600 dark:text-red-400 font-semibold text-xl">!</span>
          <span>Error: {error.message}</span>
          <button
            type="button"
            onClick={refetch}
            className="px-4 py-2 text-sm font-medium text-white bg-red-600 hover:bg-red-700 border-none rounded-md cursor-pointer transition-colors"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  // Not found state
  if (!budget) {
    return (
      <div className="max-w-[1200px] mx-auto p-6">
        <div className="flex flex-col items-center justify-center gap-4 p-8 text-muted-foreground">
          <p>Budget not found</p>
          <Link to="/budgets" className="inline-flex items-center gap-2 mb-4 text-sm text-blue-600 dark:text-blue-400 no-underline hover:text-blue-700 dark:hover:text-blue-300 transition-colors">
            <ArrowLeftIcon className="h-5 w-5" />
            Back to Budgets
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-[1200px] mx-auto p-6">
      {/* Header */}
      <header className="mb-8">
        <Link to="/budgets" className="inline-flex items-center gap-2 mb-4 text-sm text-blue-600 dark:text-blue-400 no-underline hover:text-blue-700 dark:hover:text-blue-300 transition-colors">
          <ArrowLeftIcon className="h-5 w-5" />
          Back to Budgets
        </Link>

        <div className="flex items-center gap-3 mb-2">
          <h1 className="m-0 text-3xl font-bold text-foreground">{budget.name}</h1>
          {budget.is_active && (
            <span className="px-3 py-1 text-xs font-semibold rounded-full text-green-700 dark:text-green-300 bg-green-100 dark:bg-green-900">Active</span>
          )}
        </div>

        {budget.description && (
          <p className="m-0 mb-4 text-muted-foreground">{budget.description}</p>
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
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Budget Amount</span>
            <span className="text-sm font-semibold text-foreground" data-ph-mask>
              {formatCurrency(budget.amount, budget.currency_code)}
            </span>
          </div>
          {budget.income_amount > 0 && (
            <div className="flex flex-col gap-1">
              <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Income</span>
              <span className="text-sm font-semibold text-foreground" data-ph-mask>
                {formatCurrency(budget.income_amount, budget.currency_code)}
              </span>
            </div>
          )}
        </div>

        {/* Issue #39: Quick-create automation rules */}
        <div className="flex flex-wrap gap-3 mt-4">
          <Link
            to={`/rules/new?type=budget_alert&budget_id=${budgetId}`}
            className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium text-blue-700 dark:text-blue-300 bg-blue-50 dark:bg-blue-900 border border-blue-200 dark:border-blue-700 rounded-lg no-underline hover:bg-blue-100 dark:hover:bg-blue-800 hover:border-blue-300 dark:hover:border-blue-600 transition-all"
            aria-label="Create budget alert rule"
          >
            <BellAlertIcon className="h-4 w-4" aria-hidden="true" />
            Create Alert Rule
          </Link>
          <Link
            to={`/rules/new?type=category_rollover&budget_id=${budgetId}`}
            className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium text-blue-700 dark:text-blue-300 bg-blue-50 dark:bg-blue-900 border border-blue-200 dark:border-blue-700 rounded-lg no-underline hover:bg-blue-100 dark:hover:bg-blue-800 hover:border-blue-300 dark:hover:border-blue-600 transition-all"
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
          <BudgetIncomeSection
            expectedIncome={incomeMetrics.expectedIncome}
            actualIncome={incomeMetrics.actualIncome}
            incomeSources={budget.income_sources || []}
            currencyCode={budget.currency_code}
          />
        </section>
      )}

      {/* Issue #498: Unallocated Funds Card */}
      <section className="mt-6">
        <UnallocatedFundsCard
          unallocatedAmount={incomeMetrics.unallocated}
          totalIncome={incomeMetrics.actualIncome}
          currencyCode={budget.currency_code}
          onCreateSavingsGoal={handleCreateSavingsGoal}
        />
      </section>

      {/* Category Manager */}
      <section className="mt-8">
        <BudgetCategoryManager
          budgetId={budgetId || ''}
          categories={budget.categories || []}
          totalIncome={budget.income_amount || budget.amount}
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
