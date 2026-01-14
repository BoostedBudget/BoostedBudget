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
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { RULE_TYPE_LABELS } from '../constants'
import type { RuleType } from '../types'
import './RuleTable.css'

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

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString()
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
        className="rule-table__sort-btn"
      >
        {label}
        <span className="rule-table__sort-icon">
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
      <div className={`rule-table__empty ${className}`.trim()}>
        <p>No rules found. Create your first automation rule to get started.</p>
      </div>
    )
  }

  return (
    <table
      className={`rule-table ${loading ? 'rule-table--loading' : ''} ${className}`.trim()}
    >
      <thead>
        <tr className="rule-table__header-row">
          {selectable && (
            <th className="rule-table__th rule-table__th--checkbox">
              <input
                type="checkbox"
                checked={allSelected}
                onChange={handleSelectAll}
                aria-label="Select all rules"
              />
            </th>
          )}
          <th className="rule-table__th">
            {renderSortableHeader('Name', 'name')}
          </th>
          <th className="rule-table__th">
            {renderSortableHeader('Type', 'type')}
          </th>
          <th className="rule-table__th">
            {renderSortableHeader('Status', 'status')}
          </th>
          <th className="rule-table__th">
            {renderSortableHeader('Trigger', 'trigger')}
          </th>
          <th className="rule-table__th">
            {renderSortableHeader('Last Run', 'lastRun')}
          </th>
          {hasActions && (
            <th className="rule-table__th rule-table__th--actions">Actions</th>
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
              className={`rule-table__row ${onRowClick ? 'rule-table__row--clickable' : ''} ${isSelected ? 'rule-table__row--selected' : ''}`}
              onClick={onRowClick ? () => onRowClick(rule) : undefined}
            >
              {selectable && (
                <td
                  className="rule-table__td rule-table__td--checkbox"
                  onClick={(e) => e.stopPropagation()}
                >
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => handleSelectRow(rule.id)}
                    aria-label={`Select ${rule.name}`}
                  />
                </td>
              )}
              <td className="rule-table__td rule-table__td--name">
                <span className="rule-table__name">{rule.name}</span>
                {rule.description && (
                  <span className="rule-table__description">
                    {rule.description}
                  </span>
                )}
              </td>
              <td className="rule-table__td">{typeLabel}</td>
              <td className="rule-table__td">
                <span
                  className={`rule-table__status ${rule.enabled ? 'rule-table__status--enabled' : 'rule-table__status--disabled'}`}
                >
                  {rule.enabled ? 'Enabled' : 'Disabled'}
                </span>
              </td>
              <td className="rule-table__td">
                <span className="rule-table__trigger">
                  {triggerType === 'schedule' ? (
                    <>
                      <Calendar
                        className="rule-table__trigger-icon"
                        aria-hidden="true"
                      />
                      Scheduled
                    </>
                  ) : (
                    <>
                      <Zap
                        className="rule-table__trigger-icon"
                        aria-hidden="true"
                      />
                      Event
                    </>
                  )}
                </span>
              </td>
              <td className="rule-table__td">
                {rule.last_executed_at ? (
                  <span title={new Date(rule.last_executed_at).toLocaleString()}>
                    Last run {formatRelativeTime(rule.last_executed_at)}
                  </span>
                ) : (
                  <span className="rule-table__never">Never</span>
                )}
              </td>
              {hasActions && (
                <td
                  className="rule-table__td rule-table__td--actions"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="rule-table__actions">
                    {onToggle && (
                      <button
                        type="button"
                        role="switch"
                        aria-checked={rule.enabled}
                        onClick={() => handleToggle(rule)}
                        disabled={loading}
                        className={`rule-table__toggle ${rule.enabled ? 'rule-table__toggle--on' : ''}`}
                        aria-label={
                          rule.enabled ? 'Disable rule' : 'Enable rule'
                        }
                        title={rule.enabled ? 'Disable rule' : 'Enable rule'}
                      >
                        <span className="rule-table__toggle-slider" />
                      </button>
                    )}
                    {onTest && (
                      <button
                        type="button"
                        onClick={() => onTest(rule)}
                        disabled={loading}
                        className="rule-table__action-btn"
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
                        className="rule-table__action-btn"
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
                        className="rule-table__action-btn rule-table__action-btn--danger"
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
