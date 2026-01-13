import { useState, useMemo, useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTransactions } from './hooks/useTransactions'
import { useTransactionFilters } from './hooks/useTransactionFilters'
import { TransactionTable, type TransactionSortColumn, type SortDirection } from './components/TransactionTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { TransactionEditor } from './components/TransactionEditor'
import { TransactionFilterPanel } from './components/TransactionFilterPanel'
import { useGetUserTags, getGetUserTagsQueryKey } from '@boostedbudget/api-client/endpoints/tags/tags'
import { useGetUserCategories, getGetUserCategoriesQueryKey } from '@boostedbudget/api-client/endpoints/categories/categories'
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts'
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

  // Issue #590: Sorting state
  const [sortColumn, setSortColumn] = useState<TransactionSortColumn | undefined>(undefined)
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

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

  // Issue #590: Create account filter options for inline column filter
  const accountOptions = useMemo<FilterOption[]>(() => {
    const accs = accountsData?.accounts ?? []
    return accs.map((acc) => ({
      value: acc.id,
      label: acc.nickname || acc.name,
    }))
  }, [accountsData?.accounts])

  // Issue #590: Inline filter callbacks
  const handleAccountFilterChange = useCallback(
    (value: string[]) => {
      setFilter('account_ids', value.length > 0 ? value : undefined)
    },
    [setFilter]
  )

  const handleCategoryFilterChange = useCallback(
    (value: string[]) => {
      setFilter('categories', value.length > 0 ? value : undefined)
    },
    [setFilter]
  )

  const handleAmountRangeChange = useCallback(
    (value: RangeValue | null) => {
      if (value && (value.min !== undefined || value.max !== undefined)) {
        setFilters({
          ...filters,
          min_amount: value.min,
          max_amount: value.max,
        })
      } else {
        setFilters({
          ...filters,
          min_amount: undefined,
          max_amount: undefined,
        })
      }
    },
    [filters, setFilters]
  )

  // Issue #590: Get current amount range from filters
  const currentAmountRange = useMemo<RangeValue | null>(() => {
    if (filters.min_amount !== undefined || filters.max_amount !== undefined) {
      return {
        min: filters.min_amount,
        max: filters.max_amount,
      }
    }
    return null
  }, [filters.min_amount, filters.max_amount])

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

  // Issue #590: Create category filter options from transaction data
  const categoryOptions = useMemo<FilterOption[]>(() => {
    const transactions = data?.transactions ?? []
    const categorySet = new Set<string>()

    transactions.forEach((t) => {
      const category = t.category_override || t.category
      if (category) {
        try {
          const parsed = JSON.parse(category)
          if (Array.isArray(parsed) && parsed.length > 0) {
            categorySet.add(parsed[0])
          }
        } catch {
          // If not JSON, use as-is
          categorySet.add(category)
        }
      }
    })

    return Array.from(categorySet)
      .sort()
      .map((cat) => ({
        value: cat,
        label: cat,
      }))
  }, [data?.transactions])

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

  // Issue #590: Handle sort change
  const handleSort = useCallback((column: TransactionSortColumn, direction: SortDirection) => {
    setSortColumn(column)
    setSortDirection(direction)
  }, [])

  // Issue #590: Sort transactions client-side
  const sortedTransactions = useMemo(() => {
    const transactions = data?.transactions ?? []
    if (!sortColumn) return transactions

    return [...transactions].sort((a, b) => {
      let comparison = 0

      switch (sortColumn) {
        case 'date': {
          const dateA = new Date(a.authorized_date || a.date).getTime()
          const dateB = new Date(b.authorized_date || b.date).getTime()
          comparison = dateA - dateB
          break
        }
        case 'merchant': {
          const nameA = (a.merchant_name || a.name || '').toLowerCase()
          const nameB = (b.merchant_name || b.name || '').toLowerCase()
          comparison = nameA.localeCompare(nameB)
          break
        }
        case 'amount': {
          comparison = a.amount - b.amount
          break
        }
        case 'category': {
          const catA = a.category_override || a.category || ''
          const catB = b.category_override || b.category || ''
          comparison = catA.localeCompare(catB)
          break
        }
      }

      return sortDirection === 'asc' ? comparison : -comparison
    })
  }, [data?.transactions, sortColumn, sortDirection])

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

  const transactions = sortedTransactions
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
              sortable
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              // Issue #590: Inline column filters
              accountOptions={accountOptions}
              accountFilter={filters.account_ids}
              onAccountFilterChange={handleAccountFilterChange}
              categoryOptions={categoryOptions}
              categoryFilter={filters.categories}
              onCategoryFilterChange={handleCategoryFilterChange}
              amountRange={currentAmountRange}
              onAmountRangeChange={handleAmountRangeChange}
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
