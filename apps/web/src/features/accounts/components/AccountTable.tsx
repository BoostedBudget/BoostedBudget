import { useCallback } from 'react'
import { Edit, Trash2, EyeOff, Unplug } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Account } from '@boostedbudget/api-client/models'
import { DataTable } from '@/shared/components/DataTable'
import type { ColumnDef, FilterState, SortState } from '@/shared/components/DataTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'

export type SortColumn = 'institution' | 'name' | 'type' | 'balance'
export type SortDirection = 'asc' | 'desc'

// eslint-disable-next-line react-refresh/only-export-components
export const ACCOUNT_SORTABLE_COLUMNS = new Set([
  'institution',
  'name',
  'type',
  'balance',
] as const)

export interface AccountTableProps {
  accounts: Account[]
  onEdit?: (account: Account) => void
  onDelete?: (account: Account) => void
  onRowClick?: (account: Account) => void
  loading?: boolean
  sortable?: boolean
  sortColumn?: SortColumn
  sortDirection?: SortDirection
  onSort?: (column: SortColumn, direction: SortDirection) => void
  className?: string
  institutionOptions?: FilterOption[]
  institutionFilter?: string[]
  onInstitutionFilterChange?: (value: string[]) => void
  typeOptions?: FilterOption[]
  typeFilter?: string[]
  onTypeFilterChange?: (value: string[]) => void
  balanceRange?: RangeValue | null
  onBalanceRangeChange?: (value: RangeValue | null) => void
}

function formatBalance(balance: number | undefined): string {
  if (balance === undefined || balance === null) return '—'
  const absBalance = Math.abs(balance)
  const formatted = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(absBalance)
  return balance < 0 ? `-${formatted}` : formatted
}

function getAccountTypeLabel(type: string, subtype?: string): string {
  if (subtype) {
    switch (subtype.toLowerCase()) {
      case 'checking': return 'Checking'
      case 'savings': return 'Savings'
      case 'credit card': return 'Credit Card'
      case 'money market': return 'Money Market'
      case 'cd': return 'CD'
      case 'paypal': return 'PayPal'
      case 'brokerage': return 'Brokerage'
      case '401k': return '401(k)'
      case 'ira': return 'IRA'
      case 'roth': return 'Roth IRA'
      case 'mortgage': return 'Mortgage'
      case 'auto': return 'Auto Loan'
      case 'student': return 'Student Loan'
      default: return subtype.charAt(0).toUpperCase() + subtype.slice(1)
    }
  }
  switch (type.toLowerCase()) {
    case 'depository': return 'Bank Account'
    case 'credit': return 'Credit'
    case 'investment': return 'Investment'
    case 'loan': return 'Loan'
    case 'other': return 'Other'
    default: return type.charAt(0).toUpperCase() + type.slice(1)
  }
}

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
  const hasActiveFilters =
    (institutionFilter?.length ?? 0) > 0 ||
    (typeFilter?.length ?? 0) > 0 ||
    balanceRange != null

  const handleSortChange = useCallback(
    (newSort: SortState) => {
      if (!onSort || !sortable) return
      if (newSort) {
        onSort(newSort.columnId as SortColumn, newSort.direction)
      } else if (sortColumn) {
        onSort(sortColumn, sortDirection === 'asc' ? 'desc' : 'asc')
      }
    },
    [onSort, sortable, sortColumn, sortDirection]
  )

  const sort: SortState =
    sortColumn && sortable ? { columnId: sortColumn, direction: sortDirection } : null

  const filters: FilterState = {
    institution: institutionFilter ?? [],
    type: typeFilter ?? [],
    balance: balanceRange ?? null,
  }

  const handleFiltersChange = useCallback(
    (newFilters: FilterState) => {
      if (newFilters.institution !== filters.institution) {
        onInstitutionFilterChange?.(newFilters.institution as string[])
      }
      if (newFilters.type !== filters.type) {
        onTypeFilterChange?.(newFilters.type as string[])
      }
      if (newFilters.balance !== filters.balance) {
        onBalanceRangeChange?.(newFilters.balance as RangeValue | null)
      }
    },
    [filters.institution, filters.type, filters.balance, onInstitutionFilterChange, onTypeFilterChange, onBalanceRangeChange]
  )

  const columns: ColumnDef<Account>[] = [
    {
      id: 'institution',
      header: 'Institution',
      sortable,
      widthClass: 'min-w-[120px] max-md:min-w-[100px]',
      render: (row) => (
        <span className="font-medium text-foreground">
          {row.institution_name || 'Unknown'}
        </span>
      ),
      filter:
        institutionOptions && onInstitutionFilterChange
          ? { type: 'select', options: institutionOptions, multiSelect: true, searchable: true }
          : undefined,
    },
    {
      id: 'name',
      header: 'Account',
      sortable,
      widthClass: 'min-w-[180px] max-md:min-w-[150px]',
      render: (row) => {
        const isDisconnected = row.linked_item_status === 'disconnected'
        const displayName = row.nickname || row.name
        return (
          <div className="flex items-center gap-2">
            {row.custom_emoji && (
              <span className="text-lg">{row.custom_emoji}</span>
            )}
            <span
              className="font-medium text-foreground"
              style={row.custom_color ? { color: row.custom_color } : undefined}
            >
              {displayName}
            </span>
            {isDisconnected && (
              <span
                className="ml-2 inline-flex items-center gap-1 rounded bg-amber-100 px-1.5 py-0.5 text-[0.625rem] font-medium text-amber-700 dark:bg-[#451a03] dark:text-yellow-300"
                title="Account disconnected - resubscribe to reconnect"
              >
                <Unplug className="h-2.5 w-2.5" aria-hidden="true" />
                Disconnected
              </span>
            )}
            {row.is_hidden && (
              <span className="ml-2 inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[0.625rem] font-medium text-muted-foreground">
                <EyeOff className="h-2.5 w-2.5" aria-hidden="true" />
                Hidden
              </span>
            )}
          </div>
        )
      },
    },
    {
      id: 'type',
      header: 'Type',
      sortable,
      render: (row) => <>{getAccountTypeLabel(row.type, row.subtype)}</>,
      filter:
        typeOptions && onTypeFilterChange
          ? { type: 'select', options: typeOptions, multiSelect: true }
          : undefined,
    },
    {
      id: 'mask',
      header: 'Acct #',
      numeric: true,
      render: (row) => <>{row.mask ? `••••${row.mask}` : '—'}</>,
    },
    {
      id: 'balance',
      header: 'Balance',
      sortable,
      align: 'right',
      numeric: true,
      render: (row) => {
        const isNegative = row.current_balance !== undefined && row.current_balance < 0
        return (
          <span
            className={cn(
              'font-semibold tabular-nums',
              isNegative
                ? 'account-table__balance--negative text-red-600 dark:text-red-400'
                : 'account-table__balance--positive text-foreground'
            )}
          >
            {formatBalance(row.current_balance)}
          </span>
        )
      },
      filter: onBalanceRangeChange ? { type: 'range', unit: 'currency' } : undefined,
    },
  ]

  if (accounts.length === 0 && !hasActiveFilters) {
    return (
      <div
        className={cn(
          'rounded-lg border border-dashed border-border bg-card px-6 py-12 text-center text-muted-foreground',
          className
        )}
      >
        <p>No accounts found. Connect a bank account to get started.</p>
      </div>
    )
  }

  return (
    <DataTable<Account>
      data={accounts}
      columns={columns}
      rowKey={(row) => row.id}
      sort={sort}
      onSortChange={handleSortChange}
      filters={filters}
      onFiltersChange={handleFiltersChange}
      loading={loading}
      onRowClick={onRowClick}
      rowClassName={(row) => {
        const isDisconnected = row.linked_item_status === 'disconnected'
        return cn(
          onRowClick && 'account-table__row--clickable',
          row.is_hidden && 'opacity-60',
          isDisconnected &&
            'bg-amber-50/30 hover:bg-amber-50/50 dark:bg-[rgba(69,26,3,0.3)] dark:hover:bg-[rgba(69,26,3,0.5)]'
        )
      }}
      className={cn(loading && 'account-table--loading', className)}
      maskedColumnIds={['mask', 'balance']}
      emptyState={
        <p className="py-12 px-4 text-center text-muted-foreground italic">
          No accounts match your filters. Try adjusting your criteria.
        </p>
      }
      rowActions={
        hasActions
          ? (row) => (
              <div className="flex items-center justify-end gap-2">
                {onEdit && (
                  <button
                    type="button"
                    onClick={() => onEdit(row)}
                    disabled={loading}
                    className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
                    aria-label="Edit account"
                    title="Edit account"
                  >
                    <Edit aria-hidden="true" />
                  </button>
                )}
                {onDelete && (
                  <button
                    type="button"
                    onClick={() => onDelete(row)}
                    disabled={loading}
                    className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-red-100 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4 dark:hover:bg-red-900 dark:hover:text-red-400"
                    aria-label="Delete account"
                    title="Delete account"
                  >
                    <Trash2 aria-hidden="true" />
                  </button>
                )}
              </div>
            )
          : undefined
      }
    />
  )
}
