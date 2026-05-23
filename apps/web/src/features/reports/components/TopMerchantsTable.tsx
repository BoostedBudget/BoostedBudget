import { useMemo } from 'react'
import type { SpendingByMerchantResponse } from '@boostedbudget/api-client/models'
import { formatCurrencyFull } from '@/shared/charts'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
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

// eslint-disable-next-line react-refresh/only-export-components
export const TOP_MERCHANTS_COLUMN_IDS = ['merchant_name', 'total', 'transaction_count', 'percentage'] as const

const columns: ColumnDef<SpendingMerchant>[] = [
  { id: 'merchant_name', header: 'Merchant', sortable: true, resizable: true, reorderable: true, key: 'merchant_name', defaultSize: 280, minSize: 180 },
  {
    id: 'total',
    header: 'Total',
    sortable: true,
    resizable: true,
    reorderable: true,
    align: 'right',
    numeric: true,
    defaultSize: 160,
    minSize: 120,
    maxSize: 220,
    render: (row) => formatCurrencyFull(row.total),
  },
  {
    id: 'transaction_count',
    header: 'Transactions',
    sortable: true,
    resizable: true,
    reorderable: true,
    hideable: true,
    align: 'right',
    numeric: true,
    defaultSize: 160,
    minSize: 120,
    maxSize: 220,
    render: (row) => <span className="text-muted-foreground">{row.transaction_count}</span>,
  },
  {
    id: 'percentage',
    header: '%',
    sortable: true,
    resizable: true,
    reorderable: true,
    align: 'right',
    numeric: true,
    defaultSize: 130,
    minSize: 100,
    maxSize: 180,
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
  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('top-merchants', [...TOP_MERCHANTS_COLUMN_IDS])

  const sortedMerchants = useMemo(
    () => sortMerchants(data?.merchants ?? [], sort),
    [data?.merchants, sort]
  )

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
        loading={loading}
        skeletonRows={5}
        maskedColumnIds={['merchant_name', 'total']}
        onRowClick={onMerchantClick ? (row) => onMerchantClick(row.merchant_name) : undefined}
        columnSizing={sizing}
        onColumnSizingChange={onSizingChange}
        columnOrder={columnOrder}
        onColumnOrderChange={onColumnOrderChange}
        columnVisibility={columnVisibility}
        onColumnVisibilityChange={onColumnVisibilityChange}
        className="border-0 rounded-none bg-transparent"
        emptyState={
          <p className="px-4 py-3 text-sm text-muted-foreground">No merchant data for this period.</p>
        }
      />
    </div>
  )
}
