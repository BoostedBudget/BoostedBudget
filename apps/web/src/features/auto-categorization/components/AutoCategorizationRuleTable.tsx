/**
 * AutoCategorizationRuleTable Component
 * Issue #584: Unified Card/Table View System
 *
 * Displays auto-categorization rules in a table format with sorting and actions.
 */

import { useCallback } from 'react'
import { Edit, Trash2, ChevronUp, ChevronDown } from 'lucide-react'
import type { CategorizationRule } from '../types'
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
      <div className={`autocat-table__empty ${className}`.trim()}>
        <p>No rules found. Create your first auto-categorization rule to get started.</p>
      </div>
    )
  }

  return (
    <table
      className={`autocat-table ${loading ? 'autocat-table--loading' : ''} ${className}`.trim()}
    >
      <thead>
        <tr className="autocat-table__header-row">
          <th className="autocat-table__th">
            {renderSortableHeader('Name', 'name')}
          </th>
          <th className="autocat-table__th">
            <div className="autocat-table__header-content">
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
          <th className="autocat-table__th">
            <div className="autocat-table__header-content">
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
          <th className="autocat-table__th">Conditions</th>
          <th className="autocat-table__th">
            <div className="autocat-table__header-content">
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
          <th className="autocat-table__th autocat-table__th--matches">
            {renderSortableHeader('Matches', 'matches')}
          </th>
          {hasActions && (
            <th className="autocat-table__th autocat-table__th--actions">
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {rules.length === 0 ? (
          <tr className="autocat-table__empty-row">
            <td colSpan={hasActions ? 7 : 6}>
              No rules match your filters. Try adjusting your criteria.
            </td>
          </tr>
        ) : rules.map((rule) => {
          const conditionsText = formatConditions(
            rule.conditions,
            rule.condition_logic
          )
          const categoryLabel = rule.target_category?.[0] || 'No category'
          const sourceLabel = getSourceLabel(rule.source)

          return (
            <tr
              key={rule.id}
              className={`autocat-table__row ${onRowClick ? 'autocat-table__row--clickable' : ''}`}
              onClick={onRowClick ? () => onRowClick(rule) : undefined}
            >
              <td className="autocat-table__td autocat-table__td--name">
                <div className="autocat-table__name-cell">
                  <span className="autocat-table__name">{rule.name}</span>
                  {rule.description && (
                    <span className="autocat-table__description">
                      {rule.description}
                    </span>
                  )}
                  <span
                    className={`autocat-table__source autocat-table__source--${rule.source}`}
                  >
                    {sourceLabel}
                  </span>
                </div>
              </td>
              <td className="autocat-table__td">
                <span
                  className={`autocat-table__status ${rule.enabled ? 'autocat-table__status--enabled' : 'autocat-table__status--disabled'}`}
                >
                  {rule.enabled ? 'Enabled' : 'Disabled'}
                </span>
              </td>
              <td className="autocat-table__td autocat-table__td--priority">
                {rule.priority}
              </td>
              <td className="autocat-table__td">{conditionsText}</td>
              <td className="autocat-table__td autocat-table__td--category">
                <div className="autocat-table__category-cell">
                  <span className="autocat-table__category">{categoryLabel}</span>
                  {rule.target_tags && rule.target_tags.length > 0 && (
                    <div className="autocat-table__tags">
                      {rule.target_tags.map((tag) => (
                        <span key={tag} className="autocat-table__tag">
                          {tag}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              </td>
              <td className="autocat-table__td autocat-table__td--matches">
                {rule.match_count}
              </td>
              {hasActions && (
                <td
                  className="autocat-table__td autocat-table__td--actions"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="autocat-table__actions">
                    {onToggle && (
                      <button
                        type="button"
                        role="switch"
                        aria-checked={rule.enabled}
                        onClick={() => handleToggle(rule)}
                        disabled={loading}
                        className={`autocat-table__toggle ${rule.enabled ? 'autocat-table__toggle--on' : ''}`}
                        aria-label={
                          rule.enabled ? 'Disable rule' : 'Enable rule'
                        }
                        title={rule.enabled ? 'Disable rule' : 'Enable rule'}
                      >
                        <span className="autocat-table__toggle-slider" />
                      </button>
                    )}
                    {onEdit && (
                      <button
                        type="button"
                        onClick={() => onEdit(rule)}
                        disabled={loading}
                        className="autocat-table__action-btn"
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
                        className="autocat-table__action-btn autocat-table__action-btn--danger"
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
