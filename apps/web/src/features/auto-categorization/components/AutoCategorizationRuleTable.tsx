/**
 * AutoCategorizationRuleTable Component
 * Issue #584: Unified Card/Table View System
 *
 * Displays auto-categorization rules in a table format with sorting and actions.
 */

import { useCallback } from 'react'
import { Edit, Trash2, ChevronUp, ChevronDown } from 'lucide-react'
import type { CategorizationRule } from '../types'
import { formatPFCEnum } from '@/shared/categories/formatPfc'
import {
  ColumnFilterDropdown,
  type FilterOption,
  type RangeValue,
} from '@/shared/components/ColumnFilterDropdown'

export type SortColumn = 'name' | 'status' | 'priority' | 'matches' | 'category'
export type SortDirection = 'asc' | 'desc'

export interface AutoCategorizationRuleTableProps {
  /** Rules to display */
  rules: CategorizationRule[]
  /** Called when edit button is clicked */
  onEdit?: (rule: CategorizationRule) => void
  /** Called when delete button is clicked */
  onDelete?: (rule: CategorizationRule) => void
  /** Called when toggle switch is changed */
  onToggle?: (rule: CategorizationRule, enabled: boolean) => void
  /** Called when a row is clicked */
  onRowClick?: (rule: CategorizationRule) => void
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
  /** Available status options for filtering */
  statusOptions?: FilterOption[]
  /** Current status filter value */
  statusFilter?: string[]
  /** Called when status filter changes */
  onStatusFilterChange?: (value: string[]) => void

  /** Available category options for filtering */
  categoryOptions?: FilterOption[]
  /** Current category filter value */
  categoryFilter?: string[]
  /** Called when category filter changes */
  onCategoryFilterChange?: (value: string[]) => void

  /** Current priority range filter value */
  priorityRange?: RangeValue | null
  /** Called when priority range filter changes */
  onPriorityRangeChange?: (value: RangeValue | null) => void
}

/**
 * Format conditions display text
 */
function formatConditions(
  conditions: CategorizationRule['conditions'],
  conditionLogic: CategorizationRule['condition_logic']
): string {
  const count = conditions.length
  if (count === 1) {
    return '1 condition'
  }
  return `${count} conditions (${conditionLogic})`
}

/**
 * Get source badge text
 */
function getSourceLabel(source: CategorizationRule['source']): string {
  switch (source) {
    case 'manual':
      return 'Manual'
    case 'ai':
      return 'AI'
    case 'template':
      return 'Template'
    default:
      return source
  }
}

/**
 * AutoCategorizationRuleTable displays auto-categorization rules in a sortable table format.
 */
export function AutoCategorizationRuleTable({
  rules,
  onEdit,
  onDelete,
  onToggle,
  onRowClick,
  loading = false,
  sortable = false,
  sortColumn,
  sortDirection = 'asc',
  onSort,
  className = '',
  // Issue #590: Inline filter props
  statusOptions,
  statusFilter,
  onStatusFilterChange,
  categoryOptions,
  categoryFilter,
  onCategoryFilterChange,
  priorityRange,
  onPriorityRangeChange,
}: AutoCategorizationRuleTableProps) {
  const hasActions = !!(onEdit || onDelete || onToggle)

  const handleSort = useCallback(
    (column: SortColumn) => {
      if (!sortable || !onSort) return
      const newDirection =
        sortColumn === column && sortDirection === 'asc' ? 'desc' : 'asc'
      onSort(column, newDirection)
    },
    [sortable, onSort, sortColumn, sortDirection]
  )

  const handleToggle = useCallback(
    (rule: CategorizationRule) => {
      if (onToggle && !loading) {
        onToggle(rule, !rule.enabled)
      }
    },
    [onToggle, loading]
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
        className="inline-flex cursor-pointer items-center gap-1 border-none bg-transparent p-0 font-[inherit] text-inherit"
      >
        {label}
        <span className="inline-flex h-4 w-4 items-center [&_svg]:h-3.5 [&_svg]:w-3.5">
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
    (statusFilter?.length ?? 0) > 0 ||
    (categoryFilter?.length ?? 0) > 0 ||
    priorityRange != null

  // Only show empty state if no data AND no active filters
  if (rules.length === 0 && !hasActiveFilters) {
    return (
      <div className={`text-center rounded-lg border border-dashed border-border bg-card px-6 py-12 text-muted-foreground ${className}`}>
        <p>No rules found. Create your first auto-categorization rule to get started.</p>
      </div>
    )
  }

  return (
    <table
      className={`w-full border-collapse overflow-visible rounded-lg border border-border bg-card ${loading ? 'autocat-table--loading pointer-events-none opacity-60' : ''} ${className}`}
    >
      <thead>
        <tr className="bg-muted/50">
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            {renderSortableHeader('Name', 'name')}
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Status', 'status')}
              {statusOptions && onStatusFilterChange && (
                <ColumnFilterDropdown
                  type="select"
                  options={statusOptions}
                  value={statusFilter}
                  onChange={(val) => onStatusFilterChange(val as string[])}
                  multiSelect
                  ariaLabel="Filter by status"
                />
              )}
            </div>
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Priority', 'priority')}
              {onPriorityRangeChange && (
                <ColumnFilterDropdown
                  type="range"
                  rangeValue={priorityRange}
                  onChange={(val) => onPriorityRangeChange(val as RangeValue | null)}
                  ariaLabel="Filter by priority"
                />
              )}
            </div>
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">Conditions</th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            <div className="flex items-center gap-1">
              {renderSortableHeader('Category', 'category')}
              {categoryOptions && onCategoryFilterChange && (
                <ColumnFilterDropdown
                  type="select"
                  options={categoryOptions}
                  value={categoryFilter}
                  onChange={(val) => onCategoryFilterChange(val as string[])}
                  multiSelect
                  searchable
                  ariaLabel="Filter by category"
                />
              )}
            </div>
          </th>
          <th className="border-b border-border px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            {renderSortableHeader('Matches', 'matches')}
          </th>
          {hasActions && (
            <th className="w-32 border-b border-border px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground max-md:w-24">
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {rules.length === 0 ? (
          <tr>
            <td
              colSpan={hasActions ? 7 : 6}
              className="px-4 py-12 text-center italic text-muted-foreground"
            >
              No rules match your filters. Try adjusting your criteria.
            </td>
          </tr>
        ) : rules.map((rule) => {
          const conditionsText = formatConditions(
            rule.conditions,
            rule.condition_logic
          )
          // PFC v2 (#1073 + #1080): prefer server display fields for category label.
          const categoryLabel =
            rule.target_kind === 'pfc' && rule.target_pfc_primary
              ? (rule.target_pfc_detailed_display
                  ?? rule.target_pfc_primary_display
                  ?? formatPFCEnum(rule.target_pfc_detailed ?? rule.target_pfc_primary))
              : rule.target_kind === 'custom' && rule.target_custom_category_id
                ? rule.target_custom_category_id
                : rule.target_kind === 'clear'
                  ? 'Clear override'
                  : 'No category'
          const sourceLabel = getSourceLabel(rule.source)

          return (
            <tr
              key={rule.id}
              className={`transition-colors duration-150 hover:bg-muted/50 ${
                onRowClick ? 'autocat-table__row--clickable cursor-pointer' : ''
              }`}
              onClick={onRowClick ? () => onRowClick(rule) : undefined}
            >
              <td className="min-w-[180px] border-b border-border px-4 py-3.5 text-sm align-middle text-foreground max-md:min-w-[150px] max-md:px-3 max-md:py-2.5">
                <div className="flex flex-col gap-1">
                  <span className="font-medium text-foreground">{rule.name}</span>
                  {rule.description && (
                    <span className="text-xs text-muted-foreground">
                      {rule.description}
                    </span>
                  )}
                  <span
                    className={`mt-1 inline-flex w-fit items-center rounded px-1.5 py-0.5 text-[0.625rem] font-medium ${
                      rule.source === 'manual' ? 'bg-blue-50 text-blue-700 dark:bg-blue-900 dark:text-blue-300' : ''
                    } ${
                      rule.source === 'ai' ? 'bg-purple-50 text-purple-700 dark:bg-purple-900 dark:text-purple-300' : ''
                    } ${
                      rule.source === 'template' ? 'bg-green-50 text-green-700 dark:bg-green-900 dark:text-green-300' : ''
                    }`}
                  >
                    {sourceLabel}
                  </span>
                </div>
              </td>
              <td className="border-b border-border px-4 py-3.5 text-sm align-middle max-md:px-3 max-md:py-2.5">
                <span
                  className={`inline-flex items-center rounded-md px-2 py-1 text-xs font-medium ${
                    rule.enabled
                      ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300'
                      : 'bg-muted text-muted-foreground'
                  }`}
                >
                  {rule.enabled ? 'Enabled' : 'Disabled'}
                </span>
              </td>
              <td className="border-b border-border px-4 py-3.5 text-center text-sm align-middle tabular-nums text-foreground max-md:px-3 max-md:py-2.5">
                {rule.priority}
              </td>
              <td className="border-b border-border px-4 py-3.5 text-sm align-middle text-foreground max-md:px-3 max-md:py-2.5">{conditionsText}</td>
              <td className="min-w-[150px] border-b border-border px-4 py-3.5 text-sm align-middle text-foreground max-md:px-3 max-md:py-2.5">
                <div className="flex flex-col gap-1">
                  <span className="font-medium">{categoryLabel}</span>
                  {rule.target_tags && rule.target_tags.length > 0 && (
                    <div className="flex flex-wrap gap-1">
                      {rule.target_tags.map((tag) => (
                        <span key={tag} className="inline-flex items-center rounded bg-blue-50 px-1.5 py-0.5 text-[0.625rem] font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-300">
                          {tag}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              </td>
              <td className="border-b border-border px-4 py-3.5 text-right text-sm align-middle tabular-nums text-foreground max-md:px-3 max-md:py-2.5">
                {rule.match_count}
              </td>
              {hasActions && (
                <td
                  className="border-b border-border px-4 py-3.5 text-right align-middle max-md:px-3 max-md:py-2.5"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="flex items-center justify-end gap-2">
                    {onToggle && (
                      <button
                        type="button"
                        role="switch"
                        aria-checked={rule.enabled}
                        onClick={() => handleToggle(rule)}
                        disabled={loading}
                        className={`relative h-5 w-10 cursor-pointer rounded-full border-none p-0 transition-colors duration-200 disabled:cursor-not-allowed disabled:opacity-50 ${
                          rule.enabled
                            ? 'bg-green-500 dark:bg-green-600'
                            : 'bg-muted-foreground/30'
                        }`}
                        aria-label={rule.enabled ? 'Disable rule' : 'Enable rule'}
                        title={rule.enabled ? 'Disable rule' : 'Enable rule'}
                      >
                        <span
                          className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform duration-200 ${
                            rule.enabled ? 'left-0.5 translate-x-5' : 'left-0.5'
                          }`}
                        />
                      </button>
                    )}
                    {onEdit && (
                      <button
                        type="button"
                        onClick={() => onEdit(rule)}
                        disabled={loading}
                        className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors duration-150 hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
                        aria-label="Edit rule"
                        title="Edit rule"
                      >
                        <Edit aria-hidden="true" />
                      </button>
                    )}
                    {onDelete && (
                      <button
                        type="button"
                        onClick={() => onDelete(rule)}
                        disabled={loading}
                        className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors duration-150 hover:bg-red-100 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4 dark:hover:bg-red-900 dark:hover:text-red-400"
                        aria-label="Delete rule"
                        title="Delete rule"
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
