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
import './BudgetTable.css'

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
        className="budget-table__sort-btn"
      >
        {label}
        <span className="budget-table__sort-icon">
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
      <div className={`budget-table__empty ${className}`.trim()}>
        <p>No budgets found. Create a budget to get started.</p>
      </div>
    )
  }

  return (
    <table
      className={`budget-table ${loading ? 'budget-table--loading' : ''} ${className}`.trim()}
    >
      <thead>
        <tr className="budget-table__header-row">
          <th className="budget-table__th">
            {renderSortableHeader('Name', 'name')}
          </th>
          <th className="budget-table__th">
            <div className="budget-table__header-content">
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
          <th className="budget-table__th budget-table__th--amount">
            {renderSortableHeader('Amount', 'amount')}
          </th>
          <th className="budget-table__th budget-table__th--progress">
            <div className="budget-table__header-content">
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
          <th className="budget-table__th budget-table__th--remaining">
            {renderSortableHeader('Remaining', 'remaining')}
          </th>
          <th className="budget-table__th">Categories</th>
          {hasActions && (
            <th className="budget-table__th budget-table__th--actions">
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {budgets.length === 0 ? (
          <tr className="budget-table__empty-row">
            <td colSpan={hasActions ? 7 : 6}>
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
              className={`budget-table__row ${onRowClick ? 'budget-table__row--clickable' : ''} ${!budget.is_active ? 'budget-table__row--inactive' : ''}`}
              onClick={onRowClick ? () => onRowClick(budget) : undefined}
            >
              <td className="budget-table__td budget-table__td--name">
                <div className="budget-table__name-cell">
                  <span className="budget-table__name">{budget.name}</span>
                  <span
                    className={`budget-table__status-badge ${
                      budget.is_active
                        ? 'budget-table__status-badge--active'
                        : 'budget-table__status-badge--inactive'
                    }`}
                  >
                    {budget.is_active ? 'Active' : 'Inactive'}
                  </span>
                </div>
              </td>
              <td className="budget-table__td">{getPeriodLabel(budget.period)}</td>
              <td className="budget-table__td budget-table__td--amount">
                {formatCurrency(budget.amount)}
              </td>
              <td className="budget-table__td budget-table__td--progress">
                <div className="budget-table__progress-cell">
                  <div
                    className="budget-table__progress-bar"
                    role="progressbar"
                    aria-valuenow={percentUsed}
                    aria-valuemin={0}
                    aria-valuemax={100}
                  >
                    <div
                      className={`budget-table__progress-fill ${
                        isOverBudget ? 'budget-table__progress-fill--over' : ''
                      }`}
                      style={{ width: `${Math.min(percentUsed, 100)}%` }}
                    />
                  </div>
                  <span
                    className={`budget-table__progress-text ${
                      isOverBudget ? 'budget-table__progress-text--over' : ''
                    }`}
                  >
                    {percentUsed}%
                  </span>
                </div>
              </td>
              <td className="budget-table__td budget-table__td--remaining">
                <span
                  className={`budget-table__remaining ${
                    budget.remaining_amount < 0
                      ? 'budget-table__remaining--negative'
                      : 'budget-table__remaining--positive'
                  }`}
                >
                  {formatCurrency(budget.remaining_amount)}
                </span>
              </td>
              <td className="budget-table__td budget-table__td--categories">
                {categoryCount}
              </td>
              {hasActions && (
                <td
                  className="budget-table__td budget-table__td--actions"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="budget-table__actions">
                    {onManage && (
                      <button
                        type="button"
                        onClick={() => onManage(budget)}
                        disabled={loading}
                        className="budget-table__action-btn"
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
                        className="budget-table__action-btn budget-table__action-btn--danger"
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
