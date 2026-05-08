import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { useTransactions } from './hooks/useTransactions'
import { useTransactionFilters } from './hooks/useTransactionFilters'
import { TransactionTable, type TransactionSortColumn, type SortDirection } from './components/TransactionTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { TransactionEditor } from './components/TransactionEditor'
import { TransactionFilterPanel } from './components/TransactionFilterPanel'
import { useGetUserTags, getGetUserTagsQueryKey } from '@boostedbudget/api-client/endpoints/tags/tags'
import { useGetUserCategories, getGetUserCategoriesQueryKey, useGetPlaidPrimaryCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import { useActiveAccounts } from '@/features/accounts/hooks'
import type { Transaction } from './types'
import type { Account } from '../accounts/types'
import type { UserTag } from '../tags/types'
import type { CustomCategory } from '../categories/types'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { formatPFCPrimary } from '@/shared/categories/formatPfc'

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
  const trackEvent = useTrackEvent()
  const searchDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.TRANSACTIONS_VIEWED)
  }, [trackEvent])

  // Issue #590: Sorting state
  const [sortColumn, setSortColumn] = useState<TransactionSortColumn | undefined>(undefined)
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  // Fetch available tags, categories, accounts, and curated PFC primaries
  const { data: tagsData } = useGetUserTags()
  const { data: categoriesData } = useGetUserCategories()
  const { accounts: accountsList } = useActiveAccounts()
  const { data: plaidPrimariesData } = useGetPlaidPrimaryCategories()

  const availableTags = (tagsData as unknown as UserTag[]) ?? []
  const availableCategories = (categoriesData as unknown as CustomCategory[]) ?? []
  const plaidPrimaries = useMemo(() => (plaidPrimariesData as unknown as Primary[]) ?? [], [plaidPrimariesData])

  // Issue #531: Create accounts lookup map for O(1) access
  // Issue #1109: Use useActiveAccounts to exclude replaced accounts
  const accountsMap = useMemo(() => {
    const map: Record<string, Account> = {}
    const accs = accountsList ?? []
    accs.forEach((acc) => {
      map[acc.id] = acc
    })
    return map
  }, [accountsList])

  // Issue #590: Create account filter options for inline column filter
  const accountOptions = useMemo<FilterOption[]>(() => {
    const accs = accountsList ?? []
    return accs.map((acc) => ({
      value: acc.id,
      label: acc.nickname || acc.name,
    }))
  }, [accountsList])

  // Issue #590: Inline filter callbacks
  const handleAccountFilterChange = useCallback(
    (value: string[]) => {
      setFilter('account_ids', value.length > 0 ? value : undefined)
    },
    [setFilter]
  )

  const handleCategoryFilterChange = useCallback(
    (value: string[]) => {
      setFilter('pfc_primaries', value.length > 0 ? value : undefined)
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

  // Category filter options: curated PFC primaries + observed effective primaries.
  // Values are raw enum strings (e.g. "FOOD_AND_DRINK") so they match the pfc_primary
  // column; labels are human-readable display strings.
  const categoryOptions = useMemo<FilterOption[]>(() => {
    const enumToDisplay = new Map<string, string>()

    // Seed with curated PFC primaries (enum → display)
    plaidPrimaries.forEach(p => enumToDisplay.set(p.enum, p.display))

    // Add any observed effective primaries not already in the curated list
    const transactions = data?.transactions ?? []
    transactions.forEach((t) => {
      const effectivePrimary = t.pfc_primary_override ?? t.pfc_primary
      if (effectivePrimary && !enumToDisplay.has(effectivePrimary)) {
        enumToDisplay.set(effectivePrimary, formatPFCPrimary(effectivePrimary))
      }
    })

    return Array.from(enumToDisplay.entries())
      .sort(([, a], [, b]) => a.localeCompare(b))
      .map(([value, label]) => ({ value, label }))
  }, [data?.transactions, plaidPrimaries])

  // Handle opening the transaction editor
  const handleEditTransaction = (transaction: Transaction) => {
    setSelectedTransaction(transaction)
    setIsEditorOpen(true)
  }

  // Handle closing the editor
  const handleEditorClose = useCallback(() => {
    setIsEditorOpen(false)
    setSelectedTransaction(null)
  }, [])

  // Handle successful save - update transaction in place for immediate display
  const handleEditorSave = useCallback((updatedTransaction: Transaction) => {
    updateTransaction(updatedTransaction)
    handleEditorClose()
  }, [updateTransaction, handleEditorClose])

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

    if (searchDebounceRef.current) clearTimeout(searchDebounceRef.current)
    if (searchValue) {
      searchDebounceRef.current = setTimeout(() => {
        trackEvent(ANALYTICS_EVENTS.TRANSACTION_SEARCHED, { query: searchValue })
      }, 500)
    }
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
          const resolveCat = (t: Transaction) =>
            t.pfc_primary_override ?? t.pfc_primary ?? ''
          comparison = resolveCat(a).localeCompare(resolveCat(b))
          break
        }
      }

      return sortDirection === 'asc' ? comparison : -comparison
    })
  }, [data?.transactions, sortColumn, sortDirection])

  // Loading state
  if (loading && !data) {
    return (
      <main className="p-8 max-w-[1200px] mx-auto">
        <div className="text-center py-12 px-4">Loading your transactions...</div>
      </main>
    )
  }

  // Error state
  if (error) {
    return (
      <main className="p-8 max-w-[1200px] mx-auto">
        <div className="text-center py-12 px-4">
          <p className="text-destructive mb-4 text-lg">Failed to load transactions. Please try again.</p>
          <button
            onClick={refetch}
            className="px-6 py-3 text-base bg-primary text-primary-foreground border-none rounded cursor-pointer hover:opacity-90 transition-opacity"
          >
            Retry
          </button>
        </div>
      </main>
    )
  }

  const transactions = sortedTransactions
  const isEmpty = transactions.length === 0

  return (
    <main className="p-8 max-w-[1200px] mx-auto max-sm:p-4">
      <div className="flex justify-between items-center gap-4 mb-8 max-sm:flex-col max-sm:items-start">
        <h1 className="text-foreground text-3xl font-semibold m-0">Transactions</h1>
        <input
          type="search"
          placeholder="Search transactions..."
          value={filters.search ?? ''}
          onChange={handleSearchChange}
          className="px-4 py-3 text-base border border-input rounded-md min-w-[300px] bg-background text-foreground transition-colors focus:outline-none focus:border-ring focus:ring-2 focus:ring-ring/10 max-sm:w-full max-sm:min-w-0"
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
        <EmptyState
          icon={<Search />}
          title="No transactions found"
          description={
            hasActiveFilters
              ? 'Try adjusting your filters.'
              : filters.search
                ? 'Try adjusting your search.'
                : undefined
          }
          action={
            hasActiveFilters ? (
              <Button variant="outline" size="sm" onClick={clearAllFilters}>
                Clear all filters
              </Button>
            ) : undefined
          }
          className="mt-8"
        />
      ) : (
        <>
          <div className="mb-4">
            <p className="text-base text-muted-foreground m-0">
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
          <div className="min-h-[200px] overflow-x-auto">
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
              categoryFilter={filters.pfc_primaries}
              onCategoryFilterChange={handleCategoryFilterChange}
              amountRange={currentAmountRange}
              onAmountRangeChange={handleAmountRangeChange}
            />
          </div>

          {(hasMore || currentPage > 0) && (
            <div className="flex justify-center gap-4 mt-8 pt-8 border-t border-border max-sm:flex-col">
              {currentPage > 0 && (
                <button
                  onClick={previousPage}
                  className="px-6 py-3 text-base bg-primary text-primary-foreground border-none rounded cursor-pointer hover:opacity-90 transition-opacity disabled:bg-muted disabled:text-muted-foreground disabled:cursor-not-allowed max-sm:w-full"
                  disabled={loading}
                >
                  Previous
                </button>
              )}
              {hasMore && (
                <button
                  onClick={nextPage}
                  className="px-6 py-3 text-base bg-primary text-primary-foreground border-none rounded cursor-pointer hover:opacity-90 transition-opacity disabled:bg-muted disabled:text-muted-foreground disabled:cursor-not-allowed max-sm:w-full"
                  disabled={loading}
                >
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
          plaidPrimaries={plaidPrimaries}
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
