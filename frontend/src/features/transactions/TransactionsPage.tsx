import { useState } from 'react'
import { useTransactions } from './hooks/useTransactions'
import { TransactionCard } from './components/TransactionCard'
import type { TransactionFilters } from './types'
import './TransactionsPage.css'

/**
 * Transactions Page
 *
 * Displays a list of transactions with search and pagination.
 *
 * Features:
 * - Search transactions with 300ms debounce
 * - Pagination with Next/Previous buttons
 * - Total count display
 * - Loading and error states
 * - Empty state when no transactions
 */
export function TransactionsPage() {
  const [filters, setFilters] = useState<TransactionFilters>({})

  const {
    data,
    loading,
    error,
    nextPage,
    previousPage,
    refetch,
    hasMore,
    totalCount,
    currentPage,
  } = useTransactions({ filters, limit: 50 })

  // Handle search input change
  function handleSearchChange(event: React.ChangeEvent<HTMLInputElement>) {
    const searchValue = event.target.value
    setFilters((prev) => ({
      ...prev,
      search: searchValue || undefined, // Remove if empty
    }))
  }

  // Loading state
  if (loading && !data) {
    return (
      <main className="transactions-page">
        <div className="loading">Loading your transactions...</div>
      </main>
    )
  }

  // Error state
  if (error) {
    return (
      <main className="transactions-page">
        <div className="error-state">
          <p className="error-message">Failed to load transactions. Please try again.</p>
          <button onClick={refetch} className="btn-retry">
            Retry
          </button>
        </div>
      </main>
    )
  }

  const transactions = data?.transactions ?? []
  const isEmpty = transactions.length === 0

  return (
    <main className="transactions-page">
      <div className="transactions-header">
        <h1>Transactions</h1>
        <input
          type="search"
          placeholder="Search transactions..."
          value={filters.search ?? ''}
          onChange={handleSearchChange}
          className="search-input"
          aria-label="Search transactions"
        />
      </div>

      {isEmpty ? (
        <div className="empty-state">
          <p className="empty-state__message">
            No transactions found.
            {filters.search && ' Try adjusting your search.'}
          </p>
        </div>
      ) : (
        <>
          <div className="transactions-info">
            <p className="transactions-count">
              {totalCount} {totalCount === 1 ? 'transaction' : 'transactions'} total
            </p>
          </div>

          <div className="transactions-list">
            {transactions.map((transaction) => (
              <TransactionCard key={transaction.id} transaction={transaction} />
            ))}
          </div>

          {(hasMore || currentPage > 0) && (
            <div className="pagination-controls">
              {currentPage > 0 && (
                <button onClick={previousPage} className="btn-pagination" disabled={loading}>
                  Previous
                </button>
              )}
              {hasMore && (
                <button onClick={nextPage} className="btn-pagination" disabled={loading}>
                  Next
                </button>
              )}
            </div>
          )}
        </>
      )}
    </main>
  )
}
