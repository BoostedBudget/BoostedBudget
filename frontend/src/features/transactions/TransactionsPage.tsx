import { useState, useEffect, useRef } from 'react'
import type { CSSProperties, ReactElement } from 'react'
import { List } from 'react-window'
import { useTransactions } from './hooks/useTransactions'
import { useTransactionFilters } from './hooks/useTransactionFilters'
import { TransactionCard } from './components/TransactionCard'
import { TransactionEditor } from './components/TransactionEditor'
import { TransactionFilterPanel } from './components/TransactionFilterPanel'
import { fetchTags } from '../tags/api/tagsApi'
import { fetchCategories } from '../categories/api/categoriesApi'
import type { Transaction } from './types'
import type { UserTag } from '../tags/types'
import type { CustomCategory } from '../categories/types'
import './TransactionsPage.css'

/** Height of each transaction item in the virtualized list (in pixels) */
const ITEM_HEIGHT = 96

/** Height of the virtualized list container */
const LIST_HEIGHT = 600

/** Props for the virtualized list row component */
interface TransactionRowProps {
  transactions: Transaction[]
  onEdit: (transaction: Transaction) => void
}

/**
 * Row component for the virtualized transaction list
 * react-window v2 uses a component-based API for rows
 */
function TransactionRow({
  index,
  style,
  transactions,
  onEdit,
}: {
  ariaAttributes: {
    'aria-posinset': number
    'aria-setsize': number
    role: 'listitem'
  }
  index: number
  style: CSSProperties
} & TransactionRowProps): ReactElement {
  const transaction = transactions[index]
  return (
    <div style={style} className="virtualized-list-item">
      <TransactionCard transaction={transaction} onEdit={onEdit} />
    </div>
  )
}

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
  const [availableTags, setAvailableTags] = useState<UserTag[]>([])
  const [availableCategories, setAvailableCategories] = useState<CustomCategory[]>([])
  const [listWidth, setListWidth] = useState(0)
  const listContainerRef = useRef<HTMLDivElement>(null)

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

  // Fetch available tags and categories for the editor
  useEffect(() => {
    const loadMetadata = async () => {
      try {
        const [tags, categories] = await Promise.all([
          fetchTags(),
          fetchCategories(),
        ])
        setAvailableTags(tags)
        setAvailableCategories(categories)
      } catch (err) {
        console.error('Failed to load tags/categories:', err)
      }
    }
    loadMetadata()
  }, [])

  // Measure list container width for virtualized list
  useEffect(() => {
    const updateWidth = () => {
      if (listContainerRef.current) {
        setListWidth(listContainerRef.current.offsetWidth)
      }
    }

    updateWidth()
    window.addEventListener('resize', updateWidth)
    return () => window.removeEventListener('resize', updateWidth)
  }, [])

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

  // Handle tag creation - add to available tags
  const handleTagCreated = (tag: UserTag) => {
    setAvailableTags((prev) => [...prev, tag])
  }

  // Handle category creation - add to available categories
  const handleCategoryCreated = (category: CustomCategory) => {
    setAvailableCategories((prev) => [...prev, category])
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

          <div className="transactions-list" ref={listContainerRef}>
            {/*
              For smaller lists (< 20 items) or when width isn't measured yet (tests),
              use simple rendering for better test compatibility.
              For larger lists, use virtualization for performance.
            */}
            {transactions.length < 20 || listWidth === 0 ? (
              // Simple list for small datasets or test environments
              transactions.map((transaction) => (
                <TransactionCard
                  key={transaction.id}
                  transaction={transaction}
                  onEdit={handleEditTransaction}
                />
              ))
            ) : (
              // Virtualized list for large datasets
              <List<TransactionRowProps>
                style={{
                  height: Math.min(LIST_HEIGHT, transactions.length * ITEM_HEIGHT),
                  width: listWidth,
                }}
                className="virtualized-list"
                rowComponent={TransactionRow}
                rowCount={transactions.length}
                rowHeight={ITEM_HEIGHT}
                rowProps={{
                  transactions,
                  onEdit: handleEditTransaction,
                }}
              />
            )}
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
