/**
 * BudgetTransactionList Component
 * Issue #499: Transaction-Budget Linking UI
 *
 * Displays a paginated list of transactions matching a budget category.
 */

import { Receipt } from 'lucide-react';
import { formatCurrencyFull } from '@/shared/charts/formatters';
import { useBudgetTransactions } from '../../hooks/useBudgetTransactions';
import { TransactionCard } from '../../../transactions/components/TransactionCard';
import { EmptyState } from '@/components/ui/empty-state';
import { Button } from '@/components/ui/button';
import { useGetUserCategories } from '@boostedbudget/api-client/endpoints/categories/categories';

interface BudgetTransactionListProps {
  budgetId: string;
  categoryId: string;
}

/**
 * Displays transactions matching a budget category with pagination.
 */
export function BudgetTransactionList({ budgetId, categoryId }: BudgetTransactionListProps) {
  const { data: customCategories } = useGetUserCategories();
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
        <Button variant="destructive" onClick={refetch}>
          Retry
        </Button>
      </div>
    );
  }

  // Empty state
  if (transactions.length === 0) {
    return (
      <EmptyState
        size="compact"
        icon={<Receipt />}
        title="No transactions found for this category."
      />
    );
  }

  return (
    <div className="w-full">
      {/* Header */}
      <div className="flex justify-between items-center py-3 border-b border-border mb-2">
        <h3 className="m-0 text-sm font-semibold text-foreground">
          Transactions ({totalCount})
        </h3>
        <span className="text-sm font-medium text-muted-foreground" data-ph-mask>
          Total: {formatCurrencyFull(totalAmount)}
        </span>
      </div>

      {/* Transaction list */}
      <ul className="list-none m-0 p-0" role="list">
        {transactions.map((transaction) => (
          <li key={transaction.id} className="border-b border-border last:border-b-0">
            <TransactionCard transaction={transaction} customCategories={Array.isArray(customCategories) ? customCategories : undefined} />
          </li>
        ))}
      </ul>

      {/* Load more button */}
      {hasMore && (
        <div className="flex justify-center py-4">
          <Button variant="outline" onClick={loadMore} disabled={loadingMore}>
            {loadingMore ? 'Loading...' : 'Load More'}
          </Button>
        </div>
      )}
    </div>
  );
}
