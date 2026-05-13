import { useMemo } from 'react'
import type { SpendingByMerchantResponse } from '@boostedbudget/api-client/models'
import { formatCurrencyFull } from '@/shared/charts'
import { DataTable } from '@/shared/components/DataTable'
import type { ColumnDef, SortState } from '@/shared/components/DataTable'
import type { SpendingMerchant } from '../types'

interface TopMerchantsTableProps {
  data: SpendingByMerchantResponse | null
  loading: boolean
  onMerchantClick?: (merchantName: string) => void
  className?: string
  sort: SortState
  onSortChange: (s: SortState) => void
}

const columns: ColumnDef<SpendingMerchant>[] = [
  { id: 'merchant_name', header: 'Merchant', sortable: true, key: 'merchant_name' },
  {
    id: 'total',
    header: 'Total',
    sortable: true,
    align: 'right',
    numeric: true,
    render: (row) => formatCurrencyFull(row.total),
  },
  {
    id: 'transaction_count',
    header: 'Transactions',
    sortable: true,
    align: 'right',
    numeric: true,
    render: (row) => <span className="text-muted-foreground">{row.transaction_count}</span>,
  },
  {
    id: 'percentage',
    header: '%',
    sortable: true,
    align: 'right',
    numeric: true,
    render: (row) => <span className="text-muted-foreground">{row.percentage.toFixed(1)}%</span>,
  },
]

function sortMerchants(merchants: SpendingMerchant[], sort: SortState): SpendingMerchant[] {
  if (!sort) return merchants
  return [...merchants].sort((a, b) => {
    const aVal = a[sort.columnId as keyof SpendingMerchant]
    const bVal = b[sort.columnId as keyof SpendingMerchant]
    const cmp =
      typeof aVal === 'string'
        ? (aVal as string).localeCompare(bVal as string)
        : (aVal as number) - (bVal as number)
    return sort.direction === 'asc' ? cmp : -cmp
  })
}

export function TopMerchantsTable({
  data,
  loading,
  onMerchantClick,
  className,
  sort,
  onSortChange,
}: TopMerchantsTableProps) {
  const sortedMerchants = useMemo(
    () => sortMerchants(data?.merchants ?? [], sort),
    [data?.merchants, sort]
  )

  if (loading) {
    return (
      <div
        className={className}
        aria-busy="true"
        aria-label="Loading merchants"
        data-testid="merchants-table"
      >
        <div className="h-48 animate-pulse rounded-lg bg-muted" />
      </div>
    )
  }

  return (
    <div className={className} data-testid="merchants-table">
      <DataTable<SpendingMerchant>
        data={sortedMerchants}
        columns={columns}
        rowKey={(row) => row.merchant_name}
        sort={sort}
        onSortChange={onSortChange}
        filters={{}}
        onFiltersChange={() => {}}
        maskedColumnIds={['total']}
        onRowClick={onMerchantClick ? (row) => onMerchantClick(row.merchant_name) : undefined}
        className="border-0 rounded-none bg-transparent"
        emptyState={
          <p className="px-4 py-3 text-sm text-muted-foreground">No merchant data for this period.</p>
        }
      />
    </div>
  )
}
