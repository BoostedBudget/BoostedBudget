/**
 * BoostExecutionHistory Component
 * Issue #39: Boost Builder UI
 *
 * Displays the execution history for ONE automation boost.
 * Shows execution time, status, duration, and trigger type.
 *
 * ⚠ Issue #1569: this surface CANNOT reach an execution whose boost has been deleted. It reads
 * GET /boosts/{id}/executions, whose handler calls GetBoostByID first, and that excludes
 * tombstoned rows — so a deleted boost 404s here even though every one of its executions is
 * retained. The account-wide BoostAutomationHistoryPanel is the surface that reaches them.
 *
 * The table itself lives in BoostExecutionTable so the two surfaces render rows identically.
 */

import { Loader2, AlertCircle, History } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { useGetBoostExecutions } from '@boostedbudget/api-client/endpoints/boosts/boosts'
import { BoostExecutionTable } from './BoostExecutionTable'

export interface BoostExecutionHistoryProps {
  /** Boost ID to fetch executions for */
  boostId: string
  /** Maximum number of executions per page */
  limit?: number
  /** Offset for pagination */
  offset?: number
  /** Called when page changes */
  onPageChange?: (page: number) => void
  /** Additional CSS class */
  className?: string
}

export function BoostExecutionHistory({
  boostId,
  limit,
  offset = 0,
  onPageChange,
  className = '',
}: BoostExecutionHistoryProps) {
  const params = limit ? { limit, offset } : undefined
  const { data, isLoading, error, refetch } = useGetBoostExecutions(
    boostId,
    params,
    undefined
  )

  const executions = data?.executions ?? []
  const total = (data as { total?: number } | undefined)?.total ?? executions.length
  const currentPage = limit ? Math.floor(offset / limit) : 0
  const totalPages = limit ? Math.ceil(total / limit) : 1
  const showPagination = Boolean(limit && total > limit)

  if (isLoading) {
    return (
      <div className={`bg-background border border-border rounded-lg overflow-hidden ${className}`.trim()}>
        <div className="flex flex-col items-center justify-center py-12 px-8 text-center gap-4">
          <Loader2 className="w-8 h-8 motion-safe:animate-spin motion-reduce:animate-none text-primary" />
          <p>Loading execution history...</p>
        </div>
      </div>
    )
  }

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

  if (executions.length === 0) {
    return (
      <div className={`bg-background border border-border rounded-lg overflow-hidden ${className}`.trim()}>
        <h3 className="m-0 px-6 py-4 text-base font-semibold text-foreground border-b border-border">Execution History</h3>
        <EmptyState
          size="compact"
          icon={<History />}
          title="No executions yet"
          className="py-8"
        />
      </div>
    )
  }

  return (
    <div className={`bg-background border border-border rounded-lg overflow-hidden ${className}`.trim()}>
      <h3 className="m-0 px-6 py-4 text-base font-semibold text-foreground border-b border-border">Execution History</h3>

      <BoostExecutionTable
        executions={executions}
        tableId="boost-executions-history"
        pagination={
          showPagination
            ? {
                currentPage: currentPage + 1,
                totalPages,
                onPageChange: (p) => onPageChange?.(p - 1),
              }
            : undefined
        }
      />
    </div>
  )
}
