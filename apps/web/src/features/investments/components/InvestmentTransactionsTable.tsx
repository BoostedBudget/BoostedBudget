import { useState } from 'react'
import { useInfiniteInvestmentTransactions } from '@/features/investments/hooks/useInfiniteInvestmentTransactions'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState, FilterState } from '@/shared/components/DataTable'
import { formatPortfolioAmount } from '../utils/portfolioCurrency'
import type { InvestmentTransaction } from '@boostedbudget/api-client/models/investmentTransaction'
import { ActivityTypeChip } from '@/shared/components/ActivityTypeChip'
import {
  TXN_COLUMN_IDS,
  MASKED_COLUMN_IDS,
  buildInvestmentVirtualization,
} from './investmentTransactionsTable.constants'

// #1631 CX1: formatted in the currency the ROW states, not the dollar. An investment account can
// hold positions in several currencies at once, so the per-row code is the only one that can be
// right here -- there is no portfolio-level verdict that applies to an individual trade.
// fractionDigits 0 preserves exactly what formatCurrency rendered; this change moves the SYMBOL
// and nothing else.
const formatOrDefault = (
  value: number | null | undefined,
  row: { currency_code?: string },
): string => (value != null ? formatPortfolioAmount(value, row, 0) : '--')

interface InvestmentTransactionsTableProps {
  dateRange: { start_date: string; end_date: string }
  /**
   * Test seam: when true, virtualization is disabled so jsdom renders real rows.
   * Never set in production — only used by pii-coverage tests.
   */
  disableVirtualization?: boolean
}

export function InvestmentTransactionsTable({
  dateRange,
  disableVirtualization = false,
}: InvestmentTransactionsTableProps) {
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
      // ⚠ key is the SORT accessor, and it must move with the render (#1631). This column passed
      // the RAW Plaid `type` to a chip the transactions feed feeds a NORMALIZED value, so this
      // page could never show Dividend or Reinvest and disagreed with the feed about the same row.
      // Switching only the render would leave a row DISPLAYING "Dividend" while SORTING under
      // "cash", interleaved with rows displaying "Cash" — a visible sort/display disagreement on
      // exactly the rows this issue exists to fix.
      key: 'activity_type',
      // activity_type is `omitempty`, so it is absent from older fixtures and mocks; fall back to
      // the raw type rather than rendering the "Other" chip for a row we can still classify.
      render: (r) => <ActivityTypeChip activityType={r.activity_type ?? r.type} />,
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
      render: (r) => formatOrDefault(r.price, r),
    },
    {
      id: 'amount',
      header: 'Amount',
      key: 'amount',
      // sortable intentionally omitted: cursor-based infinite scroll loads pages in
      // date order; client-side sort would only reorder the loaded subset.
      numeric: true,
      align: 'right',
      render: (r) => formatPortfolioAmount(r.amount, r, 0),
    },
    {
      id: 'fees',
      header: 'Fees',
      key: 'fees',
      numeric: true,
      align: 'right',
      render: (r) => (r.fees != null && r.fees > 0 ? formatPortfolioAmount(r.fees, r, 0) : '--'),
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
      virtualization={
        disableVirtualization
          ? undefined
          : buildInvestmentVirtualization({ fetchNextPage, hasNextPage, isFetchingNextPage })
      }
    />
  )
}
