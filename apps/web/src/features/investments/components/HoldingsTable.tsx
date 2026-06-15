import { useState } from 'react'
import { useGetHoldings } from '@boostedbudget/api-client/endpoints/investments/investments'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState, FilterState } from '@/shared/components/DataTable'
import { formatCurrency, formatPercent } from '@/shared/charts/formatters'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'

const HOLDINGS_COLUMN_IDS = [
  'name',
  'ticker',
  'type',
  'quantity',
  'price',
  'market_value',
  'cost_basis',
  'gain_loss',
  'gain_loss_pct',
  'day_change',
  'day_change_pct',
  'account',
] as const

const MASKED_COLUMN_IDS = [
  'price',
  'market_value',
  'cost_basis',
  'gain_loss',
  'gain_loss_pct',
  'day_change',
  'day_change_pct',
]

export function HoldingsTable() {
  const [sortState, setSortState] = useState<SortState>(null)
  const [filters, setFilters] = useState<FilterState>({})

  const { data: holdingsData, isLoading, isError } = useGetHoldings()

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('portfolio-holdings', [...HOLDINGS_COLUMN_IDS])

  const holdings = holdingsData?.holdings ?? []

  const accountOptions = Array.from(new Set(holdings.map((h) => h.account_id))).map((id) => ({
    label: id,
    value: id,
  }))

  const columns: ColumnDef<HoldingView>[] = [
    {
      id: 'name',
      header: 'Security',
      key: 'security_name',
      render: (r) => <span>{r.security_name ?? r.security_id}</span>,
    },
    {
      id: 'ticker',
      header: 'Ticker',
      key: 'ticker_symbol',
    },
    {
      id: 'type',
      header: 'Type',
      key: 'security_type',
    },
    {
      id: 'quantity',
      header: 'Qty',
      key: 'quantity',
      numeric: true,
      align: 'right',
    },
    {
      id: 'price',
      header: 'Price',
      key: 'institution_price',
      render: (r) => (r.institution_price != null ? formatCurrency(r.institution_price) : '--'),
      numeric: true,
      align: 'right',
    },
    {
      id: 'market_value',
      header: 'Market Value',
      sortable: true,
      sortAccessor: (r) => r.institution_value ?? 0,
      render: (r) => (r.institution_value != null ? formatCurrency(r.institution_value) : '--'),
      numeric: true,
      align: 'right',
    },
    {
      id: 'cost_basis',
      header: 'Cost Basis',
      render: (r) => (r.cost_basis != null ? formatCurrency(r.cost_basis) : '--'),
      numeric: true,
      align: 'right',
    },
    {
      id: 'gain_loss',
      header: 'Gain/Loss ($)',
      sortable: true,
      sortAccessor: (r) => r.gain_loss ?? 0,
      render: (r) => (r.gain_loss != null ? formatCurrency(r.gain_loss) : '--'),
      numeric: true,
      align: 'right',
    },
    {
      id: 'gain_loss_pct',
      header: 'Gain/Loss (%)',
      sortable: true,
      sortAccessor: (r) => r.gain_loss_pct ?? 0,
      render: (r) =>
        r.gain_loss_pct != null ? formatPercent(r.gain_loss_pct / 100) : '--',
      numeric: true,
      align: 'right',
    },
    {
      id: 'day_change',
      header: 'Day Change ($)',
      sortable: true,
      sortAccessor: (r) => r.day_change ?? 0,
      render: (r) => (r.day_change != null ? formatCurrency(r.day_change) : '--'),
      numeric: true,
      align: 'right',
    },
    {
      id: 'day_change_pct',
      header: 'Day Change (%)',
      sortable: true,
      sortAccessor: (r) => r.day_change_pct ?? 0,
      render: (r) =>
        r.day_change_pct != null ? formatPercent(r.day_change_pct / 100) : '--',
      numeric: true,
      align: 'right',
    },
    {
      id: 'account',
      header: 'Account',
      key: 'account_id',
      hideable: true,
      filter:
        accountOptions.length > 0
          ? { type: 'select', options: accountOptions, multiSelect: true, searchable: true }
          : undefined,
    },
  ]

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading holdings" className="flex justify-center py-8">
        <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
      </div>
    )
  }

  if (isError) {
    return (
      <div className="text-sm text-destructive p-4">
        Unable to load holdings. Please refresh the page.
      </div>
    )
  }

  return (
    <DataTable
      data={holdings}
      columns={columns}
      rowKey={(r) => r.id}
      sort={sortState}
      onSortChange={setSortState}
      filters={filters}
      onFiltersChange={setFilters}
      maskedColumnIds={MASKED_COLUMN_IDS}
      emptyState={
        <p className="text-sm text-muted-foreground p-4">No holdings found.</p>
      }
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
    />
  )
}
