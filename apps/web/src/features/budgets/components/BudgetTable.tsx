/**
 * BudgetTable Component
 * Issue #584: Unified Card/Table View System
 *
 * Displays budgets in a table format with progress bars and actions.
 */

import { useCallback } from 'react'
import { Settings, Trash2, ChevronUp, ChevronDown } from 'lucide-react'
import type { BudgetWithMetrics } from '../types'
import {
  ColumnFilterDropdown,
  type FilterOption,
  type RangeValue,
} from '@/shared/components/ColumnFilterDropdown'

export type SortColumn = 'name' | 'period' | 'amount' | 'progress' | 'remaining'
export type SortDirection = 'asc' | 'desc'

export interface BudgetTableProps {
  /** Budgets to display */
  budgets: BudgetWithMetrics[]
  /** Called when manage button is clicked */
  onManage?: (budget: BudgetWithMetrics) => void
  /** Called when delete button is clicked */
  onDelete?: (budgetId: string) => void
  /** Called when a row is clicked */
  onRowClick?: (budget: BudgetWithMetrics) => void
  /** Show loading state */
  loading?: boolean
  /** Enable sorting */
  sortable?: boolean
  /** Currently sorted column */
  sortColumn?: SortColumn
  /** Current sort direction */
  sortDirection?: SortDirection
  /** Called when sort changes */
  onSort?: (column: SortColumn, direction: SortDirection) => void
  /** Additional CSS class */
  className?: string

  // Issue #590: Inline filter props
  /** Available period options for filtering */
  periodOptions?: FilterOption[]
  /** Current period filter value */
  periodFilter?: string[]
  /** Called when period filter changes */
  onPeriodFilterChange?: (value: string[]) => void

  /** Current progress range filter value (percentage) */
  progressRange?: RangeValue | null
  /** Called when progress range filter changes */
  onProgressRangeChange?: (value: RangeValue | null) => void
}

/**
 * Format currency amount
 */
function formatCurrency(amount: number): string {
  const absAmount = Math.abs(amount)
  const formatted = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(absAmount)

  if (amount < 0) return `-${formatted}`
  return formatted
}

/**
 * Get human-readable period label
 */
function getPeriodLabel(period: string): string {
  switch (period.toLowerCase()) {
    case 'weekly':
      return 'Weekly'
    case 'monthly':
      return 'Monthly'
    case 'quarterly':
      return 'Quarterly'
    case 'yearly':
      return 'Yearly'
    case 'custom':
      return 'Custom'
    default:
      return period.charAt(0).toUpperCase() + period.slice(1)
  }
}

/**
 * BudgetTable displays budgets in a sortable table format with progress bars.
 */
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
  // Issue #590: Inline filter props
  periodOptions,
  periodFilter,
  onPeriodFilterChange,
  progressRange,
  onProgressRangeChange,
}: BudgetTableProps) {
  const hasActions = !!(onManage || onDelete)

  const handleSort = useCallback(
    (column: SortColumn) => {
      if (!sortable || !onSort) return
      const newDirection =
        sortColumn === column && sortDirection === 'asc' ? 'desc' : 'asc'
      onSort(column, newDirection)
    },
    [sortable, onSort, sortColumn, sortDirection]
  )

  const renderSortableHeader = (label: string, column: SortColumn) => {
    if (!sortable) {
      return label
    }

    const isActive = sortColumn === column
    const ariaSort = isActive
      ? sortDirection === 'asc'
        ? 'ascending'
        : 'descending'
      : undefined

    return (
      <button
        type="button"
        onClick={() => handleSort(column)}
        aria-sort={ariaSort}
        className="inline-flex items-center gap-1 bg-none border-none p-0 font-[inherit] color-inherit cursor-pointer hover:text-foreground"
      >
        {label}
        <span className="inline-flex items-center w-4 h-4 [&_svg]:w-3.5 [&_svg]:h-3.5">
          {isActive && sortDirection === 'asc' && (
            <ChevronUp aria-hidden="true" />
          )}
          {isActive && sortDirection === 'desc' && (
            <ChevronDown aria-hidden="true" />
          )}
        </span>
      </button>
    )
  }

  // Issue #590: Compute if any filters are active
  // Note: Use != null to check for both null and undefined
  const hasActiveFilters =
    (periodFilter?.length ?? 0) > 0 ||
    progressRange != null

  // Only show empty state if no data AND no active filters
  if (budgets.length === 0 && !hasActiveFilters) {
    return (
      <div className={`text-center py-12 px-6 text-muted-foreground bg-card border border-dashed border-border rounded-lg ${className}`.trim()}>
        <p>No budgets found. Create a budget to get started.</p>
      </div>
    )
  }

  return (
    <table
      className={`w-full border-collapse bg-card rounded-lg overflow-visible border border-border ${loading ? 'budget-table--loading opacity-60 pointer-events-none' : ''} ${className}`.trim()}
    >
      <thead>
        <tr className="bg-muted">
          <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground border-b border-border">
            {renderSortableHeader('Name', 'name')}
          </th>
          <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground border-b border-border">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Period', 'period')}
              {periodOptions && onPeriodFilterChange && (
                <ColumnFilterDropdown
                  type="select"
                  options={periodOptions}
                  value={periodFilter}
                  onChange={(val) => onPeriodFilterChange(val as string[])}
                  multiSelect
                  ariaLabel="Filter by period"
                />
              )}
            </div>
          </th>
          <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-muted-foreground border-b border-border">
            {renderSortableHeader('Amount', 'amount')}
          </th>
          <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground border-b border-border min-w-[140px]">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Progress', 'progress')}
              {onProgressRangeChange && (
                <ColumnFilterDropdown
                  type="range"
                  rangeValue={progressRange}
                  onChange={(val) => onProgressRangeChange(val as RangeValue | null)}
                  ariaLabel="Filter by progress"
                />
              )}
            </div>
          </th>
          <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-muted-foreground border-b border-border">
            {renderSortableHeader('Remaining', 'remaining')}
          </th>
          <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground border-b border-border">Categories</th>
          {hasActions && (
            <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-muted-foreground border-b border-border w-24">
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {budgets.length === 0 ? (
          <tr>
            <td colSpan={hasActions ? 7 : 6} className="py-12 px-4 text-center text-muted-foreground italic">
              No budgets match your filters. Try adjusting your criteria.
            </td>
          </tr>
        ) : budgets.map((budget) => {
          const categoryCount = budget.categories?.length ?? 0
          const percentUsed = Math.round(budget.percent_used)
          const isOverBudget = budget.is_over_budget

          return (
            <tr
              key={budget.id}
              className={`transition-colors duration-150 hover:bg-muted/50 ${onRowClick ? 'budget-table__row--clickable cursor-pointer' : ''} ${!budget.is_active ? 'opacity-60' : ''}`}
              onClick={onRowClick ? () => onRowClick(budget) : undefined}
            >
              <td className="px-4 py-3.5 text-sm text-foreground border-b border-border align-middle min-w-[200px] max-sm:min-w-[150px]">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-foreground">{budget.name}</span>
                  <span
                    className={`inline-flex items-center px-2 py-0.5 text-[0.625rem] font-medium rounded-full uppercase tracking-wide ${
                      budget.is_active
                        ? 'bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300'
                        : 'bg-muted text-muted-foreground'
                    }`}
                  >
                    {budget.is_active ? 'Active' : 'Inactive'}
                  </span>
                </div>
              </td>
              <td className="px-4 py-3.5 text-sm text-foreground border-b border-border align-middle">{getPeriodLabel(budget.period)}</td>
              <td className="px-4 py-3.5 text-sm text-foreground border-b border-border align-middle text-right font-medium tabular-nums">
                {formatCurrency(budget.amount)}
              </td>
              <td className="px-4 py-3.5 text-sm text-foreground border-b border-border align-middle min-w-[140px]">
                <div className="flex items-center gap-3 max-sm:flex-col max-sm:items-start max-sm:gap-1">
                  <div
                    className="flex-1 h-2 bg-muted rounded-full overflow-hidden"
                    role="progressbar"
                    aria-valuenow={percentUsed}
                    aria-valuemin={0}
                    aria-valuemax={100}
                  >
                    <div
                      className={`h-full rounded-full transition-[width] duration-300 ${isOverBudget ? 'bg-red-500' : 'bg-blue-500'}`}
                      style={{ width: `${Math.min(percentUsed, 100)}%` }}
                    />
                  </div>
                  <span
                    className={`min-w-12 text-right text-xs font-medium max-sm:min-w-0 max-sm:text-left ${
                      isOverBudget ? 'text-red-600 dark:text-red-400' : 'text-muted-foreground'
                    }`}
                  >
                    {percentUsed}%
                  </span>
                </div>
              </td>
              <td className="px-4 py-3.5 text-sm border-b border-border align-middle text-right">
                <span
                  className={`font-semibold tabular-nums ${
                    budget.remaining_amount < 0
                      ? 'budget-table__remaining--negative text-red-600 dark:text-red-400'
                      : 'budget-table__remaining--positive text-green-600 dark:text-green-400'
                  }`}
                >
                  {formatCurrency(budget.remaining_amount)}
                </span>
              </td>
              <td className="px-4 py-3.5 text-sm text-muted-foreground border-b border-border align-middle text-center">
                {categoryCount}
              </td>
              {hasActions && (
                <td
                  className="px-4 py-3.5 text-sm border-b border-border align-middle text-right"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="flex items-center justify-end gap-2">
                    {onManage && (
                      <button
                        type="button"
                        onClick={() => onManage(budget)}
                        disabled={loading}
                        className="inline-flex items-center justify-center w-8 h-8 p-0 bg-transparent border-none rounded-md text-muted-foreground cursor-pointer transition-colors duration-150 hover:bg-muted hover:text-foreground disabled:opacity-50 disabled:cursor-not-allowed [&_svg]:w-4 [&_svg]:h-4"
                        aria-label="Manage budget"
                        title="Manage budget"
                      >
                        <Settings aria-hidden="true" />
                      </button>
                    )}
                    {onDelete && (
                      <button
                        type="button"
                        onClick={() => onDelete(budget.id)}
                        disabled={loading}
                        className="inline-flex items-center justify-center w-8 h-8 p-0 bg-transparent border-none rounded-md text-muted-foreground cursor-pointer transition-colors duration-150 hover:bg-red-100 dark:hover:bg-red-900/30 hover:text-red-600 dark:hover:text-red-400 disabled:opacity-50 disabled:cursor-not-allowed [&_svg]:w-4 [&_svg]:h-4"
                        aria-label="Delete budget"
                        title="Delete budget"
                      >
                        <Trash2 aria-hidden="true" />
                      </button>
                    )}
                  </div>
                </td>
              )}
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
