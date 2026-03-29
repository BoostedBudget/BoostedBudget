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
import './RuleExecutionHistory.css'

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

  // Loading state
  if (isLoading) {
    return (
      <div className={`rule-execution-history ${className}`.trim()}>
        <div className="rule-execution-history__loading">
          <Loader2 className="rule-execution-history__spinner" />
          <p>Loading execution history...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className={`rule-execution-history ${className}`.trim()}>
        <div className="rule-execution-history__error">
          <AlertCircle className="rule-execution-history__error-icon" />
          <p>Error loading execution history</p>
          <button
            type="button"
            onClick={() => refetch()}
            className="rule-execution-history__retry-btn"
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
      <div className={`rule-execution-history ${className}`.trim()}>
        <h3 className="rule-execution-history__title">Execution History</h3>
        <div className="rule-execution-history__empty">
          <p>No executions yet</p>
        </div>
      </div>
    )
  }

  return (
    <div className={`rule-execution-history ${className}`.trim()}>
      <h3 className="rule-execution-history__title">Execution History</h3>

      <div className="rule-execution-history__table-container">
        <table className="rule-execution-history__table">
          <thead>
            <tr>
              <th>Time</th>
              <th>Status</th>
              <th>Duration</th>
              <th>Trigger</th>
              <th>Details</th>
            </tr>
          </thead>
          <tbody>
            {executions.map((execution: RuleExecution) => (
              <tr key={execution.id}>
                <td>{formatDateTime(execution.started_at)}</td>
                <td>
                  <span
                    className={`rule-execution-history__status rule-execution-history__status--${execution.status}`}
                  >
                    {execution.status}
                  </span>
                </td>
                <td>{formatDuration(execution.execution_duration_ms)}</td>
                <td className="rule-execution-history__trigger">
                  {getTriggerType(execution.trigger_context)}
                </td>
                <td className="rule-execution-history__details">
                  {execution.status === 'failed' && execution.error_message && (
                    <span className="rule-execution-history__error-msg">
                      {execution.error_message}
                    </span>
                  )}
                  {execution.status === 'completed' && (
                    <span className="rule-execution-history__success">
                      Completed successfully
                    </span>
                  )}
                  {execution.status === 'running' && (
                    <span className="rule-execution-history__running">
                      In progress...
                    </span>
                  )}
                  {execution.status === 'pending' && (
                    <span className="rule-execution-history__pending">
                      Waiting to start
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showPagination && (
        <div className="rule-execution-history__pagination">
          <button
            type="button"
            onClick={() => onPageChange?.(currentPage - 1)}
            disabled={currentPage === 0}
            className="rule-execution-history__page-btn"
            aria-label="Previous page"
          >
            <ChevronLeft aria-hidden="true" />
            Prev
          </button>
          <span className="rule-execution-history__page-info">
            Page {currentPage + 1} of {totalPages}
          </span>
          <button
            type="button"
            onClick={() => onPageChange?.(currentPage + 1)}
            disabled={currentPage >= totalPages - 1}
            className="rule-execution-history__page-btn"
            aria-label="Next page"
          >
            Next
            <ChevronRight aria-hidden="true" />
          </button>
        </div>
      )}
    </div>
  )
}
