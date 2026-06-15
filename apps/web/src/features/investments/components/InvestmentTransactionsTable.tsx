import { useState } from 'react'
import { useInfiniteInvestmentTransactions } from '@/features/investments/hooks/useInfiniteInvestmentTransactions'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState, FilterState } from '@/shared/components/DataTable'
import { formatCurrency } from '@/shared/charts/formatters'
import type { InvestmentTransaction } from '@boostedbudget/api-client/models/investmentTransaction'

const formatOrDefault = (value: number | null | undefined): string =>
  value != null ? formatCurrency(value) : '--'

interface InvestmentTransactionsTableProps {
  dateRange: { start_date: string; end_date: string }
}

const TXN_COLUMN_IDS = [
  'date',
  'type',
  'security',
  'quantity',
  'price',
  'amount',
  'fees',
] as const

const MASKED_COLUMN_IDS = ['price', 'amount', 'fees']

export function InvestmentTransactionsTable({ dateRange }: InvestmentTransactionsTableProps) {
  const [sortState, setSortState] = useState<SortState>(null)
  const [filters, setFilters] = useState<FilterState>({})

  const { transactions, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading, isError } =
    useInfiniteInvestmentTransactions({
      start_date: dateRange.start_date,
      end_date: dateRange.end_date,
    })

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('portfolio-investment-transactions', [...TXN_COLUMN_IDS])

  const columns: ColumnDef<InvestmentTransaction>[] = [
    {
      id: 'date',
      header: 'Date',
      key: 'date',
      render: (r) => r.date,
    },
    {
      id: 'type',
      header: 'Type',
      key: 'type',
      render: (r) => r.type[0].toUpperCase() + r.type.slice(1),
    },
    {
      id: 'security',
      header: 'Security',
      key: 'name',
    },
    {
      id: 'quantity',
      header: 'Qty',
      key: 'quantity',
      numeric: true,
      align: 'right',
      render: (r) => (r.quantity != null ? r.quantity.toFixed(4) : '--'),
    },
    {
      id: 'price',
      header: 'Price',
      key: 'price',
      numeric: true,
      align: 'right',
      render: (r) => formatOrDefault(r.price),
    },
    {
      id: 'amount',
      header: 'Amount',
      key: 'amount',
      sortable: true,
      sortAccessor: (r) => r.amount,
      numeric: true,
      align: 'right',
      render: (r) => formatCurrency(r.amount),
    },
    {
      id: 'fees',
      header: 'Fees',
      key: 'fees',
      numeric: true,
      align: 'right',
      render: (r) => (r.fees != null && r.fees > 0 ? formatCurrency(r.fees) : '--'),
    },
  ]

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading transactions" className="flex justify-center py-8">
        <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
      </div>
    )
  }

  if (isError) {
    return (
      <div className="text-sm text-destructive p-4">
        Unable to load investment transactions. Please refresh the page.
      </div>
    )
  }

  return (
    <DataTable
      data={transactions}
      columns={columns}
      rowKey={(r) => r.id}
      sort={sortState}
      onSortChange={setSortState}
      filters={filters}
      onFiltersChange={setFilters}
      maskedColumnIds={MASKED_COLUMN_IDS}
      emptyState={
        <p className="text-sm text-muted-foreground p-4">No investment transactions found.</p>
      }
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
      virtualization={{
        enabled: true,
        estimateRowHeight: 56,
        overscan: 8,
        dynamicRowHeight: true,
        scrollContainerHeight: 'calc(100dvh - 240px)',
        onScrollEnd: fetchNextPage,
        hasMore: hasNextPage,
        isFetchingMore: isFetchingNextPage,
        prefetchRows: 5,
      }}
    />
  )
}
