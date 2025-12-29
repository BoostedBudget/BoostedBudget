/**
 * AccountTable Component
 * Issue #584: Unified Card/Table View System
 *
 * Displays accounts in a table format with sorting and actions.
 */

import { useCallback } from 'react'
import { Edit, Trash2, ChevronUp, ChevronDown, EyeOff } from 'lucide-react'
import type { Account } from '@/api/generated/models'
import './AccountTable.css'

export type SortColumn = 'institution' | 'name' | 'type' | 'balance'
export type SortDirection = 'asc' | 'desc'

export interface AccountTableProps {
  /** Accounts to display */
  accounts: Account[]
  /** Called when edit button is clicked */
  onEdit?: (account: Account) => void
  /** Called when delete button is clicked */
  onDelete?: (account: Account) => void
  /** Called when a row is clicked */
  onRowClick?: (account: Account) => void
  /** Show loading state */
  loading?: boolean
  /** Enable sorting */
  sortable?: boolean
  /** Currently sorted column */
  sortColumn?: SortColumn
  /** Current sort direction */
  sortDirection?: SortDirection
  /** Called when sort changes */
  onSort?: (column: SortColumn, direction: SortDirection) => void
  /** Additional CSS class */
  className?: string
}

/**
 * Format account balance as currency
 */
function formatBalance(balance: number | undefined): string {
  if (balance === undefined || balance === null) return '—'

  const absBalance = Math.abs(balance)
  const formatted = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(absBalance)

  if (balance < 0) return `-${formatted}`
  return formatted
}

/**
 * Get human-readable account type label
 */
function getAccountTypeLabel(type: string, subtype?: string): string {
  // Use subtype for more specific label when available
  if (subtype) {
    switch (subtype.toLowerCase()) {
      case 'checking':
        return 'Checking'
      case 'savings':
        return 'Savings'
      case 'credit card':
        return 'Credit Card'
      case 'money market':
        return 'Money Market'
      case 'cd':
        return 'CD'
      case 'paypal':
        return 'PayPal'
      case 'brokerage':
        return 'Brokerage'
      case '401k':
        return '401(k)'
      case 'ira':
        return 'IRA'
      case 'roth':
        return 'Roth IRA'
      case 'mortgage':
        return 'Mortgage'
      case 'auto':
        return 'Auto Loan'
      case 'student':
        return 'Student Loan'
      default:
        // Capitalize first letter
        return subtype.charAt(0).toUpperCase() + subtype.slice(1)
    }
  }

  // Fallback to type
  switch (type.toLowerCase()) {
    case 'depository':
      return 'Bank Account'
    case 'credit':
      return 'Credit'
    case 'investment':
      return 'Investment'
    case 'loan':
      return 'Loan'
    case 'other':
      return 'Other'
    default:
      return type.charAt(0).toUpperCase() + type.slice(1)
  }
}

/**
 * AccountTable displays accounts in a sortable table format.
 */
export function AccountTable({
  accounts,
  onEdit,
  onDelete,
  onRowClick,
  loading = false,
  sortable = false,
  sortColumn,
  sortDirection = 'asc',
  onSort,
  className = '',
}: AccountTableProps) {
  const hasActions = !!(onEdit || onDelete)

  const handleSort = useCallback(
    (column: SortColumn) => {
      if (!sortable || !onSort) return
      const newDirection =
        sortColumn === column && sortDirection === 'asc' ? 'desc' : 'asc'
      onSort(column, newDirection)
    },
    [sortable, onSort, sortColumn, sortDirection]
  )

  const renderSortableHeader = (label: string, column: SortColumn) => {
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
        className="account-table__sort-btn"
      >
        {label}
        <span className="account-table__sort-icon">
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

  if (accounts.length === 0) {
    return (
      <div className={`account-table__empty ${className}`.trim()}>
        <p>No accounts found. Connect a bank account to get started.</p>
      </div>
    )
  }

  return (
    <table
      className={`account-table ${loading ? 'account-table--loading' : ''} ${className}`.trim()}
    >
      <thead>
        <tr className="account-table__header-row">
          <th className="account-table__th">
            {renderSortableHeader('Institution', 'institution')}
          </th>
          <th className="account-table__th">
            {renderSortableHeader('Account', 'name')}
          </th>
          <th className="account-table__th">
            {renderSortableHeader('Type', 'type')}
          </th>
          <th className="account-table__th">Mask</th>
          <th className="account-table__th account-table__th--balance">
            {renderSortableHeader('Balance', 'balance')}
          </th>
          {hasActions && (
            <th className="account-table__th account-table__th--actions">
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {accounts.map((account) => {
          const displayName = account.nickname || account.name
          const typeLabel = getAccountTypeLabel(account.type, account.subtype)
          const balanceFormatted = formatBalance(account.current_balance)
          const isNegativeBalance =
            account.current_balance !== undefined && account.current_balance < 0

          return (
            <tr
              key={account.id}
              className={`account-table__row ${onRowClick ? 'account-table__row--clickable' : ''} ${account.is_hidden ? 'account-table__row--hidden' : ''}`}
              onClick={onRowClick ? () => onRowClick(account) : undefined}
            >
              <td className="account-table__td account-table__td--institution">
                <span className="account-table__institution">
                  {account.institution_name || 'Unknown'}
                </span>
              </td>
              <td className="account-table__td account-table__td--name">
                <div className="account-table__name-cell">
                  {account.custom_emoji && (
                    <span className="account-table__emoji">
                      {account.custom_emoji}
                    </span>
                  )}
                  <span
                    className="account-table__name"
                    style={
                      account.custom_color
                        ? { color: account.custom_color }
                        : undefined
                    }
                  >
                    {displayName}
                  </span>
                  {account.is_hidden && (
                    <span className="account-table__hidden-badge">
                      <EyeOff className="account-table__hidden-icon" aria-hidden="true" />
                      Hidden
                    </span>
                  )}
                </div>
              </td>
              <td className="account-table__td">{typeLabel}</td>
              <td className="account-table__td account-table__td--mask">
                {account.mask ? `••••${account.mask}` : '—'}
              </td>
              <td className="account-table__td account-table__td--balance">
                <span
                  className={`account-table__balance ${
                    isNegativeBalance
                      ? 'account-table__balance--negative'
                      : 'account-table__balance--positive'
                  }`}
                >
                  {balanceFormatted}
                </span>
              </td>
              {hasActions && (
                <td
                  className="account-table__td account-table__td--actions"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="account-table__actions">
                    {onEdit && (
                      <button
                        type="button"
                        onClick={() => onEdit(account)}
                        disabled={loading}
                        className="account-table__action-btn"
                        aria-label="Edit account"
                        title="Edit account"
                      >
                        <Edit aria-hidden="true" />
                      </button>
                    )}
                    {onDelete && (
                      <button
                        type="button"
                        onClick={() => onDelete(account)}
                        disabled={loading}
                        className="account-table__action-btn account-table__action-btn--danger"
                        aria-label="Delete account"
                        title="Delete account"
                      >
                        <Trash2 aria-hidden="true" />
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
