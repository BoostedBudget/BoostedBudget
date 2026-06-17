import { useState, useMemo } from 'react'
import { useGetHoldings } from '@boostedbudget/api-client/endpoints/investments/investments'
import { useActiveAccounts } from '@/features/accounts/hooks'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState, FilterState } from '@/shared/components/DataTable'
import type { FilterOption } from '@/shared/components/ColumnFilterDropdown'
import { formatCurrency, formatPercent } from '@/shared/charts/formatters'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'

const formatOrDefault = (value: number | null | undefined): string =>
  value != null ? formatCurrency(value) : '--'

// Backend returns percentage values already (e.g. 25.0 means 25%); no /100 conversion needed
const formatPercentOrDefault = (value: number | null | undefined): string =>
  value != null ? formatPercent(value) : '--'

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
  'quantity',
  'price',
  'market_value',
  'cost_basis',
  'gain_loss',
  'gain_loss_pct',
  'day_change',
  'day_change_pct',
  'account',
]

export function HoldingsTable() {
  const [sortState, setSortState] = useState<SortState>(null)
  const [filters, setFilters] = useState<FilterState>({})

  const { data: holdingsData, isLoading, isError } = useGetHoldings()
  const { accounts } = useActiveAccounts()

  // Build id→name map for resolving account UUIDs to human-readable names.
  const accountNameById = useMemo(() => {
    const map = new Map<string, string>()
    accounts?.forEach((a) => map.set(a.id, a.nickname ?? a.name))
    return map
  }, [accounts])

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('portfolio-holdings', [...HOLDINGS_COLUMN_IDS])

  const rawHoldings = holdingsData?.holdings

  // DataTable uses manualFiltering=true — apply account filter client-side.
  const filteredHoldings = useMemo(() => {
    const all = rawHoldings ?? []
    const accountFilter = filters['account']
    if (!accountFilter || !Array.isArray(accountFilter) || accountFilter.length === 0) return all
    return all.filter((h) => (accountFilter as string[]).includes(h.account_id))
  }, [rawHoldings, filters])

  // DataTable uses manualSorting=true — apply client-side sort before rendering.
  const sortedHoldings = useMemo(() => {
    if (!sortState) return filteredHoldings
    const accessors: Partial<Record<string, (r: HoldingView) => number>> = {
      market_value: (r) => r.institution_value ?? 0,
      gain_loss: (r) => r.gain_loss ?? 0,
      gain_loss_pct: (r) => r.gain_loss_pct ?? 0,
      day_change: (r) => r.day_change ?? 0,
      day_change_pct: (r) => r.day_change_pct ?? 0,
    }
    const accessor = accessors[sortState.columnId]
    if (!accessor) return filteredHoldings
    return [...filteredHoldings].sort((a, b) => {
      const cmp = accessor(a) < accessor(b) ? -1 : accessor(a) > accessor(b) ? 1 : 0
      return sortState.direction === 'asc' ? cmp : -cmp
    })
  }, [filteredHoldings, sortState])

  // accountOptions derived from all holdings (unfiltered) so the dropdown always shows
  // every account even while a filter is active.
  const holdings = rawHoldings ?? []

  const accountOptions: FilterOption[] = Array.from(new Set(holdings.map((h) => h.account_id))).map((id) => ({
    label: accountNameById.get(id) ?? id,
    value: id,
    masked: true,
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
      render: (r) => formatOrDefault(r.institution_price),
      numeric: true,
      align: 'right',
    },
    {
      id: 'market_value',
      header: 'Market Value',
      sortable: true,
      sortAccessor: (r) => r.institution_value ?? 0,
      render: (r) => formatOrDefault(r.institution_value),
      numeric: true,
      align: 'right',
    },
    {
      id: 'cost_basis',
      header: 'Cost Basis',
      render: (r) => formatOrDefault(r.cost_basis),
      numeric: true,
      align: 'right',
    },
    {
      id: 'gain_loss',
      header: 'Gain/Loss ($)',
      sortable: true,
      sortAccessor: (r) => r.gain_loss ?? 0,
      render: (r) => formatOrDefault(r.gain_loss),
      numeric: true,
      align: 'right',
    },
    {
      id: 'gain_loss_pct',
      header: 'Gain/Loss (%)',
      sortable: true,
      sortAccessor: (r) => r.gain_loss_pct ?? 0,
      render: (r) => formatPercentOrDefault(r.gain_loss_pct),
      numeric: true,
      align: 'right',
    },
    {
      id: 'day_change',
      header: 'Day Change ($)',
      sortable: true,
      sortAccessor: (r) => r.day_change ?? 0,
      render: (r) => formatOrDefault(r.day_change),
      numeric: true,
      align: 'right',
    },
    {
      id: 'day_change_pct',
      header: 'Day Change (%)',
      sortable: true,
      sortAccessor: (r) => r.day_change_pct ?? 0,
      render: (r) => formatPercentOrDefault(r.day_change_pct),
      numeric: true,
      align: 'right',
    },
    {
      id: 'account',
      header: 'Account',
      render: (r) => <span>{accountNameById.get(r.account_id) ?? r.account_id}</span>,
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
      data={sortedHoldings}
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
