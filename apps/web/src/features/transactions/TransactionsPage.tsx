import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import { useQueryClient } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { useInfiniteTransactions } from './hooks/useInfiniteTransactions'
import { useTransactionFilters } from './hooks/useTransactionFilters'
import { TransactionTable } from './components/TransactionTable'
import type { TransactionSortColumn, SortDirection } from './components/TransactionTable'
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

export function TransactionsPage() {
  const {
    filters,
    sort,
    datePreset,
    statusFilter,
    activeFilterCount,
    hasActiveFilters,
    setFilter,
    setFilters,
    clearFilter,
    clearAllFilters,
    setSort,
    setDatePreset,
    setCustomDateRange,
    setStatusFilter,
  } = useTransactionFilters()

  const sortColumn = (sort?.columnId ?? 'date') as TransactionSortColumn
  const sortDirection = (sort?.direction ?? 'desc') as SortDirection

  const [selectedTransaction, setSelectedTransaction] = useState<Transaction | null>(null)
  const [isEditorOpen, setIsEditorOpen] = useState(false)
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()
  const searchDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.TRANSACTIONS_VIEWED)
  }, [trackEvent])

  const { data: tagsData } = useGetUserTags()
  const { data: categoriesData } = useGetUserCategories()
  const { accounts: accountsList } = useActiveAccounts()
  const { data: plaidPrimariesData } = useGetPlaidPrimaryCategories()

  const availableTags = (tagsData as unknown as UserTag[]) ?? []
  const availableCategories = (categoriesData as unknown as CustomCategory[]) ?? []
  const plaidPrimaries = useMemo(() => (plaidPrimariesData as unknown as Primary[]) ?? [], [plaidPrimariesData])

  // Create accounts lookup map
  const accountsMap = useMemo(() => {
    const map: Record<string, Account> = {}
    const accs = accountsList ?? []
    accs.forEach((acc) => {
      map[acc.id] = acc
    })
    return map
  }, [accountsList])

  // Create account filter options
  const accountOptions = useMemo<FilterOption[]>(() => {
    const accs = accountsList ?? []
    return accs.map((acc) => ({
      value: acc.id,
      label: acc.nickname || acc.name,
    }))
  }, [accountsList])

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
    transactions: infiniteTransactions,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isLoading,
    isError,
    updateTransaction,
  } = useInfiniteTransactions({ filters, limit: 50, sortBy: sortColumn, sortDir: sortDirection })

  // Track successful infinite scroll load-more
  const prevFetchingRef = useRef(false)
  useEffect(() => {
    if (prevFetchingRef.current && !isFetchingNextPage && !isError) {
      trackEvent(ANALYTICS_EVENTS.TRANSACTIONS_PAGE_LOADED_MORE)
    }
    prevFetchingRef.current = isFetchingNextPage
  }, [isFetchingNextPage, isError, trackEvent])

  // Show error toast when fetchNextPage fails
  useEffect(() => {
    if (isError) {
      toast.error('Failed to load more transactions.', {
        action: {
          label: 'Retry',
          onClick: () => fetchNextPage(),
        },
      })
    }
  }, [isError, fetchNextPage])

  const categoryOptions = useMemo<FilterOption[]>(() => {
    const enumToDisplay = new Map<string, string>()

    // Seed with curated PFC primaries (enum → display)
    plaidPrimaries.forEach(p => enumToDisplay.set(p.enum, p.display))

    // Add any observed effective primaries not already in the curated list
    const txns = infiniteTransactions
    txns.forEach((t) => {
      const effectivePrimary = t.pfc_primary_override ?? t.pfc_primary
      if (effectivePrimary && !enumToDisplay.has(effectivePrimary)) {
        enumToDisplay.set(effectivePrimary, formatPFCPrimary(effectivePrimary))
      }
    })

    return Array.from(enumToDisplay.entries())
      .sort(([, a], [, b]) => a.localeCompare(b))
      .map(([value, label]) => ({ value, label }))
  }, [infiniteTransactions, plaidPrimaries])

  const handleEditTransaction = (transaction: Transaction) => {
    setSelectedTransaction(transaction)
    setIsEditorOpen(true)
  }

  const handleEditorClose = useCallback(() => {
    setIsEditorOpen(false)
    setSelectedTransaction(null)
  }, [])

  const handleEditorSave = useCallback((updatedTransaction: Transaction) => {
    updateTransaction(updatedTransaction as unknown as import('@boostedbudget/api-client/models/transaction').Transaction)
    handleEditorClose()
  }, [updateTransaction, handleEditorClose])

  const handleTagCreated = useCallback(
    (_tag: UserTag) => {
      queryClient.invalidateQueries({ queryKey: getGetUserTagsQueryKey() })
    },
    [queryClient]
  )

  const handleCategoryCreated = useCallback(
    (_category: CustomCategory) => {
      queryClient.invalidateQueries({ queryKey: getGetUserCategoriesQueryKey() })
    },
    [queryClient]
  )

  const handleCategorizeTransaction = (transaction: Transaction) => {
    setSelectedTransaction(transaction)
    setIsEditorOpen(true)
  }

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

  const pendingCount = infiniteTransactions.filter((t) => t.pending).length

  const handleSort = useCallback(
    (column: TransactionSortColumn, direction: SortDirection) => {
      setSort({ columnId: column, direction })
    },
    [setSort],
  )

  if (isError && !infiniteTransactions.length) {
    return (
      <main className="p-8 max-w-[1200px] mx-auto">
        <div className="text-center py-12 px-4">
          <p className="text-destructive mb-4 text-lg">Failed to load transactions. Please try again.</p>
          <button
            onClick={() => fetchNextPage()}
            className="px-6 py-3 text-base bg-primary text-primary-foreground border-none rounded cursor-pointer hover:opacity-90 transition-opacity"
          >
            Retry
          </button>
        </div>
      </main>
    )
  }

  const transactions = infiniteTransactions as unknown as Transaction[]
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
        onClearAllFilters={() => clearAllFilters()}
        disabled={isLoading}
      />

      {!isLoading && isEmpty ? (
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
              <Button variant="outline" size="sm" onClick={() => clearAllFilters()}>
                Clear all filters
              </Button>
            ) : undefined
          }
          className="mt-8"
        />
      ) : (
        <>
          {!isLoading && (
            <>
              <div className="mb-4">
                <p className="text-base text-muted-foreground m-0">
                  {hasActiveFilters ? (
                    <>Showing {transactions.length.toLocaleString()} matching transactions</>
                  ) : (
                    <>{transactions.length.toLocaleString()} transactions loaded</>
                  )}
                </p>
              </div>

              <div role="status" aria-live="polite" className="sr-only">
                {isFetchingNextPage ? 'Loading more transactions' : ''}
              </div>
            </>
          )}

          <div className="min-h-[200px] overflow-x-auto">
            <TransactionTable
              transactions={transactions}
              accountsMap={accountsMap}
              onEdit={handleEditTransaction}
              onCategorize={handleCategorizeTransaction}
              loading={isLoading}
              sortable
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              accountOptions={accountOptions}
              accountFilter={filters.account_ids}
              onAccountFilterChange={handleAccountFilterChange}
              categoryOptions={categoryOptions}
              categoryFilter={filters.pfc_primaries}
              onCategoryFilterChange={handleCategoryFilterChange}
              amountRange={currentAmountRange}
              onAmountRangeChange={handleAmountRangeChange}
              onScrollEnd={fetchNextPage}
              hasMore={hasNextPage}
              isFetchingMore={isFetchingNextPage}
            />
          </div>

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
