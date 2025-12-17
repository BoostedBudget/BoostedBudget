/**
 * BudgetDetailPage
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Page component for viewing and managing a single budget's details and categories.
 */

import { useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import { ArrowLeftIcon } from '@heroicons/react/24/outline';
import { useBudgetDetail } from '../hooks/useBudgetDetail';
import { useAvailableCategories } from '../hooks/useAvailableCategories';
import { useTags } from '../../tags/hooks/useTags';
import {
  addBudgetCategory,
  updateBudgetCategory,
  deleteBudgetCategory,
} from '../api/budgetsApi';
import type {
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
  BudgetCategory,
} from '../types';
import { BudgetCategoryManager } from './category-management/BudgetCategoryManager';
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
  const { budget, loading, error, refetch } = useBudgetDetail(budgetId || '');
  const { categories: availableCategories } = useAvailableCategories();
  const { tags: availableTags } = useTags();

  // Handler for adding a new category
  const handleAddCategory = useCallback(
    async (req: CreateBudgetCategoryRequest): Promise<BudgetCategory> => {
      const result = await addBudgetCategory(budgetId || '', req);
      await refetch();
      return result;
    },
    [budgetId, refetch]
  );

  // Handler for updating a category
  const handleUpdateCategory = useCallback(
    async (categoryId: string, req: UpdateBudgetCategoryRequest): Promise<BudgetCategory> => {
      const result = await updateBudgetCategory(budgetId || '', categoryId, req);
      await refetch();
      return result;
    },
    [budgetId, refetch]
  );

  // Handler for deleting a category
  const handleDeleteCategory = useCallback(
    async (categoryId: string): Promise<void> => {
      await deleteBudgetCategory(budgetId || '', categoryId);
      await refetch();
    },
    [budgetId, refetch]
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
      </header>

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
