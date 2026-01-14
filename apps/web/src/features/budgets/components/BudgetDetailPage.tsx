/**
 * BudgetDetailPage
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #498: Budget Detail Page for Envelope Budgeting
 *
 * Page component for viewing and managing a single budget's details and categories.
 */

import { useCallback, useMemo } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
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
import './BudgetDetailPage.css';

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
  const { budgetId } = useParams<{ budgetId: string }>();
  const navigate = useNavigate();
  const { budget, loading, error, refetch } = useBudgetDetail(budgetId || '');
  const { categories: availableCategories } = useAvailableCategories();
  const { tags: availableTags } = useTags();

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
      await refetch();
      return result as BudgetCategory;
    },
    [budgetId, refetch, updateCategoryMutation]
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
      <div className="budget-detail-page">
        <div className="budget-detail-page__loading">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600" />
          <p>Loading budget...</p>
        </div>
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="budget-detail-page">
        <div className="budget-detail-page__error">
          <span className="budget-detail-page__error-icon">!</span>
          <span>Error: {error.message}</span>
          <button
            type="button"
            onClick={refetch}
            className="budget-detail-page__retry-btn"
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
      <div className="budget-detail-page">
        <div className="budget-detail-page__not-found">
          <p>Budget not found</p>
          <Link to="/budgets" className="budget-detail-page__back-link">
            <ArrowLeftIcon className="h-5 w-5" />
            Back to Budgets
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="budget-detail-page">
      {/* Header */}
      <header className="budget-detail-page__header">
        <Link to="/budgets" className="budget-detail-page__back-link">
          <ArrowLeftIcon className="h-5 w-5" />
          Back to Budgets
        </Link>

        <div className="budget-detail-page__title-row">
          <h1 className="budget-detail-page__title">{budget.name}</h1>
          {budget.is_active && (
            <span className="budget-detail-page__active-badge">Active</span>
          )}
        </div>

        {budget.description && (
          <p className="budget-detail-page__description">{budget.description}</p>
        )}

        <div className="budget-detail-page__meta">
          <div className="budget-detail-page__meta-item">
            <span className="budget-detail-page__meta-label">Period</span>
            <span className="budget-detail-page__meta-value">
              {capitalize(budget.period)}
            </span>
          </div>
          <div className="budget-detail-page__meta-item">
            <span className="budget-detail-page__meta-label">Dates</span>
            <span className="budget-detail-page__meta-value">
              {formatDate(budget.start_date)}{budget.end_date ? ` - ${formatDate(budget.end_date)}` : ''}
            </span>
          </div>
          <div className="budget-detail-page__meta-item">
            <span className="budget-detail-page__meta-label">Budget Amount</span>
            <span className="budget-detail-page__meta-value">
              {formatCurrency(budget.amount, budget.currency_code)}
            </span>
          </div>
          {budget.income_amount > 0 && (
            <div className="budget-detail-page__meta-item">
              <span className="budget-detail-page__meta-label">Income</span>
              <span className="budget-detail-page__meta-value">
                {formatCurrency(budget.income_amount, budget.currency_code)}
              </span>
            </div>
          )}
        </div>

        {/* Issue #39: Quick-create automation rules */}
        <div className="budget-detail-page__actions">
          <Link
            to={`/rules/new?type=budget_alert&budget_id=${budgetId}`}
            className="budget-detail-page__action-btn"
            aria-label="Create budget alert rule"
          >
            <BellAlertIcon className="h-4 w-4" aria-hidden="true" />
            Create Alert Rule
          </Link>
          <Link
            to={`/rules/new?type=category_rollover&budget_id=${budgetId}`}
            className="budget-detail-page__action-btn"
            aria-label="Create category rollover rule"
          >
            <ArrowPathIcon className="h-4 w-4" aria-hidden="true" />
            Create Rollover Rule
          </Link>
        </div>
      </header>

      {/* Issue #498: Income Section - shows expected vs actual income */}
      {(budget.income_sources?.length > 0 || incomeMetrics.expectedIncome > 0) && (
        <section className="budget-detail-page__income-section">
          <BudgetIncomeSection
            expectedIncome={incomeMetrics.expectedIncome}
            actualIncome={incomeMetrics.actualIncome}
            incomeSources={budget.income_sources || []}
            currencyCode={budget.currency_code}
          />
        </section>
      )}

      {/* Issue #498: Unallocated Funds Card */}
      <section className="budget-detail-page__unallocated-section">
        <UnallocatedFundsCard
          unallocatedAmount={incomeMetrics.unallocated}
          totalIncome={incomeMetrics.actualIncome}
          currencyCode={budget.currency_code}
          onCreateSavingsGoal={handleCreateSavingsGoal}
        />
      </section>

      {/* Category Manager */}
      <section className="budget-detail-page__content">
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
