import { useCallback } from 'react'
import { Edit, Trash2, EyeOff, Unplug, Building2, Archive } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { EmptyState } from '@/components/ui/empty-state'
import type { Account } from '@boostedbudget/api-client/models'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, FilterState, SortState } from '@/shared/components/DataTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'
import {
  getAccountTypeLabel,
  formatAccountBalance,
  getAccountClosedDate,
  getAccountLastKnownBalance,
} from '../utils'
import { formatHistoryLabel } from '@/shared/dates/utils'
import { isInactive } from '../hooks/useActiveAccounts'

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
  // Issue #1569, Task 6: disconnect (primary, retains history) and purge (destructive, reachable
  // only for a closed row) replace the single onDelete verb.
  onDisconnect?: (account: Account) => void
  onPurge?: (account: Account) => void
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
  onDisconnect,
  onPurge,
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
  const hasActions = !!(onEdit || onDisconnect || onPurge)
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
        const closedOn = getAccountClosedDate(row)
        return (
          <div className="flex items-center gap-2">
            {row.custom_emoji && <span className="text-lg">{row.custom_emoji}</span>}
            <span
              className="font-medium text-foreground"
              style={row.custom_color ? { color: row.custom_color } : undefined}
            >
              {displayName}
            </span>
            {/*
              Fix round 1, item 1: the badge carries the closure DATE. T7 removed the balance
              cell's "as of <date>" caption because the balance it labelled is never sent — but
              that caption was the only place in the whole app the closure date appeared, and on a
              surface whose thesis is "your history is retained", losing the WHEN is the wrong
              direction. It lives here rather than back in the balance cell because it is a fact
              about the ACCOUNT, not about a (non-existent) figure, and because a third line would
              not fit a 160px numeric column.
            */}
            {/*
              ⚠ #1569 T8.5: the tooltip must stay true for all three `removed` causes
              (user_disconnect / provider_absent / link_removed) plus `replaced` and `expired`.
              accounts.removed_reason is not on the wire, so telling the user THEY disconnected it
              was wrong for two of the five states. Keep this copy in step with AccountCard.tsx.
            */}
            {isInactive(row) && (
              <Badge
                variant="secondary"
                size="sm"
                className="ml-2"
                title="This account is no longer active — it may have been disconnected, replaced by a re-link, or stopped being reported by your bank. Its history is retained."
              >
                <Archive className="h-2.5 w-2.5" aria-hidden="true" />
                {closedOn ? `Closed ${formatHistoryLabel(closedOn, 'MMM d, yyyy')}` : 'Closed'}
              </Badge>
            )}
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
        // Issue #1580: a closed account carries no current balance — accountBalanceProjection
        // emits NULL for every terminal status — so the cell states that rather than leaving a
        // bare em dash. It replaces the former "as of <date>" caption, which was built for a
        // world where a closed account still held a frozen figure and ended up rendering
        // "— / as of Jun 4, 2025": a date attached to nothing.
        //
        // Driven off `status` (isInactive), not off a missing balance, so it says "closed" only
        // when the account is closed; an active account whose balance has not synced yet still
        // gets a plain em dash and no claim about why.
        const isClosed = isInactive(row)
        const isNegative = !isClosed && row.current_balance !== undefined && row.current_balance < 0
        // Issue #1591: the sub-line becomes a DATED historical figure when one exists. "No current
        // balance" is what we say when there is nothing else to say — it is a statement about the
        // absence, so a real last-known balance replaces it rather than stacking a third line into
        // a 160px numeric column. The primary line stays an em dash either way: this figure is not
        // a current balance and must never occupy the slot where one would appear.
        const lastKnown = getAccountLastKnownBalance(row)
        return (
          <div className="flex flex-col items-end">
            <span
              className={cn(
                'font-semibold tabular-nums',
                isNegative
                  ? 'account-table__balance--negative text-destructive'
                  : 'account-table__balance--positive text-foreground'
              )}
            >
              {isClosed
                ? '—'
                : formatAccountBalance(row.current_balance, row.currency_code, { empty: '—' })}
            </span>
            {isClosed &&
              (lastKnown ? (
                /*
                  One element, two lines: to a screen reader this reads as the single phrase
                  "Last known $4,321.00 as of Feb 24, 2026", so the historical qualifier can never
                  be read apart from the number it qualifies. The `title` is DE-PII'd on purpose —
                  attributes are recorded verbatim by session replay and cannot be masked, so the
                  amount must not appear in one (the whole <td> is masked via `maskedColumnIds`,
                  which covers the text and not the attribute).
                */
                <span
                  className="text-xs font-normal text-muted-foreground text-right"
                  title="The last balance recorded before this account closed"
                >
                  <span className="block">
                    Last known{' '}
                    {formatAccountBalance(lastKnown.amount, row.currency_code, { empty: '—' })}
                  </span>
                  <span className="block">
                    as of {formatHistoryLabel(lastKnown.at, 'MMM d, yyyy')}
                  </span>
                </span>
              ) : (
                <span className="text-xs font-normal text-muted-foreground">No current balance</span>
              ))}
          </div>
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
          ? (row) => {
              // Issue #1569, Task 6: a closed row's only sensible action is purge — Edit and
              // Disconnect both act on a LIVE account (a Task 5 reviewer finding: closed rows
              // previously kept working Edit/Delete actions that harmlessly re-ran
              // UpdateStatus(removed)).
              const closed = isInactive(row)
              return (
                <div className="flex items-center justify-end gap-2">
                  {closed ? (
                    onPurge && (
                      <button
                        type="button"
                        onClick={() => onPurge(row)}
                        disabled={loading}
                        className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
                        aria-label="Permanently delete account"
                        title="Permanently delete this account and its history"
                      >
                        <Trash2 aria-hidden="true" />
                      </button>
                    )
                  ) : (
                    <>
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
                      {onDisconnect && (
                        <button
                          type="button"
                          onClick={() => onDisconnect(row)}
                          disabled={loading}
                          className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
                          aria-label="Disconnect account"
                          title="Stop syncing this account, retaining its history"
                        >
                          <Unplug aria-hidden="true" />
                        </button>
                      )}
                    </>
                  )}
                </div>
              )
            }
          : undefined
      }
    />
  )
}
