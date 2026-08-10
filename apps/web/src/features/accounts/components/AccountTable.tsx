import { useCallback } from 'react'
import { Edit, Trash2, EyeOff, Unplug, Building2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { EmptyState } from '@/components/ui/empty-state'
import type { Account } from '@boostedbudget/api-client/models'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, FilterState, SortState } from '@/shared/components/DataTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { getAccountTypeLabel, formatAccountBalance } from '../utils'

export type SortColumn = 'institution' | 'name' | 'type' | 'balance'
export type SortDirection = 'asc' | 'desc'

// eslint-disable-next-line react-refresh/only-export-components
export const ACCOUNT_COLUMN_IDS = ['institution', 'name', 'type', 'mask', 'balance'] as const

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
  sort?: SortState
  onSortChange?: (sort: SortState) => void
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


export function AccountTable({
  accounts,
  onEdit,
  onDelete,
  onRowClick,
  loading = false,
  sort = null,
  onSortChange,
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

  const { sizing, onSizingChange, columnOrder, onColumnOrderChange, columnVisibility, onColumnVisibilityChange } =
    useTablePreferences('accounts', [...ACCOUNT_COLUMN_IDS])

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
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultSize: 180,
      minSize: 120,
      maxSize: 240,
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
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultSize: 220,
      minSize: 160,
      render: (row) => {
        // linked_item_status === 'disconnected' can mean the subscription lapsed (#679/#680) OR that
        // orphan cleanup removed the Plaid item successfully; status alone can't tell which.
        const isDisconnected = row.linked_item_status === 'disconnected'
        const displayName = row.nickname || row.name
        return (
          <div className="flex items-center gap-2">
            {row.custom_emoji && <span className="text-lg">{row.custom_emoji}</span>}
            <span
              className="font-medium text-foreground"
              style={row.custom_color ? { color: row.custom_color } : undefined}
            >
              {displayName}
            </span>
            {isDisconnected && (
              <Badge
                variant="warning"
                size="sm"
                className="ml-2"
                title="This connection is no longer active"
              >
                <Unplug className="h-2.5 w-2.5" aria-hidden="true" />
                Disconnected
              </Badge>
            )}
            {row.is_hidden && (
              <Badge variant="secondary" size="sm" className="ml-2">
                <EyeOff className="h-2.5 w-2.5" aria-hidden="true" />
                Hidden
              </Badge>
            )}
          </div>
        )
      },
    },
    {
      id: 'type',
      header: 'Type',
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultSize: 140,
      minSize: 100,
      maxSize: 200,
      render: (row) => <>{getAccountTypeLabel(row.type, row.subtype, { surface: 'table' })}</>,
      filter:
        typeOptions && onTypeFilterChange
          ? { type: 'select', options: typeOptions, multiSelect: true }
          : undefined,
    },
    {
      id: 'mask',
      header: 'Acct #',
      numeric: true,
      resizable: true,
      reorderable: true,
      hideable: true,
      defaultSize: 100,
      minSize: 80,
      maxSize: 130,
      render: (row) => <>{row.mask ? `••••${row.mask}` : '—'}</>,
    },
    {
      id: 'balance',
      header: 'Balance',
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultPinned: 'right' as const,
      align: 'right',
      numeric: true,
      defaultSize: 160,
      minSize: 120,
      maxSize: 220,
      render: (row) => {
        const isNegative = row.current_balance !== undefined && row.current_balance < 0
        return (
          <span
            className={cn(
              'font-semibold tabular-nums',
              isNegative
                ? 'account-table__balance--negative text-destructive'
                : 'account-table__balance--positive text-foreground'
            )}
          >
            {formatAccountBalance(row.current_balance, row.currency_code, { empty: '—' })}
          </span>
        )
      },
      filter: onBalanceRangeChange ? { type: 'range', unit: 'currency' } : undefined,
    },
  ]

  if (!loading && accounts.length === 0 && !hasActiveFilters) {
    return (
      <EmptyState
        className={className}
        icon={<Building2 />}
        title="No accounts found"
        description="Connect a bank account to get started."
      />
    )
  }

  return (
    <DataTable<Account>
      data={accounts}
      columns={columns}
      rowKey={(row) => row.id}
      sort={sort}
      onSortChange={onSortChange!}
      filters={filters}
      onFiltersChange={handleFiltersChange}
      loading={loading}
      skeletonRows={5}
      onRowClick={onRowClick}
      stickyHeader
      stickyHeaderTopOffset="4rem"
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
      rowClassName={(row) => {
        const isDisconnected = row.linked_item_status === 'disconnected'
        return cn(
          onRowClick && 'account-table__row--clickable',
          row.is_hidden && 'opacity-60',
          isDisconnected &&
            'bg-warning/10 hover:bg-warning/20'
        )
      }}
      className={cn(loading && 'account-table--loading', className)}
      maskedColumnIds={['institution', 'name', 'mask', 'balance']}
      emptyState={
        <EmptyState
          size="compact"
          icon={<Building2 />}
          title="No accounts match your filters"
          description="Try adjusting your criteria."
        />
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
                    className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
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
