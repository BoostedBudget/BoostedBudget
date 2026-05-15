import { useCallback, useMemo } from 'react'
import {
  Edit,
  Trash2,
  Calendar,
  Zap,
  Play,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { formatRelativeTime } from '@/shared/dates'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState } from '@/shared/components/DataTable'
import { RULE_TYPE_LABELS } from '../constants'
import type { RuleType } from '../types'

export type SortColumn = 'name' | 'type' | 'status' | 'trigger' | 'lastRun' | 'priority'
export type SortDirection = 'asc' | 'desc'

// eslint-disable-next-line react-refresh/only-export-components
export const RULE_COLUMN_IDS = ['name', 'type', 'status', 'trigger', 'lastRun', 'priority'] as const

// eslint-disable-next-line react-refresh/only-export-components
export const RULE_SORTABLE_COLUMNS = new Set([
  'name',
  'type',
  'status',
  'trigger',
  'lastRun',
  'priority',
] as const)

export interface RuleTableProps {
  rules: AutomationRule[]
  onEdit?: (rule: AutomationRule) => void
  onDelete?: (rule: AutomationRule) => void
  onToggle?: (rule: AutomationRule, enabled: boolean) => void
  onTest?: (rule: AutomationRule) => void
  onRowClick?: (rule: AutomationRule) => void
  loading?: boolean
  sortable?: boolean
  sortColumn?: SortColumn
  sortDirection?: SortDirection
  onSort?: (column: SortColumn, direction: SortDirection) => void
  selectable?: boolean
  selectedIds?: string[]
  onSelectionChange?: (selectedIds: string[]) => void
  className?: string
}

export function RuleTable({
  rules,
  onEdit,
  onDelete,
  onToggle,
  onTest,
  onRowClick,
  loading = false,
  sortable = false,
  sortColumn,
  sortDirection = 'asc',
  onSort,
  selectable = false,
  selectedIds = [],
  onSelectionChange,
  className = '',
}: RuleTableProps) {
  const hasActions = !!(onEdit || onDelete || onToggle || onTest)

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('rules', [...RULE_COLUMN_IDS])

  const allSelected = useMemo(
    () => rules.length > 0 && rules.every((rule) => selectedIds.includes(rule.id)),
    [rules, selectedIds]
  )

  const handleSelectAll = useCallback(() => {
    if (!onSelectionChange) return
    onSelectionChange(allSelected ? [] : rules.map((r) => r.id))
  }, [allSelected, rules, onSelectionChange])

  const handleSelectRow = useCallback(
    (ruleId: string) => {
      if (!onSelectionChange) return
      if (selectedIds.includes(ruleId)) {
        onSelectionChange(selectedIds.filter((id) => id !== ruleId))
      } else {
        onSelectionChange([...selectedIds, ruleId])
      }
    },
    [selectedIds, onSelectionChange]
  )

  const handleToggle = useCallback(
    (rule: AutomationRule) => {
      if (onToggle && !loading) onToggle(rule, !rule.enabled)
    },
    [onToggle, loading]
  )

  // Bridge DataTable 3-state sort → RuleTable (column, direction) callback
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

  const columns: ColumnDef<AutomationRule>[] = [
    ...(selectable
      ? [
          {
            id: 'select',
            header: (
              <input
                type="checkbox"
                checked={allSelected}
                onChange={handleSelectAll}
                aria-label="Select all rules"
                className="h-4 w-4 cursor-pointer accent-primary"
              />
            ),
            widthClass: 'w-12',
            render: (row: AutomationRule) => (
              <div
                className="flex justify-center"
                onClick={(e) => e.stopPropagation()}
              >
                <input
                  type="checkbox"
                  checked={selectedIds.includes(row.id)}
                  onChange={() => handleSelectRow(row.id)}
                  aria-label={`Select ${row.name}`}
                  className="h-4 w-4 cursor-pointer accent-primary"
                />
              </div>
            ),
          },
        ]
      : []),
    {
      id: 'name',
      header: 'Name',
      sortable,
      resizable: true,
      reorderable: true,
      render: (row) => (
        <div className="flex flex-col gap-1">
          <span className="font-medium text-foreground">{row.name}</span>
          {row.description && (
            <span className="text-xs text-muted-foreground">{row.description}</span>
          )}
        </div>
      ),
    },
    {
      id: 'type',
      header: 'Type',
      sortable,
      resizable: true,
      reorderable: true,
      render: (row) => {
        const ruleType = row.rule_type as RuleType
        return <>{RULE_TYPE_LABELS[ruleType] || row.rule_type}</>
      },
    },
    {
      id: 'status',
      header: 'Status',
      sortable,
      resizable: true,
      reorderable: true,
      defaultPinned: 'left' as const,
      render: (row) => (
        <span
          className={cn(
            'inline-flex items-center rounded-md px-2 py-1 text-xs font-medium',
            row.enabled
              ? 'bg-green-50 text-green-700 dark:bg-green-900 dark:text-green-300'
              : 'bg-muted text-muted-foreground'
          )}
        >
          {row.enabled ? 'Enabled' : 'Disabled'}
        </span>
      ),
    },
    {
      id: 'trigger',
      header: 'Trigger',
      sortable,
      resizable: true,
      reorderable: true,
      render: (row) => {
        const triggerType = row.trigger_config?.type || 'schedule'
        return (
          <span className="inline-flex items-center gap-1.5">
            {triggerType === 'schedule' ? (
              <>
                <Calendar className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                Scheduled
              </>
            ) : (
              <>
                <Zap className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                Event
              </>
            )}
          </span>
        )
      },
    },
    {
      id: 'lastRun',
      header: 'Last Run',
      sortable,
      resizable: true,
      reorderable: true,
      hideable: true,
      render: (row) =>
        row.last_executed_at ? (
          <span title={new Date(row.last_executed_at).toLocaleString()}>
            Last run {formatRelativeTime(row.last_executed_at)}
          </span>
        ) : (
          <span className="italic text-muted-foreground">Never</span>
        ),
    },
    {
      id: 'priority',
      header: 'Priority',
      sortable,
      resizable: true,
      reorderable: true,
      hideable: true,
      render: (row) => <span className="tabular-nums text-muted-foreground">{row.priority}</span>,
    },
  ]

  if (rules.length === 0) {
    return (
      <div
        className={cn(
          'rounded-lg border border-dashed border-border bg-card px-6 py-12 text-center text-muted-foreground',
          className
        )}
      >
        <p>No rules found. Create your first automation rule to get started.</p>
      </div>
    )
  }

  return (
    <DataTable<AutomationRule>
      data={rules}
      columns={columns}
      rowKey={(row) => row.id}
      sort={sort}
      onSortChange={handleSortChange}
      filters={{}}
      onFiltersChange={() => {}}
      loading={loading}
      onRowClick={onRowClick}
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
      rowClassName={(row) => {
        const parts: string[] = []
        if (onRowClick) parts.push('rule-table__row--clickable')
        if (selectedIds.includes(row.id)) {
          parts.push('bg-blue-50 hover:bg-blue-100 dark:bg-blue-900 dark:hover:bg-blue-800')
        }
        return parts.join(' ')
      }}
      className={cn(loading && 'rule-table--loading', className)}
      rowActions={
        hasActions
          ? (row) => (
              <div className="flex items-center justify-end gap-2">
                {onToggle && (
                  <button
                    type="button"
                    role="switch"
                    aria-checked={row.enabled}
                    onClick={() => handleToggle(row)}
                    disabled={loading}
                    className={cn(
                      'relative h-5 w-9 shrink-0 cursor-pointer rounded-full border-none transition-colors disabled:cursor-not-allowed disabled:opacity-50',
                      row.enabled
                        ? 'bg-green-500 dark:bg-green-600'
                        : 'bg-muted-foreground/30 dark:bg-muted-foreground/20'
                    )}
                    aria-label={row.enabled ? 'Disable rule' : 'Enable rule'}
                    title={row.enabled ? 'Disable rule' : 'Enable rule'}
                  >
                    <span
                      className={cn(
                        'absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-white transition-transform',
                        row.enabled && 'translate-x-4'
                      )}
                    />
                  </button>
                )}
                {onTest && (
                  <button
                    type="button"
                    onClick={() => onTest(row)}
                    disabled={loading}
                    className="inline-flex h-8 w-8 items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&>svg]:h-4 [&>svg]:w-4"
                    aria-label="Backtest rule"
                    title="Backtest - See what this rule would have done with your recent data"
                  >
                    <Play aria-hidden="true" />
                  </button>
                )}
                {onEdit && (
                  <button
                    type="button"
                    onClick={() => onEdit(row)}
                    disabled={loading}
                    className="inline-flex h-8 w-8 items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&>svg]:h-4 [&>svg]:w-4"
                    aria-label="Edit rule"
                    title="Edit rule"
                  >
                    <Edit aria-hidden="true" />
                  </button>
                )}
                {onDelete && (
                  <button
                    type="button"
                    onClick={() => onDelete(row)}
                    disabled={loading}
                    className="inline-flex h-8 w-8 items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-red-50 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-red-900 dark:hover:text-red-400 [&>svg]:h-4 [&>svg]:w-4"
                    aria-label="Delete rule"
                    title="Delete rule"
                  >
                    <Trash2 aria-hidden="true" />
                  </button>
                )}
              </div>
            )
          : undefined
      }
    />
  )
}
