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
import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import { getCategoryColor } from '@/lib/category-colors'
import {
  ColumnFilterDropdown,
  type FilterOption,
  type RangeValue,
} from '@/shared/components/ColumnFilterDropdown'
import './TransactionTable.css'

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
 * Parse category from JSON string
 * Uses primary category (index 0) for consistency
 */
function parseCategory(categoryJson: string | null | undefined): string {
  if (!categoryJson) return 'Uncategorized'

  try {
    const categories = JSON.parse(categoryJson)
    if (Array.isArray(categories) && categories.length > 0) {
      return categories[0]
    }
    return 'Uncategorized'
  } catch {
    return 'Uncategorized'
  }
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
        className="transaction-table__sort-btn"
      >
        {label}
        <span className="transaction-table__sort-icon">
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
      <div className={`transaction-table__empty ${className}`.trim()}>
        <p>No transactions found.</p>
      </div>
    )
  }

  return (
    <table
      className={`transaction-table ${loading ? 'transaction-table--loading' : ''} ${className}`.trim()}
    >
      <thead>
        <tr className="transaction-table__header-row">
          <th className="transaction-table__th">
            {renderSortableHeader('Date', 'date')}
          </th>
          <th className="transaction-table__th">
            {renderSortableHeader('Description', 'merchant')}
          </th>
          <th className="transaction-table__th">
            <div className="transaction-table__header-content">
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
          <th className="transaction-table__th">
            <div className="transaction-table__header-content">
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
          <th className="transaction-table__th transaction-table__th--amount">
            <div className="transaction-table__header-content">
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
            <th className="transaction-table__th transaction-table__th--actions">
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
          const categoryToDisplay =
            transaction.category_override || transaction.category
          const categoryLabel = parseCategory(categoryToDisplay)
          const categoryColor = getCategoryColor(categoryLabel)
          const accountDisplayName = account
            ? account.nickname || account.name
            : 'Unknown'
          const accountBackgroundColor = account?.custom_color || '#E5E7EB'
          const amountFormatted = formatAmount(transaction.amount)

          return (
            <tr
              key={transaction.id}
              className={`transaction-table__row ${onRowClick ? 'transaction-table__row--clickable' : ''} ${transaction.pending ? 'transaction-table__row--pending' : ''}`}
              onClick={onRowClick ? () => onRowClick(transaction) : undefined}
            >
              <td className="transaction-table__td transaction-table__td--date">
                {displayDate}
              </td>
              <td className="transaction-table__td transaction-table__td--description">
                <div className="transaction-table__name">{displayName}</div>
                {transaction.tags && transaction.tags.length > 0 && (
                  <div className="transaction-table__tags">
                    {transaction.tags.map((tag) => (
                      <span key={tag} className="transaction-table__tag">
                        {tag}
                      </span>
                    ))}
                  </div>
                )}
              </td>
              <td className="transaction-table__td transaction-table__td--account">
                <span
                  className="transaction-table__account-badge"
                  style={{ backgroundColor: accountBackgroundColor }}
                >
                  {account?.custom_emoji && <span>{account.custom_emoji}</span>}
                  {accountDisplayName}
                </span>
              </td>
              <td className="transaction-table__td transaction-table__td--category">
                <span className="transaction-table__category">
                  <span
                    className="transaction-table__category-dot"
                    style={{ backgroundColor: categoryColor }}
                  />
                  {categoryLabel}
                </span>
              </td>
              <td className="transaction-table__td transaction-table__td--amount">
                <div className="transaction-table__amount-container">
                  <span
                    className={`transaction-table__amount ${
                      transaction.amount > 0
                        ? 'transaction-table__amount--credit'
                        : transaction.amount < 0
                          ? 'transaction-table__amount--debit'
                          : ''
                    }`}
                    data-ph-mask
                  >
                    {amountFormatted}
                  </span>
                  {transaction.pending && (
                    <span className="transaction-table__pending-badge">
                      <Clock
                        className="transaction-table__pending-icon"
                        aria-hidden="true"
                      />
                      Pending
                    </span>
                  )}
                </div>
              </td>
              {hasActions && (
                <td
                  className="transaction-table__td transaction-table__td--actions"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="transaction-table__actions">
                    {onEdit && (
                      <button
                        type="button"
                        onClick={() => onEdit(transaction)}
                        disabled={loading}
                        className="transaction-table__action-btn"
                        aria-label="Edit transaction"
                        title="Edit transaction"
                      >
                        <Edit aria-hidden="true" />
                      </button>
                    )}
                    {onCategorize && (
                      <button
                        type="button"
                        onClick={() => onCategorize(transaction)}
                        disabled={loading}
                        className="transaction-table__action-btn"
                        aria-label="Categorize transaction"
                        title="Categorize transaction"
                      >
                        <Tag aria-hidden="true" />
                      </button>
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
