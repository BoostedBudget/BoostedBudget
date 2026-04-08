/**
 * AccountTable Component
 * Issue #584: Unified Card/Table View System
 *
 * Displays accounts in a table format with sorting and actions.
 */

import { useCallback } from 'react'
import { Edit, Trash2, ChevronUp, ChevronDown, EyeOff, Unplug } from 'lucide-react'
import type { Account } from '@boostedbudget/api-client/models'
import {
  ColumnFilterDropdown,
  type FilterOption,
  type RangeValue,
} from '@/shared/components/ColumnFilterDropdown'

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

  // Issue #590: Inline filter props
  /** Available institution options for filtering */
  institutionOptions?: FilterOption[]
  /** Current institution filter value */
  institutionFilter?: string[]
  /** Called when institution filter changes */
  onInstitutionFilterChange?: (value: string[]) => void

  /** Available type options for filtering */
  typeOptions?: FilterOption[]
  /** Current type filter value */
  typeFilter?: string[]
  /** Called when type filter changes */
  onTypeFilterChange?: (value: string[]) => void

  /** Current balance range filter value */
  balanceRange?: RangeValue | null
  /** Called when balance range filter changes */
  onBalanceRangeChange?: (value: RangeValue | null) => void
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
  // Issue #590: Inline filter props
  institutionOptions,
  institutionFilter,
  onInstitutionFilterChange,
  typeOptions,
  typeFilter,
  onTypeFilterChange,
  balanceRange,
  onBalanceRangeChange,
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

  // Issue #590: Compute if any filters are active
  // Note: Use != null to check for both null and undefined
  const hasActiveFilters =
    (institutionFilter?.length ?? 0) > 0 ||
    (typeFilter?.length ?? 0) > 0 ||
    balanceRange != null

  // Only show empty state if no data AND no active filters
  if (accounts.length === 0 && !hasActiveFilters) {
    return (
      <div className={`text-center rounded-lg border border-dashed border-gray-300 bg-white px-6 py-12 text-gray-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-400 ${className}`.trim()}>
        <p>No accounts found. Connect a bank account to get started.</p>
      </div>
    )
  }

  return (
    <table
      className={`w-full border-collapse overflow-visible rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800 ${loading ? 'account-table--loading pointer-events-none opacity-60' : ''} ${className}`.trim()}
    >
      <thead>
        <tr className="bg-gray-50 dark:bg-gray-900">
          <th className="border-b border-gray-200 px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-gray-600 dark:border-gray-700 dark:text-gray-400">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Institution', 'institution')}
              {institutionOptions && onInstitutionFilterChange && (
                <ColumnFilterDropdown
                  type="select"
                  options={institutionOptions}
                  value={institutionFilter}
                  onChange={(val) => onInstitutionFilterChange(val as string[])}
                  multiSelect
                  searchable
                  ariaLabel="Filter by institution"
                />
              )}
            </div>
          </th>
          <th className="border-b border-gray-200 px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-gray-600 dark:border-gray-700 dark:text-gray-400">
            {renderSortableHeader('Account', 'name')}
          </th>
          <th className="border-b border-gray-200 px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-gray-600 dark:border-gray-700 dark:text-gray-400">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Type', 'type')}
              {typeOptions && onTypeFilterChange && (
                <ColumnFilterDropdown
                  type="select"
                  options={typeOptions}
                  value={typeFilter}
                  onChange={(val) => onTypeFilterChange(val as string[])}
                  multiSelect
                  ariaLabel="Filter by type"
                />
              )}
            </div>
          </th>
          <th className="border-b border-gray-200 px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-gray-600 dark:border-gray-700 dark:text-gray-400">Acct #</th>
          <th className="border-b border-gray-200 px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.05em] text-gray-600 dark:border-gray-700 dark:text-gray-400">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Balance', 'balance')}
              {onBalanceRangeChange && (
                <ColumnFilterDropdown
                  type="range"
                  rangeValue={balanceRange}
                  onChange={(val) => onBalanceRangeChange(val as RangeValue | null)}
                  ariaLabel="Filter by balance"
                />
              )}
            </div>
          </th>
          {hasActions && (
            <th className="w-24 border-b border-gray-200 px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.05em] text-gray-600 max-md:w-16 dark:border-gray-700 dark:text-gray-400">
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {accounts.length === 0 ? (
          <tr>
            <td
              colSpan={hasActions ? 6 : 5}
              className="px-4 py-12 text-center italic text-gray-500 dark:text-gray-400"
            >
              No accounts match your filters. Try adjusting your criteria.
            </td>
          </tr>
        ) : accounts.map((account) => {
          const displayName = account.nickname || account.name
          const typeLabel = getAccountTypeLabel(account.type, account.subtype)
          const balanceFormatted = formatBalance(account.current_balance)
          const isNegativeBalance =
            account.current_balance !== undefined && account.current_balance < 0
          // Issue #679, #680: Check if linked item is disconnected
          const isDisconnected = account.linked_item_status === 'disconnected'

          return (
            <tr
              key={account.id}
              className={[
                'transition-colors duration-150',
                onRowClick ? 'account-table__row--clickable cursor-pointer' : '',
                account.is_hidden ? 'opacity-60' : '',
                isDisconnected
                  ? 'bg-amber-50/30 hover:bg-amber-50/50 dark:bg-[rgba(69,26,3,0.3)] dark:hover:bg-[rgba(69,26,3,0.5)]'
                  : 'hover:bg-gray-50 dark:hover:bg-gray-700/50',
              ].filter(Boolean).join(' ')}
              onClick={onRowClick ? () => onRowClick(account) : undefined}
            >
              <td className="min-w-[120px] border-b border-gray-200 px-4 py-3.5 text-sm align-middle text-gray-700 max-md:min-w-[100px] max-md:px-3 max-md:py-2.5 dark:border-gray-700 dark:text-gray-300">
                <span className="font-medium text-gray-900 dark:text-gray-100">
                  {account.institution_name || 'Unknown'}
                </span>
              </td>
              <td className="min-w-[180px] border-b border-gray-200 px-4 py-3.5 text-sm align-middle text-gray-700 max-md:min-w-[150px] max-md:px-3 max-md:py-2.5 dark:border-gray-700 dark:text-gray-300">
                <div className="flex items-center gap-2">
                  {account.custom_emoji && (
                    <span className="text-lg">
                      {account.custom_emoji}
                    </span>
                  )}
                  <span
                    className="font-medium text-gray-900 dark:text-gray-100"
                    style={
                      account.custom_color
                        ? { color: account.custom_color }
                        : undefined
                    }
                  >
                    {displayName}
                  </span>
                  {/* Issue #679, #680: Disconnected badge */}
                  {isDisconnected && (
                    <span
                      className="ml-2 inline-flex items-center gap-1 rounded bg-amber-100 px-1.5 py-0.5 text-[0.625rem] font-medium text-amber-700 dark:bg-[#451a03] dark:text-yellow-300"
                      title="Account disconnected - resubscribe to reconnect"
                    >
                      <Unplug className="h-2.5 w-2.5" aria-hidden="true" />
                      Disconnected
                    </span>
                  )}
                  {account.is_hidden && (
                    <span className="ml-2 inline-flex items-center gap-1 rounded bg-gray-100 px-1.5 py-0.5 text-[0.625rem] font-medium text-gray-600 dark:bg-gray-700 dark:text-gray-400">
                      <EyeOff className="h-2.5 w-2.5" aria-hidden="true" />
                      Hidden
                    </span>
                  )}
                </div>
              </td>
              <td className="border-b border-gray-200 px-4 py-3.5 text-sm align-middle text-gray-700 max-md:px-3 max-md:py-2.5 dark:border-gray-700 dark:text-gray-300">{typeLabel}</td>
              <td className="border-b border-gray-200 px-4 py-3.5 text-sm align-middle tabular-nums text-gray-500 max-md:px-3 max-md:py-2.5 dark:border-gray-700 dark:text-gray-400" data-ph-mask>
                {account.mask ? `••••${account.mask}` : '—'}
              </td>
              <td className="border-b border-gray-200 px-4 py-3.5 text-right text-sm align-middle max-md:px-3 max-md:py-2.5 dark:border-gray-700">
                <span
                  className={`font-semibold tabular-nums ${
                    isNegativeBalance
                      ? 'account-table__balance--negative text-red-600 dark:text-red-400'
                      : 'account-table__balance--positive text-gray-900 dark:text-gray-100'
                  }`}
                  data-ph-mask
                >
                  {balanceFormatted}
                </span>
              </td>
              {hasActions && (
                <td
                  className="border-b border-gray-200 px-4 py-3.5 text-right align-middle max-md:px-3 max-md:py-2.5 dark:border-gray-700"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="flex items-center justify-end gap-2">
                    {onEdit && (
                      <button
                        type="button"
                        onClick={() => onEdit(account)}
                        disabled={loading}
                        className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-gray-500 transition-colors duration-150 hover:bg-gray-100 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4 dark:hover:bg-gray-700 dark:hover:text-gray-300"
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
                        className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-gray-500 transition-colors duration-150 hover:bg-red-100 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4 dark:hover:bg-red-900 dark:hover:text-red-400"
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
