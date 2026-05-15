/**
 * RuleExecutionHistory Component
 * Issue #39: Rule Builder UI
 *
 * Displays the execution history for an automation rule.
 * Shows execution time, status, duration, and trigger type.
 */

import { Loader2, AlertCircle, ChevronLeft, ChevronRight } from 'lucide-react'
import { useGetRuleExecutions } from '@boostedbudget/api-client/endpoints/automation-rules/automation-rules'
import type { RuleExecution } from '@boostedbudget/api-client/models'
import { DataTable } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'

export interface RuleExecutionHistoryProps {
  /** Rule ID to fetch executions for */
  ruleId: string
  /** Maximum number of executions per page */
  limit?: number
  /** Offset for pagination */
  offset?: number
  /** Called when page changes */
  onPageChange?: (page: number) => void
  /** Additional CSS class */
  className?: string
}

function formatDateTime(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatDuration(ms: number | undefined): string {
  if (ms === undefined) return '-'
  const seconds = ms / 1000
  return `${seconds.toFixed(1)}s`
}

function getTriggerType(context: unknown): string {
  if (!context || typeof context !== 'object') return 'unknown'
  const ctx = context as Record<string, unknown>
  return (ctx.type as string) || 'unknown'
}

/**
 * RuleExecutionHistory displays a table of past rule executions.
 */
export function RuleExecutionHistory({
  ruleId,
  limit,
  offset = 0,
  onPageChange,
  className = '',
}: RuleExecutionHistoryProps) {
  const params = limit ? { limit, offset } : undefined
  const { data, isLoading, error, refetch } = useGetRuleExecutions(
    ruleId,
    params,
    undefined
  )

  const executions = data?.executions ?? []
  const total = (data as { total?: number } | undefined)?.total ?? executions.length
  const currentPage = limit ? Math.floor(offset / limit) : 0
  const totalPages = limit ? Math.ceil(total / limit) : 1
  const showPagination = limit && total > limit

  const statusBadgeClass = (status: string) => {
    const base = 'inline-block px-2 py-0.5 text-xs font-medium rounded-full'
    switch (status) {
      case 'completed': return `${base} status-completed bg-success/10 text-success`
      case 'failed': return `${base} status-failed bg-destructive/10 text-destructive`
      case 'running': return `${base} status-running bg-info/10 text-info`
      default: return `${base} status-pending bg-muted text-muted-foreground`
    }
  }

  // Loading state
  if (isLoading) {
    return (
      <div className={`bg-background border border-border rounded-lg overflow-hidden ${className}`.trim()}>
        <div className="flex flex-col items-center justify-center py-12 px-8 text-center gap-4">
          <Loader2 className="w-8 h-8 animate-spin text-primary" />
          <p>Loading execution history...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className={`bg-background border border-border rounded-lg overflow-hidden ${className}`.trim()}>
        <div className="flex flex-col items-center justify-center py-12 px-8 text-center gap-4">
          <AlertCircle className="w-8 h-8 text-destructive" />
          <p>Error loading execution history</p>
          <button
            type="button"
            onClick={() => refetch()}
            className="px-4 py-2 text-sm font-medium text-foreground bg-muted border border-border rounded-md cursor-pointer hover:bg-accent transition-colors"
            aria-label="Retry loading"
          >
            Retry
          </button>
        </div>
      </div>
    )
  }

  // Empty state
  if (executions.length === 0) {
    return (
      <div className={`bg-background border border-border rounded-lg overflow-hidden ${className}`.trim()}>
        <h3 className="m-0 px-6 py-4 text-base font-semibold text-foreground border-b border-border">Execution History</h3>
        <div className="p-8 text-center text-muted-foreground">
          <p>No executions yet</p>
        </div>
      </div>
    )
  }

  const columns: ColumnDef<RuleExecution>[] = [
    {
      id: 'time',
      header: 'Time',
      render: (execution) => formatDateTime(execution.started_at),
    },
    {
      id: 'status',
      header: 'Status',
      render: (execution) => (
        <span className={statusBadgeClass(execution.status)}>
          {execution.status}
        </span>
      ),
    },
    {
      id: 'duration',
      header: 'Duration',
      render: (execution) => formatDuration(execution.execution_duration_ms),
    },
    {
      id: 'trigger',
      header: 'Trigger',
      render: (execution) => (
        <span className="capitalize">{getTriggerType(execution.trigger_context)}</span>
      ),
    },
    {
      id: 'details',
      header: 'Details',
      render: (execution) => (
        <span className="max-w-[200px] block">
          {execution.status === 'failed' && execution.error_message && (
            <span className="text-destructive text-xs">{execution.error_message}</span>
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
    <div className={`bg-background border border-border rounded-lg overflow-hidden ${className}`.trim()}>
      <h3 className="m-0 px-6 py-4 text-base font-semibold text-foreground border-b border-border">Execution History</h3>

      <DataTable<RuleExecution>
        data={executions}
        columns={columns}
        rowKey={(e) => e.id}
        sort={null}
        onSortChange={() => {}}
        filters={{}}
        onFiltersChange={() => {}}
      />

      {showPagination && (
        <div className="flex items-center justify-center gap-4 p-4 border-t border-border">
          <button
            type="button"
            onClick={() => onPageChange?.(currentPage - 1)}
            disabled={currentPage === 0}
            className="flex items-center gap-1 px-3 py-2 text-sm font-medium text-foreground border border-border rounded-md cursor-pointer hover:bg-muted transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            aria-label="Previous page"
          >
            <ChevronLeft aria-hidden="true" className="w-4 h-4" />
            Prev
          </button>
          <span className="text-sm text-muted-foreground">
            Page {currentPage + 1} of {totalPages}
          </span>
          <button
            type="button"
            onClick={() => onPageChange?.(currentPage + 1)}
            disabled={currentPage >= totalPages - 1}
            className="flex items-center gap-1 px-3 py-2 text-sm font-medium text-foreground border border-border rounded-md cursor-pointer hover:bg-muted transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            aria-label="Next page"
          >
            Next
            <ChevronRight aria-hidden="true" className="w-4 h-4" />
          </button>
        </div>
      )}
    </div>
  )
}
