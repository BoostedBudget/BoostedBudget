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
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { EmptyState } from '@/components/ui/empty-state'
import { RULE_TYPE_LABELS } from '../constants'
import { formatRuleConditionsSummary, formatRuleActionSummary } from '../utils'
import type { RuleType } from '../types'

export type SortColumn = 'name' | 'type' | 'status' | 'trigger' | 'lastRun' | 'priority'
export type SortDirection = 'asc' | 'desc'

// eslint-disable-next-line react-refresh/only-export-components
export const RULE_COLUMN_IDS = ['select', 'name', 'type', 'status', 'trigger', 'lastRun', 'priority', 'details'] as const

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
  sort?: SortState
  onSortChange?: (sort: SortState) => void
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
  sort = null,
  onSortChange,
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
            defaultSize: 48,
            minSize: 48,
            maxSize: 48,
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
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultSize: 240,
      minSize: 160,
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
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultSize: 140,
      minSize: 100,
      maxSize: 200,
      render: (row) => {
        const ruleType = row.rule_type as RuleType
        return <>{RULE_TYPE_LABELS[ruleType] || row.rule_type}</>
      },
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
      render: (row) => (
        <Badge variant={row.enabled ? 'success' : 'secondary'}>
          {row.enabled ? 'Enabled' : 'Disabled'}
        </Badge>
      ),
    },
    {
      id: 'trigger',
      header: 'Trigger',
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultSize: 200,
      minSize: 140,
      maxSize: 280,
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
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      hideable: true,
      defaultSize: 180,
      minSize: 140,
      maxSize: 220,
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
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      hideable: true,
      defaultSize: 110,
      minSize: 80,
      maxSize: 140,
      render: (row) => <span className="tabular-nums text-muted-foreground">{row.priority}</span>,
    },
    {
      id: 'details',
      header: 'Details',
      resizable: true,
      reorderable: true,
      hideable: true,
      defaultSize: 240,
      minSize: 160,
      render: (row) => {
        const conditionSummary = formatRuleConditionsSummary(row.conditions, row.condition_logic)
        const actionSummary = formatRuleActionSummary(row.rule_type, row.actions)
        return (
          <div className="flex flex-col gap-0.5">
            {conditionSummary && (
              <span className="text-xs text-muted-foreground italic">{conditionSummary}</span>
            )}
            {actionSummary && (
              <span className="text-xs text-muted-foreground italic">{actionSummary}</span>
            )}
          </div>
        )
      },
    },
  ]

  if (!loading && rules.length === 0) {
    return (
      <EmptyState
        icon={<Zap />}
        title="No rules found"
        description="Create your first automation rule to get started."
        className={className}
      />
    )
  }

  return (
    <DataTable<AutomationRule>
      data={rules}
      columns={columns}
      rowKey={(row) => row.id}
      sort={sort}
      onSortChange={onSortChange!}
      filters={{}}
      onFiltersChange={() => {}}
      loading={loading}
      skeletonRows={5}
      onRowClick={onRowClick}
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      maskedColumnIds={['details']}
      tableId="rules"
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
      rowClassName={(row) =>
        cn(
          onRowClick && 'rule-table__row--clickable',
          selectedIds.includes(row.id) && 'bg-accent'
        )
      }
      className={cn(loading && 'rule-table--loading', className)}
      rowActions={
        hasActions
          ? (row) => (
              <ReadOnlyGuard>
                <div className="flex items-center justify-end gap-2">
                  {onToggle && (
                    <Switch
                      checked={row.enabled}
                      onCheckedChange={() => handleToggle(row)}
                      disabled={loading}
                      aria-label={row.enabled ? 'Disable rule' : 'Enable rule'}
                      title={row.enabled ? 'Disable rule' : 'Enable rule'}
                    />
                  )}
                  {onTest && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => onTest(row)}
                      disabled={loading}
                      aria-label="Backtest rule"
                      title="Backtest - See what this rule would have done with your recent data"
                    >
                      <Play aria-hidden="true" />
                    </Button>
                  )}
                  {onEdit && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => onEdit(row)}
                      disabled={loading}
                      aria-label="Edit rule"
                      title="Edit rule"
                    >
                      <Edit aria-hidden="true" />
                    </Button>
                  )}
                  {onDelete && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => onDelete(row)}
                      disabled={loading}
                      aria-label="Delete rule"
                      title="Delete rule"
                      className="text-destructive hover:text-destructive hover:bg-destructive/10"
                    >
                      <Trash2 aria-hidden="true" />
                    </Button>
                  )}
                </div>
              </ReadOnlyGuard>
            )
          : undefined
      }
    />
  )
}
