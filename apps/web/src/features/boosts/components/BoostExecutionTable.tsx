/**
 * BoostExecutionTable — the single renderer for a list of boost executions.
 *
 * Issue #1569 Task 7.3. Extracted from BoostExecutionHistory so the account-wide history added in
 * this task does not become a second, drifting renderer for the same rows. The two callers differ
 * in exactly one way: the account-wide one has to say WHICH automation each row belongs to,
 * because it can show rows from boosts that no longer exist. That is the `boostCell` prop.
 *
 * ⚠ MASKING IS PER ELEMENT HERE, NOT PER COLUMN. `DataTable` applies `data-ph-mask` to a `<td>`
 * only when the column's id appears in `maskedColumnIds`, which would mask the WHOLE cell. Both
 * cells that carry PII here are mixed: the boost cell pairs a user-authored name with a "Deleted
 * boost" marker, and the details cell pairs a possibly-sensitive error string with fixed status
 * text. Masking the cell would erase the very signal (which rows are deleted, what the outcome
 * was) that makes the surface readable in session replay, so the mask goes on the inner element
 * and the fixed text stays legible — the pattern PlaidOrphanedItemsPage uses for the same reason.
 */

import type { ReactNode } from 'react'
import { Badge } from '@/components/ui/badge'
import type { BadgeProps } from '@/components/ui/badge'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import type { BoostExecution } from '@boostedbudget/api-client/models'

const BOOST_EXECUTION_COLUMN_IDS = ['time', 'status', 'duration', 'trigger', 'details']

const STATUS_BADGE_VARIANT: Record<string, BadgeProps['variant']> = {
  completed: 'success',
  failed: 'destructive',
  running: 'info',
  pending: 'secondary',
}

function formatExecutionDateTime(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatExecutionDuration(ms: number | undefined): string {
  if (ms === undefined) return '-'
  const seconds = ms / 1000
  return `${seconds.toFixed(1)}s`
}

function getTriggerType(context: unknown): string {
  if (!context || typeof context !== 'object') return 'unknown'
  const ctx = context as Record<string, unknown>
  return (ctx.type as string) || 'unknown'
}

export interface BoostExecutionTableProps {
  executions: BoostExecution[]
  /** Namespaces the user's column-sizing preferences; must be stable per surface. */
  tableId: string
  /**
   * When supplied, a leading "Boost" column is rendered with this cell. Only the account-wide
   * history needs it — a per-boost table already knows whose runs it is showing.
   */
  boostCell?: (execution: BoostExecution) => ReactNode
  loading?: boolean
  emptyState?: ReactNode
  /** Pagination is optional: the per-boost table only shows it when a page limit is in play. */
  pagination?: {
    currentPage: number
    totalPages: number
    onPageChange: (page: number) => void
  }
}

export function BoostExecutionTable({
  executions,
  tableId,
  boostCell,
  loading,
  emptyState,
  pagination,
}: BoostExecutionTableProps) {
  const columnIds = boostCell
    ? ['boost', ...BOOST_EXECUTION_COLUMN_IDS]
    : BOOST_EXECUTION_COLUMN_IDS
  const { sizing, onSizingChange } = useTablePreferences(tableId, columnIds)

  const columns: ColumnDef<BoostExecution>[] = [
    ...(boostCell
      ? [
          {
            id: 'boost',
            header: 'Boost',
            resizable: true,
            defaultSize: 220,
            minSize: 140,
            render: boostCell,
          } as ColumnDef<BoostExecution>,
        ]
      : []),
    {
      id: 'time',
      header: 'Time',
      resizable: true,
      defaultSize: 180,
      minSize: 140,
      maxSize: 220,
      render: (execution) => formatExecutionDateTime(execution.started_at),
    },
    {
      id: 'status',
      header: 'Status',
      defaultSize: 110,
      minSize: 90,
      maxSize: 140,
      render: (execution) => (
        <Badge
          variant={STATUS_BADGE_VARIANT[execution.status] ?? 'secondary'}
          className={`status-${execution.status}`}
        >
          {execution.status}
        </Badge>
      ),
    },
    {
      id: 'duration',
      header: 'Duration',
      defaultSize: 110,
      minSize: 80,
      maxSize: 140,
      render: (execution) => formatExecutionDuration(execution.execution_duration_ms),
    },
    {
      id: 'trigger',
      header: 'Trigger',
      resizable: true,
      defaultSize: 200,
      minSize: 140,
      render: (execution) => (
        <span className="capitalize">{getTriggerType(execution.trigger_context)}</span>
      ),
    },
    {
      id: 'details',
      header: 'Details',
      resizable: true,
      defaultSize: 320,
      minSize: 200,
      render: (execution) => (
        <span className="max-w-[200px] block">
          {execution.status === 'failed' && execution.error_message && (
            // A failure message is provider/rule text and can name an amount or a merchant.
            <span className="text-destructive text-xs" data-ph-mask>
              {execution.error_message}
            </span>
          )}
          {execution.status === 'completed' && (
            <span className="text-success text-xs">Completed successfully</span>
          )}
          {execution.status === 'running' && (
            <span className="text-info text-xs">In progress...</span>
          )}
          {execution.status === 'pending' && (
            <span className="text-muted-foreground text-xs">Waiting to start</span>
          )}
        </span>
      ),
    },
  ]

  return (
    <>
      <DataTable<BoostExecution>
        data={executions}
        columns={columns}
        rowKey={(e) => e.id}
        tableId={tableId}
        sort={null}
        onSortChange={() => {}}
        filters={{}}
        onFiltersChange={() => {}}
        columnSizing={sizing}
        onColumnSizingChange={onSizingChange}
        loading={loading}
        emptyState={emptyState}
      />

      {pagination && (
        <div className="p-4 border-t border-border">
          <TablePagination
            currentPage={pagination.currentPage}
            totalPages={pagination.totalPages}
            hasMore={pagination.currentPage < pagination.totalPages}
            onPageChange={pagination.onPageChange}
          />
        </div>
      )}
    </>
  )
}
