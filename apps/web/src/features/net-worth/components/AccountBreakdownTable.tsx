import { TrendingUp } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { TableHeaderCell } from '@/shared/components/TableHeaderCell'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { AccountBreakdownRow } from '../types'
import type { SortColumn, SortDirection } from '../hooks/useNetWorthBreakdownFilters'

const TH_BASE = 'border-b border-border px-4 py-3 text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground'
const TH_CLASS = `${TH_BASE} text-left`
const TH_RIGHT_CLASS = `${TH_BASE} text-right`
const TD_CLASS = 'border-b border-border px-4 py-3.5 text-sm align-middle text-foreground'
const TD_MUTED_CLASS = `${TD_CLASS} text-muted-foreground capitalize`
const TD_NUM_CLASS = `${TD_CLASS} text-right tabular-nums`

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

function AccountRows({
  rows,
  sideLabel,
}: {
  rows: AccountBreakdownRow[]
  sideLabel: string
}) {
  return (
    <>
      {rows.map((row) => (
        <tr key={row.id} className="hover:bg-muted/30 transition-colors">
          <td className={TD_CLASS}>{row.institutionName}</td>
          <td className={TD_CLASS}>{row.name}</td>
          <td className={TD_MUTED_CLASS}>{row.type}</td>
          <td className={TD_NUM_CLASS} data-ph-mask>
            {formatCurrency(row.currentBalance, row.currencyCode)}
          </td>
          <td
            className={TD_NUM_CLASS}
            data-testid={`breakdown-percent-${row.id}`}
            aria-label={`${row.percentOfSide.toFixed(1)}% of ${sideLabel}`}
          >
            {row.percentOfSide.toFixed(1)}%
          </td>
        </tr>
      ))}
    </>
  )
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
  if (total === 0 && rows.length === 0) return null

  const emptyLabel = title === 'Assets' ? 'asset' : 'liability'

  const getAriaSort = (col: SortColumn): React.AriaAttributes['aria-sort'] => {
    if (sortColumn !== col) return undefined
    return sortDirection === 'asc' ? 'ascending' : 'descending'
  }

  return (
    <div>
      <table className="w-full border-collapse">
        <caption className="text-left text-sm font-semibold text-foreground caption-top mb-2">
          {title} — {formatCurrency(total)}
        </caption>
        <thead>
          <tr className="bg-muted/50">
            <th scope="col" className={TH_CLASS} aria-sort={getAriaSort('institution')}>
              <TableHeaderCell
                label="Institution"
                sortable
                sortColumn="institution"
                currentSortColumn={sortColumn ?? undefined}
                currentSortDirection={sortDirection}
                onSort={(col) => onSort(col as SortColumn)}
                filterable={!!(institutionOptions && onInstitutionFilterChange)}
                filterType="select"
                filterOptions={institutionOptions}
                filterValue={institutionFilter}
                multiSelect
                searchable
                onFilterChange={(v) => onInstitutionFilterChange?.(v as string[])}
                filterAriaLabel="Filter by institution"
              />
            </th>
            <th scope="col" className={TH_CLASS} aria-sort={getAriaSort('name')}>
              <TableHeaderCell
                label="Account"
                sortable
                sortColumn="name"
                currentSortColumn={sortColumn ?? undefined}
                currentSortDirection={sortDirection}
                onSort={(col) => onSort(col as SortColumn)}
              />
            </th>
            <th scope="col" className={TH_CLASS} aria-sort={getAriaSort('type')}>
              <TableHeaderCell
                label="Type"
                sortable
                sortColumn="type"
                currentSortColumn={sortColumn ?? undefined}
                currentSortDirection={sortDirection}
                onSort={(col) => onSort(col as SortColumn)}
                filterable={!!(typeOptions && onTypeFilterChange)}
                filterType="select"
                filterOptions={typeOptions}
                filterValue={typeFilter}
                multiSelect
                onFilterChange={(v) => onTypeFilterChange?.(v as string[])}
                filterAriaLabel="Filter by type"
              />
            </th>
            <th scope="col" className={TH_RIGHT_CLASS} aria-sort={getAriaSort('balance')}>
              <TableHeaderCell
                label="Balance"
                className="w-full justify-end"
                sortable
                sortColumn="balance"
                currentSortColumn={sortColumn ?? undefined}
                currentSortDirection={sortDirection}
                onSort={(col) => onSort(col as SortColumn)}
                filterable={!!onBalanceRangeChange}
                filterType="range"
                filterValue={balanceRange}
                onFilterChange={(v) => onBalanceRangeChange?.(v as RangeValue | null)}
                filterAriaLabel="Filter by balance range"
              />
            </th>
            <th scope="col" className={TH_RIGHT_CLASS} aria-sort={getAriaSort('percent')}>
              <TableHeaderCell
                label={percentLabel}
                className="w-full justify-end"
                sortable
                sortColumn="percent"
                currentSortColumn={sortColumn ?? undefined}
                currentSortDirection={sortDirection}
                onSort={(col) => onSort(col as SortColumn)}
              />
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={5} className="px-4 py-6 text-sm text-muted-foreground text-center">
                No {emptyLabel} accounts match the current filters.
              </td>
            </tr>
          ) : (
            <AccountRows
              rows={rows}
              sideLabel={title.toLowerCase()}
            />
          )}
        </tbody>
      </table>
    </div>
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
