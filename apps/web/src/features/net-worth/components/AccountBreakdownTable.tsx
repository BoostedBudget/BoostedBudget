import { useCallback } from 'react'
import { TrendingUp } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { DataTable } from '@/shared/components/DataTable'
import type { ColumnDef, FilterState, SortState } from '@/shared/components/DataTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { AccountBreakdownRow } from '../types'
import type { SortColumn, SortDirection } from '../hooks/useNetWorthBreakdownFilters'

interface AccountBreakdownTableProps {
  assets: AccountBreakdownRow[]
  liabilities: AccountBreakdownRow[]
  assetsTotal: number
  liabilitiesTotal: number
  loading: boolean
  error: Error | null
  sortColumn: SortColumn | null
  sortDirection: SortDirection
  onSort: (column: SortColumn) => void
  institutionOptions?: FilterOption[]
  typeOptions?: FilterOption[]
  institutionFilter?: string[]
  typeFilter?: string[]
  onInstitutionFilterChange?: (value: string[]) => void
  onTypeFilterChange?: (value: string[]) => void
  balanceRange?: RangeValue | null
  onBalanceRangeChange?: (value: RangeValue | null) => void
}

function formatCurrency(value: number, currencyCode = 'USD'): string {
  return Math.abs(value).toLocaleString('en-US', {
    style: 'currency',
    currency: currencyCode,
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

interface AccountSectionProps extends Pick<
  AccountBreakdownTableProps,
  | 'sortColumn'
  | 'sortDirection'
  | 'onSort'
  | 'institutionOptions'
  | 'typeOptions'
  | 'institutionFilter'
  | 'typeFilter'
  | 'onInstitutionFilterChange'
  | 'onTypeFilterChange'
  | 'balanceRange'
  | 'onBalanceRangeChange'
> {
  title: string
  rows: AccountBreakdownRow[]
  total: number
  percentLabel: string
}

function AccountSection({
  title,
  rows,
  total,
  percentLabel,
  sortColumn,
  sortDirection,
  onSort,
  institutionOptions,
  typeOptions,
  institutionFilter,
  typeFilter,
  onInstitutionFilterChange,
  onTypeFilterChange,
  balanceRange,
  onBalanceRangeChange,
}: AccountSectionProps) {
  const emptyLabel = title === 'Assets' ? 'asset' : 'liability'

  const sort: SortState = sortColumn
    ? { columnId: sortColumn, direction: sortDirection }
    : null

  const filters: FilterState = {
    institution: institutionFilter ?? [],
    type: typeFilter ?? [],
    balance: balanceRange ?? null,
  }

  const handleSortChange = useCallback(
    (newState: SortState) => {
      if (newState === null) {
        // DataTable 3-state cycle reached null — fall back to cycling via onSort
        // (hook toggles desc→asc, preserving 2-state behavior)
        if (sortColumn) onSort(sortColumn)
      } else {
        onSort(newState.columnId as SortColumn)
      }
    },
    [sortColumn, onSort]
  )

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
    [
      filters.institution,
      filters.type,
      filters.balance,
      onInstitutionFilterChange,
      onTypeFilterChange,
      onBalanceRangeChange,
    ]
  )

  if (total === 0 && rows.length === 0) return null

  const columns: ColumnDef<AccountBreakdownRow>[] = [
    {
      id: 'institution',
      header: 'Institution',
      sortable: true,
      key: 'institutionName',
      filter:
        institutionOptions && onInstitutionFilterChange
          ? {
              type: 'select',
              options: institutionOptions,
              multiSelect: true,
              searchable: true,
            }
          : undefined,
    },
    {
      id: 'name',
      header: 'Account',
      sortable: true,
      key: 'name',
    },
    {
      id: 'type',
      header: 'Type',
      sortable: true,
      render: (row) => (
        <span className="text-muted-foreground capitalize">{row.type}</span>
      ),
      filter:
        typeOptions && onTypeFilterChange
          ? { type: 'select', options: typeOptions, multiSelect: true }
          : undefined,
    },
    {
      id: 'balance',
      header: 'Balance',
      sortable: true,
      align: 'right',
      numeric: true,
      render: (row) => formatCurrency(row.currentBalance, row.currencyCode),
      filter: onBalanceRangeChange ? { type: 'range' } : undefined,
    },
    {
      id: 'percent',
      header: percentLabel,
      sortable: true,
      align: 'right',
      numeric: true,
      render: (row) => (
        <span
          data-testid={`breakdown-percent-${row.id}`}
          aria-label={`${row.percentOfSide.toFixed(1)}% of ${title.toLowerCase()}`}
        >
          {row.percentOfSide.toFixed(1)}%
        </span>
      ),
    },
  ]

  return (
    <DataTable<AccountBreakdownRow>
      data={rows}
      columns={columns}
      rowKey={(row) => row.id}
      sort={sort}
      onSortChange={handleSortChange}
      filters={filters}
      onFiltersChange={handleFiltersChange}
      caption={`${title} — ${formatCurrency(total)}`}
      maskedColumnIds={['balance']}
      emptyState={
        <span className="block px-4 py-6 text-sm text-muted-foreground text-center">
          No {emptyLabel} accounts match the current filters.
        </span>
      }
    />
  )
}

export function AccountBreakdownTable({
  assets,
  liabilities,
  assetsTotal,
  liabilitiesTotal,
  loading,
  error,
  sortColumn,
  sortDirection,
  onSort,
  institutionOptions,
  typeOptions,
  institutionFilter,
  typeFilter,
  onInstitutionFilterChange,
  onTypeFilterChange,
  balanceRange,
  onBalanceRangeChange,
}: AccountBreakdownTableProps) {
  if (loading) {
    return (
      <div role="status" aria-label="Loading account breakdown" className="flex justify-center py-8">
        <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-primary" />
      </div>
    )
  }

  if (error) {
    return <p className="text-sm text-destructive py-4">{error.message}</p>
  }

  if (assetsTotal === 0 && liabilitiesTotal === 0 && assets.length === 0 && liabilities.length === 0) {
    return (
      <EmptyState
        icon={<TrendingUp />}
        title="No accounts found"
        description="Connect a bank account to see your breakdown."
      />
    )
  }

  const sectionProps = {
    sortColumn,
    sortDirection,
    onSort,
    institutionOptions,
    typeOptions,
    institutionFilter,
    typeFilter,
    onInstitutionFilterChange,
    onTypeFilterChange,
    balanceRange,
    onBalanceRangeChange,
  }

  return (
    <div className="overflow-x-auto space-y-6">
      <AccountSection
        title="Assets"
        rows={assets}
        total={assetsTotal}
        percentLabel="% of Assets"
        {...sectionProps}
      />
      <AccountSection
        title="Liabilities"
        rows={liabilities}
        total={liabilitiesTotal}
        percentLabel="% of Liabilities"
        {...sectionProps}
      />
    </div>
  )
}
