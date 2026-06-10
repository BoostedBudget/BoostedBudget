import { useCallback, useMemo } from 'react'
import {
  Edit,
  Trash2,
  Calendar,
  Zap,
  Play,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Boost } from '@boostedbudget/api-client/models'
import { formatRelativeTime } from '@/shared/dates'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState } from '@/shared/components/DataTable'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { EmptyState } from '@/components/ui/empty-state'
import { BOOST_TYPE_LABELS } from '../boostConstants'
import { formatBoostConditionsSummary, formatBoostActionSummary } from '../boostUtils'
import type { BoostType } from '../types'

export type SortColumn = 'name' | 'type' | 'status' | 'trigger' | 'lastRun' | 'priority'
export type SortDirection = 'asc' | 'desc'

// eslint-disable-next-line react-refresh/only-export-components
export const BOOST_COLUMN_IDS = ['select', 'name', 'type', 'status', 'trigger', 'lastRun', 'priority', 'details'] as const

// eslint-disable-next-line react-refresh/only-export-components
export const BOOST_SORTABLE_COLUMNS = new Set([
  'name',
  'type',
  'status',
  'trigger',
  'lastRun',
  'priority',
] as const)

export interface BoostTableProps {
  boosts: Boost[]
  onEdit?: (boost: Boost) => void
  onDelete?: (boost: Boost) => void
  onToggle?: (boost: Boost, enabled: boolean) => void
  onTest?: (boost: Boost) => void
  onRowClick?: (boost: Boost) => void
  loading?: boolean
  sort?: SortState
  onSortChange?: (sort: SortState) => void
  selectable?: boolean
  selectedIds?: string[]
  onSelectionChange?: (selectedIds: string[]) => void
  className?: string
}

export function BoostTable({
  boosts,
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
}: BoostTableProps) {
  const hasActions = !!(onEdit || onDelete || onToggle || onTest)

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('boosts', [...BOOST_COLUMN_IDS])

  const allSelected = useMemo(
    () => boosts.length > 0 && boosts.every((boost) => selectedIds.includes(boost.id)),
    [boosts, selectedIds]
  )

  const handleSelectAll = useCallback(() => {
    if (!onSelectionChange) return
    onSelectionChange(allSelected ? [] : boosts.map((b) => b.id))
  }, [allSelected, boosts, onSelectionChange])

  const handleSelectRow = useCallback(
    (boostId: string) => {
      if (!onSelectionChange) return
      if (selectedIds.includes(boostId)) {
        onSelectionChange(selectedIds.filter((id) => id !== boostId))
      } else {
        onSelectionChange([...selectedIds, boostId])
      }
    },
    [selectedIds, onSelectionChange]
  )

  const handleToggle = useCallback(
    (boost: Boost) => {
      if (onToggle && !loading) onToggle(boost, !boost.enabled)
    },
    [onToggle, loading]
  )


  const columns: ColumnDef<Boost>[] = [
    ...(selectable
      ? [
          {
            id: 'select',
            header: (
              <input
                type="checkbox"
                checked={allSelected}
                onChange={handleSelectAll}
                aria-label="Select all boosts"
                className="h-4 w-4 cursor-pointer accent-primary"
              />
            ),
            defaultSize: 48,
            minSize: 48,
            maxSize: 48,
            render: (row: Boost) => (
              <div
                className="flex justify-center"
                onClick={(e) => e.stopPropagation()}
              >
                <input
                  type="checkbox"
                  checked={selectedIds.includes(row.id)}
                  onChange={() => handleSelectRow(row.id)}
                  aria-label="Select boost"
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
          <span className="font-medium text-foreground" data-ph-mask>{row.name}</span>
          {row.description && (
            <span className="text-xs text-muted-foreground" data-ph-mask>{row.description}</span>
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
        const boostType = row.boost_type as BoostType
        return <>{BOOST_TYPE_LABELS[boostType] || row.boost_type}</>
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
        const conditionSummary = formatBoostConditionsSummary(row.conditions, row.condition_logic)
        const actionSummary = formatBoostActionSummary(row.boost_type, row.actions)
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

  if (!loading && boosts.length === 0) {
    return (
      <EmptyState
        icon={<Zap />}
        title="No boosts found"
        description="Create your first boost to get started."
        className={className}
      />
    )
  }

  return (
    <DataTable<Boost>
      data={boosts}
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
      tableId="boosts"
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
      rowClassName={(row) =>
        cn(
          onRowClick && 'boost-table__row--clickable',
          selectedIds.includes(row.id) && 'bg-accent'
        )
      }
      className={cn(loading && 'boost-table--loading', className)}
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
                      aria-label={row.enabled ? 'Disable boost' : 'Enable boost'}
                      title={row.enabled ? 'Disable boost' : 'Enable boost'}
                    />
                  )}
                  {onTest && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => onTest(row)}
                      disabled={loading}
                      aria-label="Backtest boost"
                      title="Backtest - See what this boost would have done with your recent data"
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
                      aria-label="Edit boost"
                      title="Edit boost"
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
                      aria-label="Delete boost"
                      title="Delete boost"
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
