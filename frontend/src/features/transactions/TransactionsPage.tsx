import { useState, useMemo, useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTransactions } from './hooks/useTransactions'
import { useTransactionFilters } from './hooks/useTransactionFilters'
import { TransactionTable } from './components/TransactionTable'
import { TransactionEditor } from './components/TransactionEditor'
import { TransactionFilterPanel } from './components/TransactionFilterPanel'
import { useGetUserTags, getGetUserTagsQueryKey } from '@/api/generated/endpoints/tags/tags'
import { useGetUserCategories, getGetUserCategoriesQueryKey } from '@/api/generated/endpoints/categories/categories'
import { useGetAccounts } from '@/api/generated/endpoints/accounts/accounts'
import type { Transaction } from './types'
import type { Account } from '../accounts/types'
import type { UserTag } from '../tags/types'
import type { CustomCategory } from '../categories/types'
import './TransactionsPage.css'

/**
 * Transactions Page
 *
 * Displays a list of transactions with comprehensive filtering and pagination.
 *
 * Features:
 * - Multi-filter support (date, account, category, type, amount, status)
 * - URL state sync for shareable/bookmarkable filters
 * - Search transactions with 300ms debounce
 * - Pagination with Next/Previous buttons
 * - Total count display showing filtered vs total
 * - Loading and error states
 * - Empty state when no transactions
 *
 * Issue #529: Transaction page filtering
 */
export function TransactionsPage() {
  const {
    filters,
    datePreset,
    statusFilter,
    activeFilterCount,
    hasActiveFilters,
    setFilter,
    setFilters,
    clearFilter,
    clearAllFilters,
    setDatePreset,
    setCustomDateRange,
    setStatusFilter,
  } = useTransactionFilters()

  const [selectedTransaction, setSelectedTransaction] = useState<Transaction | null>(null)
  const [isEditorOpen, setIsEditorOpen] = useState(false)
  const queryClient = useQueryClient()

  // Fetch available tags, categories, and accounts using generated hooks
  const { data: tagsData } = useGetUserTags()
  const { data: categoriesData } = useGetUserCategories()
  const { data: accountsData } = useGetAccounts()

  const availableTags = (tagsData as unknown as UserTag[]) ?? []
  const availableCategories = (categoriesData as unknown as CustomCategory[]) ?? []

  // Issue #531: Create accounts lookup map for O(1) access
  // Use accountsData?.accounts directly to avoid unstable reference in deps
  const accountsMap = useMemo(() => {
    const map: Record<string, Account> = {}
    const accs = accountsData?.accounts ?? []
    accs.forEach((acc) => {
      map[acc.id] = acc
    })
    return map
  }, [accountsData?.accounts])

  const {
    data,
    loading,
    error,
    nextPage,
    previousPage,
    refetch,
    updateTransaction,
    hasMore,
    totalCount,
    currentPage,
  } = useTransactions({ filters, limit: 50 })

  // Handle opening the transaction editor
  const handleEditTransaction = (transaction: Transaction) => {
    setSelectedTransaction(transaction)
    setIsEditorOpen(true)
  }

  // Handle closing the editor
  const handleEditorClose = () => {
    setIsEditorOpen(false)
    setSelectedTransaction(null)
  }

  // Handle successful save - update transaction in place for immediate display
  const handleEditorSave = (updatedTransaction: Transaction) => {
    updateTransaction(updatedTransaction)
    handleEditorClose()
  }

  // Handle tag creation - invalidate query to refetch tags
  const handleTagCreated = useCallback(
    (_tag: UserTag) => {
      queryClient.invalidateQueries({ queryKey: getGetUserTagsQueryKey() })
    },
    [queryClient]
  )

  // Handle category creation - invalidate query to refetch categories
  const handleCategoryCreated = useCallback(
    (_category: CustomCategory) => {
      queryClient.invalidateQueries({ queryKey: getGetUserCategoriesQueryKey() })
    },
    [queryClient]
  )

  // Issue #531: Handle categorize quick action (opens editor focused on category)
  const handleCategorizeTransaction = (transaction: Transaction) => {
    setSelectedTransaction(transaction)
    setIsEditorOpen(true)
  }

  // Handle search input change
  function handleSearchChange(event: React.ChangeEvent<HTMLInputElement>) {
    const searchValue = event.target.value
    setFilter('search', searchValue || undefined) // Remove if empty
  }

  // Calculate pending count from transactions data
  const pendingCount = data?.transactions?.filter((t) => t.pending).length ?? 0

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

      {/* Filter Panel - Issue #529 */}
      <TransactionFilterPanel
        filters={filters}
        datePreset={datePreset}
        statusFilter={statusFilter}
        activeFilterCount={activeFilterCount}
        hasActiveFilters={hasActiveFilters}
        pendingCount={pendingCount}
        onFilterChange={setFilter}
        onFiltersChange={setFilters}
        onDatePresetChange={setDatePreset}
        onCustomDateChange={setCustomDateRange}
        onStatusFilterChange={setStatusFilter}
        onClearFilter={clearFilter}
        onClearAllFilters={clearAllFilters}
        disabled={loading}
      />

      {isEmpty ? (
        <div className="empty-state">
          <p className="empty-state__message">
            No transactions found.
            {hasActiveFilters && ' Try adjusting your filters.'}
            {filters.search && !hasActiveFilters && ' Try adjusting your search.'}
          </p>
          {hasActiveFilters && (
            <button
              onClick={clearAllFilters}
              className="btn-clear-filters"
            >
              Clear all filters
            </button>
          )}
        </div>
      ) : (
        <>
          <div className="transactions-info">
            <p className="transactions-count">
              {hasActiveFilters ? (
                <>
                  Showing {transactions.length} of {totalCount}{' '}
                  {totalCount === 1 ? 'transaction' : 'transactions'}
                </>
              ) : (
                <>
                  {totalCount} {totalCount === 1 ? 'transaction' : 'transactions'} total
                </>
              )}
            </p>
          </div>

          {/* Issue #584: Transaction table view */}
          <div className="transactions-table-container">
            <TransactionTable
              transactions={transactions}
              accountsMap={accountsMap}
              onEdit={handleEditTransaction}
              onCategorize={handleCategorizeTransaction}
              loading={loading}
            />
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

      {selectedTransaction && (
        <TransactionEditor
          transaction={selectedTransaction}
          availableTags={availableTags}
          availableCategories={availableCategories}
          isOpen={isEditorOpen}
          onClose={handleEditorClose}
          onSave={handleEditorSave}
          onTagCreated={handleTagCreated}
          onCategoryCreated={handleCategoryCreated}
        />
      )}
    </main>
  )
}
