/**
 * AutoCategorizationRuleTable Component
 * Issue #584: Unified Card/Table View System
 * Issue #1143: Migrate to shared DataTable (actual ~-30 LOC net; rich rowActions
 * JSX and column render functions absorbed the raw-table structural savings)
 *
 * Displays auto-categorization rules in a table format with sorting and actions.
 *
 * PII Audit: No columns in this table contain PII requiring data-ph-mask.
 * Audit: name (non-financial), status, priority (integer), conditions (count string),
 * category (label string), matches (integer count).
 */

import { useCallback, useMemo } from 'react'
import { Edit, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { CategorizationRule } from '../types'
import { formatPFCEnum } from '@/shared/categories/formatPfc'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState, FilterState } from '@/shared/components/DataTable'

export type SortColumn = 'name' | 'status' | 'priority' | 'matches' | 'category'
export type SortDirection = 'asc' | 'desc'

// eslint-disable-next-line react-refresh/only-export-components
export const AUTOCAT_COLUMN_IDS = ['name', 'status', 'priority', 'conditions', 'category', 'matches'] as const

// eslint-disable-next-line react-refresh/only-export-components
export const RULE_SORTABLE_COLUMNS = new Set([
  'name',
  'status',
  'priority',
  'matches',
  'category',
] as const)

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

/** Format conditions display text */
function formatConditions(
  conditions: CategorizationRule['conditions'],
  conditionLogic: CategorizationRule['condition_logic']
): string {
  const count = conditions.length
  if (count === 1) return '1 condition'
  return `${count} conditions (${conditionLogic})`
}

/** Get source badge label */
function getSourceLabel(source: CategorizationRule['source']): string {
  switch (source) {
    case 'manual': return 'Manual'
    case 'ai': return 'AI'
    case 'template': return 'Template'
    default: return source
  }
}

/** CSS class for source badge */
function getSourceBadgeClass(source: CategorizationRule['source']): string {
  if (source === 'manual') return 'bg-blue-50 text-blue-700 dark:bg-blue-900 dark:text-blue-300'
  if (source === 'ai') return 'bg-purple-50 text-purple-700 dark:bg-purple-900 dark:text-purple-300'
  if (source === 'template') return 'bg-green-50 text-green-700 dark:bg-green-900 dark:text-green-300'
  return ''
}

/** Compute category label for a rule */
function getCategoryLabel(rule: CategorizationRule): string {
  if (rule.target_kind === 'pfc' && rule.target_pfc_primary) {
    return (
      rule.target_pfc_detailed_display ??
      rule.target_pfc_primary_display ??
      formatPFCEnum(rule.target_pfc_detailed ?? rule.target_pfc_primary)
    )
  }
  if (rule.target_kind === 'custom' && rule.target_custom_category_id) {
    return rule.target_custom_category_display ?? rule.target_custom_category_id
  }
  if (rule.target_kind === 'clear') return 'Clear override'
  return 'No category'
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

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('auto-categorization', [...AUTOCAT_COLUMN_IDS])

  const hasActiveFilters =
    (statusFilter?.length ?? 0) > 0 ||
    (categoryFilter?.length ?? 0) > 0 ||
    priorityRange != null

  // Translate external sort props → DataTable SortState
  const sort: SortState = sortColumn
    ? { columnId: sortColumn, direction: sortDirection }
    : null

  const handleSortChange = useCallback(
    (s: SortState) => {
      // null (clear) is silently ignored — consumer has no clear API
      if (s && onSort) onSort(s.columnId as SortColumn, s.direction)
    },
    [onSort]
  )

  // Translate external filter props → DataTable FilterState
  const filters: FilterState = useMemo(() => {
    const f: FilterState = {}
    if (statusFilter?.length) f.status = statusFilter
    if (categoryFilter?.length) f.category = categoryFilter
    if (priorityRange != null) f.priority = priorityRange
    return f
  }, [statusFilter, categoryFilter, priorityRange])

  const handleFiltersChange = useCallback(
    (f: FilterState) => {
      // Only fire a callback when its key is present in f — guards against spurious resets
      // when an unrelated column's filter changes (DataTable emits the full merged FilterState).
      if (onStatusFilterChange && 'status' in f) {
        const v = f.status
        onStatusFilterChange(Array.isArray(v) ? v : [])
      }
      if (onCategoryFilterChange && 'category' in f) {
        const v = f.category
        onCategoryFilterChange(Array.isArray(v) ? v : [])
      }
      if (onPriorityRangeChange && 'priority' in f) {
        const v = f.priority
        onPriorityRangeChange(!Array.isArray(v) ? (v as RangeValue ?? null) : null)
      }
    },
    [onStatusFilterChange, onCategoryFilterChange, onPriorityRangeChange]
  )

  const handleToggle = useCallback(
    (rule: CategorizationRule) => {
      if (onToggle && !loading) onToggle(rule, !rule.enabled)
    },
    [onToggle, loading]
  )

  const columns = useMemo((): ColumnDef<CategorizationRule>[] => [
    {
      id: 'name',
      header: 'Name',
      sortable: sortable,
      resizable: true,
      reorderable: true,
      sortAccessor: (r) => r.name,
      defaultSize: 220,
      minSize: 160,
      render: (r) => (
        <div className="flex flex-col gap-1">
          <span className="font-medium text-foreground">{r.name}</span>
          {r.description && (
            <span className="text-xs text-muted-foreground">{r.description}</span>
          )}
          <span className={cn(
            'mt-1 inline-flex w-fit items-center rounded px-1.5 py-0.5 text-[0.625rem] font-medium',
            getSourceBadgeClass(r.source)
          )}>
            {getSourceLabel(r.source)}
          </span>
        </div>
      ),
    },
    {
      id: 'status',
      header: 'Status',
      sortable: sortable,
      resizable: true,
      reorderable: true,
      defaultPinned: 'left' as const,
      defaultSize: 120,
      minSize: 90,
      maxSize: 160,
      sortAccessor: (r) => (r.enabled ? 'enabled' : 'disabled'),
      filter: statusOptions
        ? { type: 'select', options: statusOptions, multiSelect: true }
        : undefined,
      render: (r) => (
        <span className={cn(
          'inline-flex items-center rounded-md px-2 py-1 text-xs font-medium',
          r.enabled
            ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300'
            : 'bg-muted text-muted-foreground'
        )}>
          {r.enabled ? 'Enabled' : 'Disabled'}
        </span>
      ),
    },
    {
      id: 'priority',
      header: 'Priority',
      sortable: sortable,
      resizable: true,
      reorderable: true,
      hideable: true,
      sortAccessor: (r) => r.priority,
      align: 'center' as const,
      numeric: true,
      defaultSize: 110,
      minSize: 80,
      maxSize: 140,
      key: 'priority' as keyof CategorizationRule,
      filter: onPriorityRangeChange ? { type: 'range' as const } : undefined,
    },
    {
      id: 'conditions',
      header: 'Conditions',
      resizable: true,
      reorderable: true,
      defaultSize: 160,
      minSize: 120,
      maxSize: 220,
      render: (r) => formatConditions(r.conditions, r.condition_logic),
    },
    {
      id: 'category',
      header: 'Category',
      sortable: sortable,
      resizable: true,
      reorderable: true,
      sortAccessor: (r) => getCategoryLabel(r),
      defaultSize: 200,
      minSize: 150,
      maxSize: 280,
      filter: categoryOptions
        ? { type: 'select', options: categoryOptions, multiSelect: true, searchable: true }
        : undefined,
      render: (r) => (
        <div className="flex flex-col gap-1">
          <span className="font-medium">{getCategoryLabel(r)}</span>
          {r.target_tags && r.target_tags.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {r.target_tags.map((tag) => (
                <span key={tag} className="inline-flex items-center rounded bg-blue-50 px-1.5 py-0.5 text-[0.625rem] font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-300">
                  {tag}
                </span>
              ))}
            </div>
          )}
        </div>
      ),
    },
    {
      id: 'matches',
      header: 'Matches',
      align: 'right' as const,
      numeric: true,
      sortable: sortable,
      resizable: true,
      reorderable: true,
      hideable: true,
      sortAccessor: (r) => r.match_count,
      defaultSize: 110,
      minSize: 80,
      maxSize: 140,
      key: 'match_count' as keyof CategorizationRule,
    },
  ], [sortable, statusOptions, categoryOptions, onPriorityRangeChange])

  // Pre-filters empty state: no data and no active filters → return external div
  if (rules.length === 0 && !hasActiveFilters) {
    return (
      <div className={cn(
        'text-center rounded-lg border border-dashed border-border bg-card px-6 py-12 text-muted-foreground',
        className
      )}>
        <p>No rules found. Create your first auto-categorization rule to get started.</p>
      </div>
    )
  }

  return (
    <DataTable<CategorizationRule>
      data={rules}
      columns={columns}
      rowKey={(r) => r.id}
      sort={sort}
      onSortChange={handleSortChange}
      filters={filters}
      onFiltersChange={handleFiltersChange}
      loading={loading}
      className={cn(loading ? 'autocat-table--loading' : '', className)}
      onRowClick={onRowClick}
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
      rowClassName={onRowClick ? () => 'autocat-table__row--clickable' : undefined}
      emptyState={
        <span className="px-4 py-12 text-center italic text-muted-foreground">
          No rules match your filters. Try adjusting your criteria.
        </span>
      }
      rowActions={hasActions ? (rule) => (
        <div className="flex items-center justify-end gap-2">
          {onToggle && (
            <button
              type="button"
              role="switch"
              aria-checked={rule.enabled}
              onClick={() => handleToggle(rule)}
              onKeyDown={(e) => { if (e.key === ' ') e.preventDefault() }}
              disabled={loading}
              className={cn(
                'relative h-5 w-10 cursor-pointer rounded-full border-none p-0 transition-colors duration-200 disabled:cursor-not-allowed disabled:opacity-50',
                rule.enabled ? 'bg-green-500 dark:bg-green-600' : 'bg-muted-foreground/30'
              )}
              aria-label={rule.enabled ? 'Disable rule' : 'Enable rule'}
              title={rule.enabled ? 'Disable rule' : 'Enable rule'}
            >
              <span className={cn(
                'absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform duration-200',
                rule.enabled ? 'left-0.5 translate-x-5' : 'left-0.5'
              )} />
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
      ) : undefined}
    />
  )
}
