/**
 * AutoCategorizationRuleTable Component
 * Issue #584: Unified Card/Table View System
 * Issue #1143: Migrate to shared DataTable (actual ~-30 LOC net; rich rowActions
 * JSX and column render functions absorbed the raw-table structural savings)
 *
 * Displays auto-categorization rules in a table format with sorting and actions.
 *
 * PII Audit: The `name` column is masked via maskedColumnIds={['name']} so that
 * DataTable's own <td> renderer applies data-ph-mask to the entire name cell,
 * covering both the rule name and description rendered there. This matches the
 * masking applied in AutoCategorizationRuleCard (name at :76, description at :83).
 * `description` is NOT a PII_PROPERTY_DENYLIST member (see piiScrubbing.ts —
 * removed as a bare denylist token because it false-positives on metadata
 * fields like `descriptionLength`); the `data-ph-mask` cell masking above is
 * this column's actual defense against leaking description content via
 * PostHog session replay/analytics.
 * Other columns (status, priority, conditions, category, matches) are non-PII.
 */

import { useCallback, useMemo } from 'react'
import { Edit, Filter, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { EmptyState } from '@/components/ui/empty-state'
import { Switch } from '@/components/ui/switch'
import type { CategorizationRule } from '../types'
import { UNNAMED_CUSTOM_CATEGORY_LABEL } from '../constants'
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
  /** Current sort state */
  sort?: SortState
  /** Called when sort changes (receives null to clear) */
  onSortChange?: (sort: SortState) => void
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
  if (source === 'manual') return 'bg-info/10 text-info'
  if (source === 'ai') return 'bg-primary/10 text-primary'
  if (source === 'template') return 'bg-success/10 text-success'
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
    // #1626: never the id. Same latent fallback the card carried; wording matches the backend's
    // `unnamedCustomCategoryLabel` so the rules and analytics surfaces agree.
    return rule.target_custom_category_display ?? UNNAMED_CUSTOM_CATEGORY_LABEL
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
  sort = null,
  onSortChange,
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

  const columns = useMemo((): ColumnDef<CategorizationRule>[] => [
    {
      id: 'name',
      header: 'Name',
      sortable: !!onSortChange,
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
      sortable: !!onSortChange,
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
        <Badge variant={r.enabled ? 'success' : 'secondary'}>
          {r.enabled ? 'Enabled' : 'Disabled'}
        </Badge>
      ),
    },
    {
      id: 'priority',
      header: 'Priority',
      sortable: !!onSortChange,
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
      sortable: !!onSortChange,
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
                <span key={tag} className="inline-flex items-center rounded bg-info/10 px-1.5 py-0.5 text-[0.625rem] font-medium text-info">
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
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      hideable: true,
      sortAccessor: (r) => r.match_count,
      defaultSize: 110,
      minSize: 80,
      maxSize: 140,
      key: 'match_count' as keyof CategorizationRule,
    },
  ], [onSortChange, statusOptions, categoryOptions, onPriorityRangeChange])

  // Pre-filters empty state: no data and no active filters → return external div
  if (rules.length === 0 && !hasActiveFilters) {
    return (
      <EmptyState
        icon={<Filter />}
        title="No rules found"
        description="Create your first auto-categorization rule to get started."
        className={className}
      />
    )
  }

  return (
    <DataTable<CategorizationRule>
      data={rules}
      columns={columns}
      rowKey={(r) => r.id}
      sort={sort}
      onSortChange={onSortChange!}
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
      maskedColumnIds={['name']}
      rowClassName={onRowClick ? () => 'autocat-table__row--clickable' : undefined}
      emptyState={
        <EmptyState
          size="compact"
          icon={<Filter />}
          title="No rules match your filters"
          description="Try adjusting your criteria."
        />
      }
      rowActions={hasActions ? (rule) => (
        <div className="flex items-center justify-end gap-2">
          {onToggle && (
            <Switch
              checked={rule.enabled}
              onCheckedChange={(checked) => onToggle(rule, checked)}
              disabled={loading}
              aria-label={rule.enabled ? 'Disable rule' : 'Enable rule'}
              title={rule.enabled ? 'Disable rule' : 'Enable rule'}
            />
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
              className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors duration-150 hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
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
