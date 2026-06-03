/**
 * BudgetTransactionList Component
 * Issue #499: Transaction-Budget Linking UI
 *
 * Displays a paginated list of transactions matching a budget category.
 */

import { useBudgetTransactions } from '../../hooks/useBudgetTransactions';
import { TransactionCard } from '../../../transactions/components/TransactionCard';

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
      <div className="flex items-center justify-center gap-2 p-8 text-muted-foreground" role="status">
        <span
          className="w-4 h-4 rounded-full border-2 border-border border-t-primary animate-spin"
          aria-hidden="true"
        />
        <span>Loading transactions...</span>
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="p-6 text-center">
        <p className="m-0 mb-4 text-destructive">
          {error.message || 'Failed to load transactions'}
        </p>
        <button
          className="px-4 py-2 text-sm font-medium text-destructive bg-transparent border border-destructive rounded-md cursor-pointer transition-colors duration-150 hover:bg-destructive hover:text-background"
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
      <div className="p-8 text-center text-muted-foreground">
        <p>No transactions found for this category.</p>
      </div>
    );
  }

  return (
    <div className="w-full">
      {/* Header */}
      <div className="flex justify-between items-center py-3 border-b border-border mb-2">
        <h3 className="m-0 text-sm font-semibold text-foreground">
          Transactions ({totalCount})
        </h3>
        <span className="text-sm font-medium text-muted-foreground">
          Total: {formatCurrency(totalAmount)}
        </span>
      </div>

      {/* Transaction list */}
      <ul className="list-none m-0 p-0" role="list">
        {transactions.map((transaction) => (
          <li key={transaction.id} className="border-b border-border last:border-b-0">
            <TransactionCard transaction={transaction} />
          </li>
        ))}
      </ul>

      {/* Load more button */}
      {hasMore && (
        <div className="flex justify-center py-4">
          <button
            className="px-6 py-2 text-sm font-medium text-primary bg-transparent border border-primary rounded-md cursor-pointer transition-colors duration-150 hover:bg-primary hover:text-primary-foreground disabled:opacity-60 disabled:cursor-not-allowed"
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
