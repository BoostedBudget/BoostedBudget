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
import type { SortState } from '@/shared/components/DataTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { TransactionEditor } from './components/TransactionEditor'
import { InvestmentDetailDrawer } from './components/InvestmentDetailDrawer'
import { TransactionFilterPanel } from './components/TransactionFilterPanel'
import { useGetUserTags, getGetUserTagsQueryKey } from '@boostedbudget/api-client/endpoints/tags/tags'
import { useGetUserCategories, getGetUserCategoriesQueryKey, useGetPlaidPrimaryCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import { useAllAccounts, isInactive } from '@/features/accounts/hooks'
import type { Transaction, FeedRow, InvestmentActivity, ActivityType } from './types'
import { isInvestmentRow } from './types'
import type { Account } from '../accounts/types'
import type { UserTag } from '../tags/types'
import type { CustomCategory } from '../categories/types'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'

/**
 * T3: When investment activity is included, the API is forced to sort_by=date (see
 * getEffectiveSortBy in useInfiniteTransactions). The table must receive an effectiveSort
 * that reflects this so the sort indicator matches the actual row order — without
 * overwriting the user's sort preference in URL state (they see date indicator while
 * investments are on; their original sort resumes when they toggle off investments).
 *
 * Exported for unit testing.
 */
// eslint-disable-next-line react-refresh/only-export-components
export function computeEffectiveSort(
  sort: SortState | null,
  includeInvestments: boolean,
): SortState | null {
  if (!includeInvestments) return sort
  if (!sort) return { columnId: 'date', direction: 'desc' }
  if (sort.columnId === 'date') return sort
  return { columnId: 'date', direction: sort.direction }
}

/**
 * Issue #1569, Task 3: the backend deliberately returns a disconnected account's
 * transactions (retainedAccountVisibilityLeg), so the filter dropdown must offer
 * retained (non-active) accounts too — not just active ones — or the user has no
 * way to filter to them.
 *
 * Active accounts are listed first (this is a transactions page, not an account
 * manager — a long tail of closed accounts must not crowd the top of the list),
 * and each closed account's label carries the "(Closed)" suffix used by the
 * closed-accounts surface on AccountsPage (see AccountTable.tsx / AccountCard.tsx's
 * "Closed" badge), so the user cannot mistake a closed account for a live one.
 *
 * Exported for unit testing.
 */
// eslint-disable-next-line react-refresh/only-export-components
export function buildAccountOptions(accounts: Account[]): FilterOption[] {
  const toOption = (acc: Account): FilterOption => ({
    value: acc.id,
    label: isInactive(acc) ? `${acc.nickname || acc.name} (Closed)` : (acc.nickname || acc.name),
    masked: true,
  })
  const active = accounts.filter((acc) => !isInactive(acc))
  const closed = accounts.filter(isInactive)
  return [...active.map(toOption), ...closed.map(toOption)]
}

export function TransactionsPage() {
  const {
    filters,
    sort,
    hasActiveFilters,
    setFilter,
    setFilters,
    clearAllFilters,
    setSort,
    datePreset,
    statusFilter,
    setDatePreset,
    setCustomDateRange,
    setStatusFilter,
    setIncludeInvestmentActivity,
    setIncludeReversed,
    setUncategorized,
    setActivityTypes,
  } = useTransactionFilters()

  // T3: effectiveSort keeps the table sort indicator honest — it shows 'date' when the
  // API is forced to date-sort (investment activity enabled). Does NOT mutate URL state.
  const investmentsIncluded = filters.include_investment_activity !== false
  const effectiveSort = computeEffectiveSort(sort, investmentsIncluded)
  const sortColumn = (effectiveSort?.columnId ?? 'date') as TransactionSortColumn
  const sortDirection = (effectiveSort?.direction ?? 'desc') as SortDirection

  const [selectedTransaction, setSelectedTransaction] = useState<Transaction | null>(null)
  const [isEditorOpen, setIsEditorOpen] = useState(false)
  const [selectedInvestment, setSelectedInvestment] = useState<InvestmentActivity | null>(null)
  const [isInvestmentDrawerOpen, setIsInvestmentDrawerOpen] = useState(false)
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()
  const searchDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.TRANSACTIONS_VIEWED)
  }, [trackEvent])

  const { data: tagsData } = useGetUserTags()
  const { data: categoriesData } = useGetUserCategories()
  // Issue #1569, Task 3: useAllAccounts (not useActiveAccounts) — the backend now
  // deliberately returns a disconnected account's transactions, so both the
  // id→name lookup (accountsMap) and the filter dropdown (accountOptions) need
  // the full universe of statuses, not just active accounts.
  const { accounts: accountsList } = useAllAccounts()
  const { data: plaidPrimariesData } = useGetPlaidPrimaryCategories()

  const availableTags = (tagsData as unknown as UserTag[]) ?? []
  const availableCategories = (categoriesData as unknown as CustomCategory[]) ?? []
  const plaidPrimaries = (plaidPrimariesData as unknown as Primary[]) ?? []

  // Create accounts lookup map
  const accountsMap = useMemo(() => {
    return (accountsList ?? []).reduce<Record<string, Account>>((map, acc) => {
      map[acc.id] = acc
      return map
    }, {})
  }, [accountsList])

  // Create account filter options — active accounts first, closed accounts labeled
  // and appended after (see buildAccountOptions).
  const accountOptions = useMemo<FilterOption[]>(() => buildAccountOptions(accountsList ?? []), [accountsList])

  const handleAccountFilterChange = useCallback(
    (value: string[]) => setFilter('account_ids', value.length > 0 ? value : undefined),
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

  const handleOpenEditor = useCallback((transaction: Transaction) => {
    setSelectedTransaction(transaction)
    setIsEditorOpen(true)
  }, [])

  // T2: handleRowClick now takes FeedRow directly — no double-cast needed.
  const handleRowClick = useCallback((row: FeedRow) => {
    if (isInvestmentRow(row)) {
      setSelectedInvestment(row)
      setIsInvestmentDrawerOpen(true)
      trackEvent(ANALYTICS_EVENTS.INVESTMENT_DETAIL_DRAWER_OPENED)
    } else {
      // row is BankingTransaction here; single narrow cast to Transaction for the editor.
      handleOpenEditor(row as unknown as Transaction)
    }
  }, [handleOpenEditor, trackEvent])

  const handleIncludeInvestmentChange = useCallback((enabled: boolean) => {
    setIncludeInvestmentActivity(enabled)
    trackEvent(ANALYTICS_EVENTS.INVESTMENT_ACTIVITY_FILTER_TOGGLED, { enabled })
  }, [setIncludeInvestmentActivity, trackEvent])

  // Issue #1569 T12: reversals are opt-in, so the toggle is a user decision worth recording.
  const handleIncludeReversedChange = useCallback((enabled: boolean) => {
    setIncludeReversed(enabled)
    trackEvent(ANALYTICS_EVENTS.REVERSED_TRANSACTIONS_FILTER_TOGGLED, { enabled })
  }, [setIncludeReversed, trackEvent])

  // Issue #1626. Only ever called with false: the pill is a way OUT of a filter the reports
  // Sankey turned on, so the event records a clear, not a toggle.
  const handleUncategorizedChange = useCallback((enabled: boolean) => {
    setUncategorized(enabled)
    trackEvent(ANALYTICS_EVENTS.UNCATEGORIZED_FILTER_CLEARED, { enabled })
  }, [setUncategorized, trackEvent])

  const handleActivityTypesChange = useCallback((types: ActivityType[]) => {
    setActivityTypes(types)
    trackEvent(ANALYTICS_EVENTS.INVESTMENT_ACTIVITY_TYPE_FILTER_APPLIED, { count: types.length })
  }, [setActivityTypes, trackEvent])

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

  function handleSearchChange(event: React.ChangeEvent<HTMLInputElement>) {
    const searchValue = event.target.value
    setFilter('search', searchValue || undefined) // Remove if empty

    if (searchDebounceRef.current) clearTimeout(searchDebounceRef.current)
    if (searchValue) {
      searchDebounceRef.current = setTimeout(() => {
        trackEvent(ANALYTICS_EVENTS.TRANSACTION_SEARCHED, { query_length: searchValue.length })
      }, 500)
    }
  }

  const pendingCount = useMemo(
    () => infiniteTransactions.filter((t) => t.pending).length,
    [infiniteTransactions]
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

  // T2: infiniteTransactions is now typed FeedRow[] via the toFeedRow boundary mapper.
  const isEmpty = infiniteTransactions.length === 0

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
        pendingCount={pendingCount}
        onDatePresetChange={setDatePreset}
        onCustomDateChange={setCustomDateRange}
        onStatusFilterChange={setStatusFilter}
        onIncludeInvestmentActivityChange={handleIncludeInvestmentChange}
        onActivityTypesChange={handleActivityTypesChange}
        onIncludeReversedChange={handleIncludeReversedChange}
        onUncategorizedChange={handleUncategorizedChange}
        onClearAll={clearAllFilters}
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
                  {hasActiveFilters
                    ? `Showing ${infiniteTransactions.length.toLocaleString()} matching transactions`
                    : `${infiniteTransactions.length.toLocaleString()} transactions loaded`}
                </p>
              </div>
              <div role="status" aria-live="polite" className="sr-only">
                {isFetchingNextPage && 'Loading more transactions'}
              </div>
            </>
          )}

          <div className="min-h-[200px] overflow-x-auto">
            <TransactionTable
              transactions={infiniteTransactions}
              accountsMap={accountsMap}
              onEdit={handleOpenEditor}
              onCategorize={handleOpenEditor}
              onRowClick={handleRowClick}
              loading={isLoading}
              sort={effectiveSort}
              onSortChange={setSort}
              accountOptions={accountOptions}
              accountFilter={filters.account_ids}
              onAccountFilterChange={handleAccountFilterChange}
              selectedPrimaries={filters.pfc_primaries}
              onPrimariesChange={(v) => {
                const isApplied = v.length > 0
                setFilter('pfc_primaries', isApplied ? v : undefined)
                trackEvent(isApplied ? ANALYTICS_EVENTS.CATEGORY_FILTER_APPLIED : ANALYTICS_EVENTS.CATEGORY_FILTER_CLEARED)
              }}
              selectedDetaileds={filters.pfc_detaileds}
              onDetailedsChange={(v) => {
                const isApplied = v.length > 0
                setFilter('pfc_detaileds', isApplied ? v : undefined)
                trackEvent(isApplied ? ANALYTICS_EVENTS.CATEGORY_FILTER_APPLIED : ANALYTICS_EVENTS.CATEGORY_FILTER_CLEARED)
              }}
              customCategories={Array.isArray(categoriesData) ? categoriesData : undefined}
              selectedCustomIds={filters.custom_category_ids}
              onCustomIdsChange={(v) => {
                const isApplied = v.length > 0
                setFilter('custom_category_ids', isApplied ? v : undefined)
                trackEvent(isApplied ? ANALYTICS_EVENTS.CATEGORY_FILTER_APPLIED : ANALYTICS_EVENTS.CATEGORY_FILTER_CLEARED)
              }}
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

      <InvestmentDetailDrawer
        transaction={selectedInvestment}
        open={isInvestmentDrawerOpen}
        onOpenChange={setIsInvestmentDrawerOpen}
        accountsMap={accountsMap}
      />
    </main>
  )
}
