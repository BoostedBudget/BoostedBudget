/**
 * BoostAutomationHistoryPanel — Issue #1569 Task 7.3, item 1.
 *
 * `boosts` is a tombstone table. Deleting a boost sets `deleted_at` and destroys nothing, so every
 * row in `boost_executions` survives the delete. But the retention was unreachable: every
 * boost-scoped read filters on `boost_id`, and `GetBoostExecutions` calls `GetBoostByID` first,
 * which excludes tombstoned rows — so the deleted boost 404s and its retained runs vanish from the
 * user's view. `GET /api/v1/boost-executions` was built for exactly this, and its published
 * description advertises the affordance ("this also returns executions whose boost has since been
 * deleted"), yet `useGetUserBoostExecutions` had ZERO consumers in the app. The endpoint promised
 * something no screen delivered.
 *
 * WHY ACCOUNT-WIDE RATHER THAN "DELETED BOOSTS ONLY". A deleted-only surface would have to filter
 * client-side — the endpoint has no such flag — and the filter would run over ONE PAGE of a
 * chronological list. A user whose deleted boost last ran a year ago would open a panel that
 * truthfully said "nothing here" while the rows sat on page nine. Paginating the full history and
 * MARKING the deleted rows keeps the server's `total` honest and reaches every retained row.
 *
 * WHY A DELETED ROW IS IDENTIFIED CLIENT-SIDE. There are two shapes, and the boolean has to cover
 * both. A tombstoned boost still holds its `boost_id` (only `deleted_at` moved), so the row looks
 * ordinary; a PURGED boost has `boost_id` NULL, because `fk_boost_executions_boost_id` is
 * ON DELETE SET NULL. Comparing against the live boost list catches both, and the live list is
 * fetched here UNFILTERED on purpose — the parent page may be showing only some boost types, and
 * borrowing its narrowed list would label live boosts of other types as deleted.
 *
 * WHILE THE LIVE LIST IS STILL LOADING NOTHING IS MARKED. Claiming a boost is deleted because we
 * have not fetched the live ones yet would be the same class of false statement this task exists
 * to remove.
 *
 * COLLAPSED BY DEFAULT, mirroring DeletedGoalsPanel: this is a look-back surface, not part of the
 * boost workflow, and it should not compete with the user's live automations.
 */

import { useId, useMemo, useState } from 'react'
import { ChevronDown, History } from 'lucide-react'
import {
  useGetBoosts,
  useGetUserBoostExecutions,
} from '@boostedbudget/api-client/endpoints/boosts/boosts'
import type { BoostExecution } from '@boostedbudget/api-client/models'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { BoostExecutionTable } from './BoostExecutionTable'

const PAGE_SIZE = 20

export function BoostAutomationHistoryPanel() {
  const [isOpen, setIsOpen] = useState(false)
  const [offset, setOffset] = useState(0)
  const panelId = useId()
  const trackEvent = useTrackEvent()

  const { data, isLoading, error, refetch } = useGetUserBoostExecutions({
    limit: PAGE_SIZE,
    offset,
  })
  const { data: liveBoosts, isLoading: liveBoostsLoading } = useGetBoosts()

  const executions = useMemo<BoostExecution[]>(() => data?.executions ?? [], [data])
  const total = data?.total ?? executions.length

  /** `null` while the live list is unknown — see the header comment. */
  const liveBoostIds = useMemo<ReadonlySet<string> | null>(() => {
    if (liveBoostsLoading || !liveBoosts) return null
    return new Set(liveBoosts.map((b) => b.id))
  }, [liveBoosts, liveBoostsLoading])

  const isFromDeletedBoost = (execution: BoostExecution): boolean => {
    if (liveBoostIds === null) return false
    if (!execution.boost_id) return true
    return !liveBoostIds.has(execution.boost_id)
  }

  const deletedRowCount = executions.filter(isFromDeletedBoost).length

  // Nothing has ever run for this user: say nothing rather than offer an empty promise.
  if (!isLoading && !error && total === 0) return null

  function handleToggle() {
    const opening = !isOpen
    setIsOpen(opening)
    if (opening) {
      trackEvent(ANALYTICS_EVENTS.BOOST_EXECUTION_HISTORY_VIEWED, {
        has_deleted_boost_rows: deletedRowCount > 0,
      })
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const boostCell = (execution: BoostExecution) => {
    const deleted = isFromDeletedBoost(execution)
    return (
      <span className="flex flex-wrap items-center gap-1.5">
        {/* User-authored name, captured at execution time. Masked at the element so the marker
            beside it stays legible in session replay. */}
        <span className="truncate font-medium" data-ph-mask>
          {execution.boost_name ?? 'Untitled boost'}
        </span>
        {deleted && (
          // Never colour alone: the word itself carries the distinction.
          <Badge variant="outline" size="sm">
            Deleted boost
          </Badge>
        )}
      </span>
    )
  }

  return (
    <section aria-labelledby="boost-automation-history-heading" className="mt-8">
      <h2 id="boost-automation-history-heading" className="sr-only">
        Automation history
      </h2>
      <button
        type="button"
        onClick={handleToggle}
        aria-expanded={isOpen}
        aria-controls={panelId}
        className="flex w-full items-center justify-between rounded-lg border border-border px-4 py-3 text-left transition-colors motion-reduce:transition-none hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="text-sm font-medium text-foreground">
          Automation history{!isLoading && !error ? ` (${total})` : ''}
        </span>
        <ChevronDown
          aria-hidden="true"
          className={`size-4 text-muted-foreground transition-transform motion-reduce:transition-none ${isOpen ? 'rotate-180' : ''}`}
        />
      </button>

      {isOpen && (
        <div id={panelId} className="mt-2 space-y-2">
          <p className="px-1 text-xs text-muted-foreground">
            Every run of every boost you have had, including boosts you have since deleted &mdash;
            deleting a boost stops it running but keeps what it already did. Rows from a boost that
            is no longer in your list are marked &ldquo;Deleted boost&rdquo; and show the name it
            ran under.
          </p>

          {isLoading && (
            <div role="status" className="px-4 py-6 text-xs text-muted-foreground">
              Loading automation history&hellip;
            </div>
          )}

          {!isLoading && error && (
            <div role="alert" className="flex flex-col items-start gap-2 px-4 py-6 text-xs text-destructive">
              <p className="m-0">
                We could not load your automation history just now. It is still kept &mdash; try
                again in a moment.
              </p>
              <Button variant="outline" size="sm" onClick={() => void refetch()}>
                Retry
              </Button>
            </div>
          )}

          {!isLoading && !error && (
            <BoostExecutionTable
              executions={executions}
              tableId="boost-user-executions"
              boostCell={boostCell}
              emptyState={
                <EmptyState
                  size="compact"
                  icon={<History />}
                  title="No runs on this page"
                  className="py-8"
                />
              }
              pagination={
                total > PAGE_SIZE
                  ? {
                      currentPage,
                      totalPages,
                      onPageChange: (page) => setOffset((page - 1) * PAGE_SIZE),
                    }
                  : undefined
              }
            />
          )}
        </div>
      )}
    </section>
  )
}
