/**
 * BudgetTransactionList Component
 * Issue #499: Transaction-Budget Linking UI
 *
 * Displays a paginated list of transactions matching a budget category.
 */

import { useBudgetTransactions } from '../../hooks/useBudgetTransactions';
import { TransactionCard } from '../../../transactions/components/TransactionCard';
import './BudgetTransactionList.css';

interface BudgetTransactionListProps {
  budgetId: string;
  categoryId: string;
}

/**
 * Displays transactions matching a budget category with pagination.
 */
export function BudgetTransactionList({ budgetId, categoryId }: BudgetTransactionListProps) {
  const {
    transactions,
    totalCount,
    totalAmount,
    loading,
    loadingMore,
    error,
    hasMore,
    loadMore,
    refetch,
  } = useBudgetTransactions(budgetId, categoryId);

  // Format currency
  const formatCurrency = (amount: number) => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
    }).format(amount);
  };

  // Loading state
  if (loading) {
    return (
      <div className="budget-transaction-list budget-transaction-list--loading" role="status">
        <span className="budget-transaction-list__spinner" aria-hidden="true" />
        <span>Loading transactions...</span>
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="budget-transaction-list budget-transaction-list--error">
        <p className="budget-transaction-list__error-message">
          {error.message || 'Failed to load transactions'}
        </p>
        <button
          className="budget-transaction-list__retry-button"
          onClick={refetch}
          type="button"
        >
          Retry
        </button>
      </div>
    );
  }

  // Empty state
  if (transactions.length === 0) {
    return (
      <div className="budget-transaction-list budget-transaction-list--empty">
        <p>No transactions found for this category.</p>
      </div>
    );
  }

  return (
    <div className="budget-transaction-list">
      {/* Header */}
      <div className="budget-transaction-list__header">
        <h3 className="budget-transaction-list__title">
          Transactions ({totalCount})
        </h3>
        <span className="budget-transaction-list__total">
          Total: {formatCurrency(totalAmount)}
        </span>
      </div>

      {/* Transaction list */}
      <ul className="budget-transaction-list__items" role="list">
        {transactions.map((transaction) => (
          <li key={transaction.id} className="budget-transaction-list__item">
            <TransactionCard transaction={transaction} />
          </li>
        ))}
      </ul>

      {/* Load more button */}
      {hasMore && (
        <div className="budget-transaction-list__footer">
          <button
            className="budget-transaction-list__load-more"
            onClick={loadMore}
            disabled={loadingMore}
            type="button"
          >
            {loadingMore ? 'Loading...' : 'Load More'}
          </button>
        </div>
      )}
    </div>
  );
}
