import { useCallback } from 'react'
import { Settings, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { DataTable } from '@/shared/components/DataTable'
import type { ColumnDef, FilterState, SortState } from '@/shared/components/DataTable'
import type { BudgetWithMetrics } from '../types'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'

export type SortColumn = 'name' | 'period' | 'amount' | 'progress' | 'remaining'
export type SortDirection = 'asc' | 'desc'

export interface BudgetTableProps {
  budgets: BudgetWithMetrics[]
  onManage?: (budget: BudgetWithMetrics) => void
  onDelete?: (budgetId: string) => void
  onRowClick?: (budget: BudgetWithMetrics) => void
  loading?: boolean
  sortable?: boolean
  sortColumn?: SortColumn
  sortDirection?: SortDirection
  onSort?: (column: SortColumn, direction: SortDirection) => void
  className?: string
  periodOptions?: FilterOption[]
  periodFilter?: string[]
  onPeriodFilterChange?: (value: string[]) => void
  progressRange?: RangeValue | null
  onProgressRangeChange?: (value: RangeValue | null) => void
}

function formatCurrency(amount: number): string {
  const absAmount = Math.abs(amount)
  const formatted = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(absAmount)
  return amount < 0 ? `-${formatted}` : formatted
}

function getPeriodLabel(period: string): string {
  switch (period.toLowerCase()) {
    case 'weekly': return 'Weekly'
    case 'monthly': return 'Monthly'
    case 'quarterly': return 'Quarterly'
    case 'yearly': return 'Yearly'
    case 'custom': return 'Custom'
    default: return period.charAt(0).toUpperCase() + period.slice(1)
  }
}

export function BudgetTable({
  budgets,
  onManage,
  onDelete,
  onRowClick,
  loading = false,
  sortable = false,
  sortColumn,
  sortDirection = 'asc',
  onSort,
  className = '',
  periodOptions,
  periodFilter,
  onPeriodFilterChange,
  progressRange,
  onProgressRangeChange,
}: BudgetTableProps) {
  const hasActions = !!(onManage || onDelete)
  const hasActiveFilters = (periodFilter?.length ?? 0) > 0 || progressRange != null

  // Bridge DataTable 3-state sort → BudgetTable (column, direction) callback
  const handleSortChange = useCallback(
    (newSort: SortState) => {
      if (!onSort || !sortable) return
      if (newSort) {
        onSort(newSort.columnId as SortColumn, newSort.direction)
      } else if (sortColumn) {
        onSort(sortColumn, sortDirection === 'asc' ? 'desc' : 'asc')
      }
    },
    [onSort, sortable, sortColumn, sortDirection]
  )

  const sort: SortState =
    sortColumn && sortable ? { columnId: sortColumn, direction: sortDirection } : null

  const filters: FilterState = {
    period: periodFilter ?? [],
    progress: progressRange ?? null,
  }

  const handleFiltersChange = useCallback(
    (newFilters: FilterState) => {
      if (newFilters.period !== filters.period) {
        onPeriodFilterChange?.(newFilters.period as string[])
      }
      if (newFilters.progress !== filters.progress) {
        onProgressRangeChange?.(newFilters.progress as RangeValue | null)
      }
    },
    [filters.period, filters.progress, onPeriodFilterChange, onProgressRangeChange]
  )

  const columns: ColumnDef<BudgetWithMetrics>[] = [
    {
      id: 'name',
      header: 'Name',
      sortable,
      widthClass: 'min-w-[200px] max-sm:min-w-[150px]',
      render: (row) => (
        <div className="flex items-center gap-2">
          <span className="font-medium text-foreground">{row.name}</span>
          {row.is_active ? (
            <Badge variant="success">Active</Badge>
          ) : (
            <Badge variant="secondary">Inactive</Badge>
          )}
        </div>
      ),
    },
    {
      id: 'period',
      header: 'Period',
      sortable,
      render: (row) => <>{getPeriodLabel(row.period)}</>,
      filter:
        periodOptions && onPeriodFilterChange
          ? { type: 'select', options: periodOptions, multiSelect: true }
          : undefined,
    },
    {
      id: 'amount',
      header: 'Amount',
      sortable,
      align: 'right',
      numeric: true,
      render: (row) => (
        <span className="font-medium">
          {formatCurrency(row.expected_income ?? row.amount)}
        </span>
      ),
    },
    {
      id: 'income',
      header: 'Income',
      align: 'right',
      numeric: true,
      render: (row) =>
        (row.expected_income ?? 0) > 0
          ? `${formatCurrency(row.income_amount ?? 0)} / ${formatCurrency(row.expected_income!)}`
          : '—',
    },
    {
      id: 'progress',
      header: 'Progress',
      sortable,
      widthClass: 'min-w-[140px]',
      render: (row) => {
        const percentUsed = Math.round(row.percent_used)
        const isOverBudget = row.is_over_budget
        return (
          <div className="flex items-center gap-3 max-sm:flex-col max-sm:items-start max-sm:gap-1">
            <div
              className="flex-1 h-2 bg-muted rounded-full overflow-hidden"
              role="progressbar"
              aria-valuenow={percentUsed}
              aria-valuemin={0}
              aria-valuemax={100}
            >
              <div
                className={cn(
                  'h-full rounded-full transition-[width] duration-300',
                  isOverBudget ? 'bg-destructive' : 'bg-primary'
                )}
                style={{ width: `${Math.min(percentUsed, 100)}%` }}
              />
            </div>
            <span
              className={cn(
                'min-w-12 text-right text-xs font-medium max-sm:min-w-0 max-sm:text-left',
                isOverBudget ? 'text-destructive' : 'text-muted-foreground'
              )}
            >
              {percentUsed}%
            </span>
          </div>
        )
      },
      filter: onProgressRangeChange ? { type: 'range', unit: 'percent' } : undefined,
    },
    {
      id: 'remaining',
      header: 'Remaining',
      sortable,
      align: 'right',
      render: (row) => (
        <span
          className={cn(
            'font-semibold tabular-nums',
            row.remaining_amount < 0
              ? 'budget-table__remaining--negative text-destructive'
              : 'budget-table__remaining--positive text-success'
          )}
        >
          {formatCurrency(row.remaining_amount)}
        </span>
      ),
    },
    {
      id: 'categories',
      header: 'Categories',
      align: 'center',
      render: (row) => <>{row.categories?.length ?? 0}</>,
    },
  ]

  if (budgets.length === 0 && !hasActiveFilters) {
    return (
      <div
        className={cn(
          'text-center py-12 px-6 text-muted-foreground bg-card border border-dashed border-border rounded-lg',
          className
        )}
      >
        <p>No budgets found. Create a budget to get started.</p>
      </div>
    )
  }

  return (
    <DataTable<BudgetWithMetrics>
      data={budgets}
      columns={columns}
      rowKey={(row) => row.id}
      sort={sort}
      onSortChange={handleSortChange}
      filters={filters}
      onFiltersChange={handleFiltersChange}
      loading={loading}
      onRowClick={onRowClick}
      rowClassName={(row) =>
        cn(
          onRowClick && 'budget-table__row--clickable',
          !row.is_active && 'opacity-60'
        )
      }
      className={cn(loading && 'budget-table--loading', className)}
      maskedColumnIds={['amount', 'income', 'remaining']}
      emptyState={
        <p className="py-12 px-4 text-center text-muted-foreground italic">
          No budgets match your filters. Try adjusting your criteria.
        </p>
      }
      rowActions={
        hasActions
          ? (row) => (
              <div className="flex items-center justify-end gap-2">
                {onManage && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => onManage(row)}
                    disabled={loading}
                    aria-label="Manage budget"
                    title="Manage budget"
                  >
                    <Settings aria-hidden="true" />
                  </Button>
                )}
                {onDelete && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => onDelete(row.id)}
                    disabled={loading}
                    aria-label="Delete budget"
                    title="Delete budget"
                    className="hover:bg-destructive/10 hover:text-destructive"
                  >
                    <Trash2 aria-hidden="true" />
                  </Button>
                )}
              </div>
            )
          : undefined
      }
    />
  )
}
