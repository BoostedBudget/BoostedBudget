/**
 * RuleTable Component
 * Issue #39: Rule Builder UI
 *
 * Displays automation rules in a table format with sorting and selection.
 */

import { useCallback, useMemo } from 'react'
import {
  Edit,
  Trash2,
  Calendar,
  Zap,
  ChevronUp,
  ChevronDown,
  Play,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { formatRelativeTime } from '@/shared/dates'
import { RULE_TYPE_LABELS } from '../constants'
import type { RuleType } from '../types'

export type SortColumn = 'name' | 'type' | 'status' | 'trigger' | 'lastRun'
export type SortDirection = 'asc' | 'desc'

export interface RuleTableProps {
  /** Rules to display */
  rules: AutomationRule[]
  /** Called when edit button is clicked */
  onEdit?: (rule: AutomationRule) => void
  /** Called when delete button is clicked */
  onDelete?: (rule: AutomationRule) => void
  /** Called when toggle switch is changed */
  onToggle?: (rule: AutomationRule, enabled: boolean) => void
  /** Called when test button is clicked */
  onTest?: (rule: AutomationRule) => void
  /** Called when a row is clicked */
  onRowClick?: (rule: AutomationRule) => void
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
  /** Enable row selection */
  selectable?: boolean
  /** Currently selected rule IDs */
  selectedIds?: string[]
  /** Called when selection changes */
  onSelectionChange?: (selectedIds: string[]) => void
  /** Additional CSS class */
  className?: string
}

/**
 * RuleTable displays automation rules in a sortable, selectable table format.
 */
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

  const allSelected = useMemo(() => {
    if (rules.length === 0) return false
    return rules.every((rule) => selectedIds.includes(rule.id))
  }, [rules, selectedIds])

  const handleSort = useCallback(
    (column: SortColumn) => {
      if (!sortable || !onSort) return
      const newDirection =
        sortColumn === column && sortDirection === 'asc' ? 'desc' : 'asc'
      onSort(column, newDirection)
    },
    [sortable, onSort, sortColumn, sortDirection]
  )

  const handleSelectAll = useCallback(() => {
    if (!onSelectionChange) return
    if (allSelected) {
      onSelectionChange([])
    } else {
      onSelectionChange(rules.map((r) => r.id))
    }
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
      if (onToggle && !loading) {
        onToggle(rule, !rule.enabled)
      }
    },
    [onToggle, loading]
  )

  const renderSortableHeader = (
    label: string,
    column: SortColumn
  ) => {
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
        className="inline-flex cursor-pointer items-center gap-1 border-none bg-transparent p-0 font-[inherit] text-inherit hover:text-foreground"
      >
        {label}
        <span className="inline-flex h-4 w-4 items-center [&>svg]:h-3.5 [&>svg]:w-3.5">
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

  if (rules.length === 0) {
    return (
      <div className={cn('rounded-lg border border-dashed border-border bg-card px-6 py-12 text-center text-muted-foreground', className)}>
        <p>No rules found. Create your first automation rule to get started.</p>
      </div>
    )
  }

  return (
    <table
      className={cn(
        'w-full border-collapse overflow-hidden rounded-lg border border-border bg-card',
        loading && 'pointer-events-none opacity-60',
        className
      )}
    >
      <thead>
        <tr className="bg-muted/50 dark:bg-muted/20">
          {selectable && (
            <th className="w-12 border-b border-border px-4 py-3 text-center text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              <input
                type="checkbox"
                checked={allSelected}
                onChange={handleSelectAll}
                aria-label="Select all rules"
                className="h-4 w-4 cursor-pointer accent-primary"
              />
            </th>
          )}
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {renderSortableHeader('Name', 'name')}
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {renderSortableHeader('Type', 'type')}
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {renderSortableHeader('Status', 'status')}
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {renderSortableHeader('Trigger', 'trigger')}
          </th>
          <th className="border-b border-border px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {renderSortableHeader('Last Run', 'lastRun')}
          </th>
          {hasActions && (
            <th className="w-40 border-b border-border px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {rules.map((rule) => {
          const ruleType = rule.rule_type as RuleType
          const typeLabel = RULE_TYPE_LABELS[ruleType] || rule.rule_type
          const triggerType = rule.trigger_config?.type || 'schedule'
          const isSelected = selectedIds.includes(rule.id)

          return (
            <tr
              key={rule.id}
              className={cn(
                'transition-colors duration-150',
                onRowClick && 'cursor-pointer',
                isSelected
                  ? 'bg-blue-50 hover:bg-blue-100 dark:bg-blue-900 dark:hover:bg-blue-800'
                  : 'hover:bg-muted/30'
              )}
              onClick={onRowClick ? () => onRowClick(rule) : undefined}
            >
              {selectable && (
                <td
                  className="border-b border-border px-4 py-3.5 text-center align-middle text-sm text-foreground/80"
                  onClick={(e) => e.stopPropagation()}
                >
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => handleSelectRow(rule.id)}
                    aria-label={`Select ${rule.name}`}
                    className="h-4 w-4 cursor-pointer accent-primary"
                  />
                </td>
              )}
              <td className="border-b border-border px-4 py-3.5 align-middle text-sm text-foreground/80">
                <div className="flex flex-col gap-1">
                  <span className="font-medium text-foreground">{rule.name}</span>
                  {rule.description && (
                    <span className="text-xs text-muted-foreground">{rule.description}</span>
                  )}
                </div>
              </td>
              <td className="border-b border-border px-4 py-3.5 align-middle text-sm text-foreground/80">{typeLabel}</td>
              <td className="border-b border-border px-4 py-3.5 align-middle text-sm text-foreground/80">
                <span
                  className={cn(
                    'inline-flex items-center rounded-md px-2 py-1 text-xs font-medium',
                    rule.enabled
                      ? 'bg-green-50 text-green-700 dark:bg-green-900 dark:text-green-300'
                      : 'bg-muted text-muted-foreground'
                  )}
                >
                  {rule.enabled ? 'Enabled' : 'Disabled'}
                </span>
              </td>
              <td className="border-b border-border px-4 py-3.5 align-middle text-sm text-foreground/80">
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
              </td>
              <td className="border-b border-border px-4 py-3.5 align-middle text-sm text-foreground/80">
                {rule.last_executed_at ? (
                  <span title={new Date(rule.last_executed_at).toLocaleString()}>
                    Last run {formatRelativeTime(rule.last_executed_at)}
                  </span>
                ) : (
                  <span className="italic text-muted-foreground">Never</span>
                )}
              </td>
              {hasActions && (
                <td
                  className="border-b border-border px-4 py-3.5 text-right align-middle text-sm text-foreground/80"
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
                        className={cn(
                          'relative h-5 w-9 shrink-0 cursor-pointer rounded-full border-none transition-colors disabled:cursor-not-allowed disabled:opacity-50',
                          rule.enabled
                            ? 'bg-green-500 dark:bg-green-600'
                            : 'bg-muted-foreground/30 dark:bg-muted-foreground/20'
                        )}
                        aria-label={rule.enabled ? 'Disable rule' : 'Enable rule'}
                        title={rule.enabled ? 'Disable rule' : 'Enable rule'}
                      >
                        <span
                          className={cn(
                            'absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-white transition-transform',
                            rule.enabled && 'translate-x-4'
                          )}
                        />
                      </button>
                    )}
                    {onTest && (
                      <button
                        type="button"
                        onClick={() => onTest(rule)}
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
                        onClick={() => onEdit(rule)}
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
                        onClick={() => onDelete(rule)}
                        disabled={loading}
                        className="inline-flex h-8 w-8 items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-red-50 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-red-900 dark:hover:text-red-400 [&>svg]:h-4 [&>svg]:w-4"
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
