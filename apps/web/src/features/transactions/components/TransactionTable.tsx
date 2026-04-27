/**
 * TransactionTable Component
 * Issue #584: Unified Card/Table View System
 * Issue #590: Enhanced Table Sorting and Inline Column Filtering
 *
 * Displays transactions in a table format for the Transactions page.
 * This is the table-only view (no card/table toggle for transactions).
 */

import { useCallback } from 'react'
import { Edit, Tag, ChevronUp, ChevronDown, Clock } from 'lucide-react'
import { ReadOnlyGuard } from '@/auth'
import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import { getCategoryColor } from '@/lib/category-colors'
import { getEffectiveCategory } from '@/shared/categories/getEffectiveCategory'
import {
  ColumnFilterDropdown,
  type FilterOption,
  type RangeValue,
} from '@/shared/components/ColumnFilterDropdown'

export type TransactionSortColumn = 'date' | 'merchant' | 'amount' | 'category'
export type SortDirection = 'asc' | 'desc'

export interface TransactionTableProps {
  /** Transactions to display */
  transactions: Transaction[]
  /** Account lookup map for displaying account info */
  accountsMap: Record<string, Account>
  /** Called when edit button clicked */
  onEdit?: (transaction: Transaction) => void
  /** Called when categorize button clicked */
  onCategorize?: (transaction: Transaction) => void
  /** Called when a row is clicked */
  onRowClick?: (transaction: Transaction) => void
  /** Show loading state */
  loading?: boolean
  /** Enable sorting */
  sortable?: boolean
  /** Currently sorted column */
  sortColumn?: TransactionSortColumn
  /** Current sort direction */
  sortDirection?: SortDirection
  /** Called when sort changes */
  onSort?: (column: TransactionSortColumn, direction: SortDirection) => void
  /** Additional CSS class */
  className?: string

  // Issue #590: Inline filter props
  /** Available account options for filtering */
  accountOptions?: FilterOption[]
  /** Current account filter value */
  accountFilter?: string[]
  /** Called when account filter changes */
  onAccountFilterChange?: (value: string[]) => void

  /** Available category options for filtering */
  categoryOptions?: FilterOption[]
  /** Current category filter value */
  categoryFilter?: string[]
  /** Called when category filter changes */
  onCategoryFilterChange?: (value: string[]) => void

  /** Current amount range filter value */
  amountRange?: RangeValue | null
  /** Called when amount range filter changes */
  onAmountRangeChange?: (value: RangeValue | null) => void
}

/**
 * Format transaction amount with sign and currency
 */
function formatAmount(amount: number): string {
  const absAmount = Math.abs(amount)
  const formatted = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(absAmount)

  if (amount === 0) return formatted
  if (amount > 0) return `+${formatted}`
  return `-${formatted}`
}


/**
 * Format date for display
 */
function formatDate(dateStr: string | null | undefined): string {
  if (!dateStr || typeof dateStr !== 'string') {
    return 'Invalid Date'
  }

  // Extract date portion from RFC3339 format
  const datePortion = dateStr.split('T')[0]

  if (!/^\d{4}-\d{2}-\d{2}$/.test(datePortion)) {
    return 'Invalid Date'
  }

  const [year, month, day] = datePortion.split('-').map(Number)

  if (isNaN(year) || isNaN(month) || isNaN(day)) {
    return 'Invalid Date'
  }

  const transactionDate = new Date(year, month - 1, day)

  if (isNaN(transactionDate.getTime())) {
    return 'Invalid Date'
  }

  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }).format(transactionDate)
}

/**
 * TransactionTable displays transactions in a sortable table format.
 */
export function TransactionTable({
  transactions,
  accountsMap,
  onEdit,
  onCategorize,
  onRowClick,
  loading = false,
  sortable = false,
  sortColumn,
  sortDirection = 'asc',
  onSort,
  className = '',
  // Issue #590: Inline filter props
  accountOptions,
  accountFilter,
  onAccountFilterChange,
  categoryOptions,
  categoryFilter,
  onCategoryFilterChange,
  amountRange,
  onAmountRangeChange,
}: TransactionTableProps) {
  const hasActions = !!(onEdit || onCategorize)

  const handleSort = useCallback(
    (column: TransactionSortColumn) => {
      if (!sortable || !onSort) return
      const newDirection =
        sortColumn === column && sortDirection === 'asc' ? 'desc' : 'asc'
      onSort(column, newDirection)
    },
    [sortable, onSort, sortColumn, sortDirection]
  )

  const renderSortableHeader = (
    label: string,
    column: TransactionSortColumn
  ) => {
    if (!sortable) {
      return label
    }

    const isActive = sortColumn === column
    const ariaSort = isActive
      ? sortDirection === 'asc'
        ? 'ascending'
        : 'descending'
      : undefined

    return (
      <button
        type="button"
        onClick={() => handleSort(column)}
        aria-sort={ariaSort}
        className="inline-flex cursor-pointer items-center gap-1 border-none bg-transparent p-0 font-[inherit] text-inherit"
      >
        {label}
        <span className="inline-flex h-4 w-4 items-center [&_svg]:h-3.5 [&_svg]:w-3.5">
          {isActive && sortDirection === 'asc' && (
            <ChevronUp aria-hidden="true" />
          )}
          {isActive && sortDirection === 'desc' && (
            <ChevronDown aria-hidden="true" />
          )}
        </span>
      </button>
    )
  }

  if (transactions.length === 0) {
    return (
      <div className={`text-center rounded-lg border border-dashed border-border bg-card px-6 py-12 text-muted-foreground ${className}`}>
        <p>No transactions found.</p>
      </div>
    )
  }

  return (
    <table
      className={`w-full border-collapse overflow-visible rounded-lg border border-border bg-card ${loading ? 'transaction-table--loading pointer-events-none opacity-60' : ''} ${className}`}
    >
      <thead>
        <tr className="bg-muted/50">
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            {renderSortableHeader('Date', 'date')}
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            {renderSortableHeader('Description', 'merchant')}
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            <div className="flex items-center gap-1">
              <span>Account</span>
              {accountOptions && onAccountFilterChange && (
                <ColumnFilterDropdown
                  type="select"
                  options={accountOptions}
                  value={accountFilter}
                  onChange={(val) => onAccountFilterChange(val as string[])}
                  multiSelect
                  searchable
                  ariaLabel="Filter by account"
                />
              )}
            </div>
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Category', 'category')}
              {categoryOptions && onCategoryFilterChange && (
                <ColumnFilterDropdown
                  type="select"
                  options={categoryOptions}
                  value={categoryFilter}
                  onChange={(val) => onCategoryFilterChange(val as string[])}
                  multiSelect
                  searchable
                  ariaLabel="Filter by category"
                />
              )}
            </div>
          </th>
          <th className="border-b border-border px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Amount', 'amount')}
              {onAmountRangeChange && (
                <ColumnFilterDropdown
                  type="range"
                  rangeValue={amountRange}
                  onChange={(val) => onAmountRangeChange(val as RangeValue | null)}
                  ariaLabel="Filter by amount"
                />
              )}
            </div>
          </th>
          {hasActions && (
            <th className="w-24 border-b border-border px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground max-md:w-16">
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {transactions.map((transaction) => {
          const account = accountsMap[transaction.account_id]
          const displayDate = formatDate(
            transaction.authorized_date || transaction.date
          )
          const displayName = transaction.merchant_name || transaction.name
          const effective = getEffectiveCategory(transaction)
          const categoryLabel = effective.length > 0 ? effective[0] : 'Uncategorized'
          const categoryColor = getCategoryColor(categoryLabel)
          const accountDisplayName = account
            ? account.nickname || account.name
            : 'Unknown'
          const accountBackgroundColor = account?.custom_color || '#E5E7EB'
          const amountFormatted = formatAmount(transaction.amount)

          return (
            <tr
              key={transaction.id}
              className={`transition-colors duration-150 ${
                onRowClick ? 'transaction-table__row--clickable cursor-pointer' : ''
              } ${
                transaction.pending
                  ? 'bg-yellow-50 hover:bg-yellow-100 dark:bg-yellow-900 dark:hover:bg-yellow-800'
                  : 'hover:bg-muted/50'
              }`}
              onClick={onRowClick ? () => onRowClick(transaction) : undefined}
            >
              <td className="whitespace-nowrap border-b border-border px-4 py-3.5 text-sm align-middle text-muted-foreground max-md:px-3 max-md:py-2.5">
                {displayDate}
              </td>
              <td className="min-w-[200px] border-b border-border px-4 py-3.5 text-sm align-middle max-md:min-w-[150px] max-md:px-3 max-md:py-2.5">
                <div className="font-medium text-foreground">{displayName}</div>
                {transaction.tags && transaction.tags.length > 0 && (
                  <div className="mt-1 flex flex-wrap gap-1">
                    {transaction.tags.map((tag) => (
                      <span key={tag} className="inline-flex items-center rounded bg-blue-50 px-1.5 py-0.5 text-[0.625rem] font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-300">
                        {tag}
                      </span>
                    ))}
                  </div>
                )}
              </td>
              <td className="border-b border-border px-4 py-3.5 text-sm align-middle max-md:px-3 max-md:py-2.5">
                <span
                  className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-gray-800"
                  style={{ backgroundColor: accountBackgroundColor }}
                >
                  {account?.custom_emoji && <span>{account.custom_emoji}</span>}
                  {accountDisplayName}
                </span>
              </td>
              <td className="border-b border-border px-4 py-3.5 text-sm align-middle text-foreground max-md:px-3 max-md:py-2.5">
                <span className="inline-flex items-center gap-1.5">
                  <span
                    className="h-2 w-2 shrink-0 rounded-full"
                    style={{ backgroundColor: categoryColor }}
                  />
                  {categoryLabel}
                </span>
              </td>
              <td className="border-b border-border px-4 py-3.5 text-right align-middle max-md:px-3 max-md:py-2.5">
                <div className="flex flex-col items-end gap-1">
                  <span
                    className={`font-semibold tabular-nums ${
                      transaction.amount > 0
                        ? 'transaction-table__amount--credit text-green-600 dark:text-green-400'
                        : transaction.amount < 0
                          ? 'transaction-table__amount--debit text-red-600 dark:text-red-400'
                          : 'text-foreground'
                    }`}
                    data-ph-mask
                  >
                    {amountFormatted}
                  </span>
                  {transaction.pending && (
                    <span className="inline-flex items-center gap-1 rounded bg-yellow-100 px-1.5 py-0.5 text-[0.625rem] font-medium text-yellow-800 dark:bg-yellow-800 dark:text-yellow-200">
                      <Clock
                        className="h-2.5 w-2.5"
                        aria-hidden="true"
                      />
                      Pending
                    </span>
                  )}
                </div>
              </td>
              {hasActions && (
                <td
                  className="border-b border-border px-4 py-3.5 text-right align-middle max-md:px-3 max-md:py-2.5"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="flex items-center justify-end gap-2">
                    {onEdit && (
                      <ReadOnlyGuard message="Resubscribe to edit transactions">
                        <button
                          type="button"
                          onClick={() => onEdit(transaction)}
                          disabled={loading}
                          className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors duration-150 hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
                          aria-label="Edit transaction"
                          title="Edit transaction"
                        >
                          <Edit aria-hidden="true" />
                        </button>
                      </ReadOnlyGuard>
                    )}
                    {onCategorize && (
                      <ReadOnlyGuard message="Resubscribe to categorize transactions">
                        <button
                          type="button"
                          onClick={() => onCategorize(transaction)}
                          disabled={loading}
                          className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors duration-150 hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
                          aria-label="Categorize transaction"
                          title="Categorize transaction"
                        >
                          <Tag aria-hidden="true" />
                        </button>
                      </ReadOnlyGuard>
                    )}
                  </div>
                </td>
              )}
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
