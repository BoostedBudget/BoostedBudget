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
 * ⚠ AND IF THE LIVE LIST FAILS, WE SAY SO RATHER THAN FAIL OPEN (T7-FIX, finding C). "Not loaded
 * yet" and "could not be loaded" produce the same `liveBoostIds === null`, and treating the second
 * one silently would render every retained execution WITHOUT the "Deleted boost" marker — the panel
 * would look complete and be wrong, which is precisely the failure this whole panel exists to
 * remove. Failing open on a "this data was retained" marker is the wrong direction for #1569.
 *
 * The rows are still SHOWN, deliberately: withholding retained history because a SECOND, decorative
 * request failed would destroy more of the promise than it protects. What is withheld is the
 * CLAIM — a notice says the markers may be missing, and the analytics event carries the degraded
 * flag so "no deleted rows" in the data is never confused with "we could not tell".
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
  const { data: liveBoosts, isLoading: liveBoostsLoading, error: liveBoostsError } = useGetBoosts()

  const executions = useMemo<BoostExecution[]>(() => data?.executions ?? [], [data])
  const total = data?.total ?? executions.length

  /**
   * `null` while the live list is unknown — still loading, or failed. Both mean "we cannot say
   * which boosts are deleted"; only the second is worth telling the user about, and
   * `markersUnavailable` is what distinguishes them.
   *
   * ⚠ #1569 T8-FIX, finding B. `liveBoostsError` is part of the condition, and its absence was the
   * defect: React Query keeps the last SUCCESSFUL list when a later refetch fails, so `liveBoosts`
   * and `liveBoostsError` are both set at once. Testing only `!liveBoosts` left the markers being
   * computed from the stale set while the banner below said markers were unavailable — the UI
   * contradicting itself, and a row markable "Deleted boost" from a list too old to say so. The two
   * must move together: this and `markersUnavailable` are one decision expressed twice.
   */
  const liveBoostIds = useMemo<ReadonlySet<string> | null>(() => {
    if (liveBoostsLoading || liveBoostsError || !liveBoosts) return null
    return new Set(liveBoosts.map((b) => b.id))
  }, [liveBoosts, liveBoostsLoading, liveBoostsError])

  /** The live-boost read failed, so no row can be honestly marked "Deleted boost". */
  const markersUnavailable = !liveBoostsLoading && !!liveBoostsError

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
        // Without this, a degraded view is indistinguishable in the data from a view where nothing
        // was deleted — the measurement would silently under-count the affordance it exists to
        // measure.
        deleted_markers_unavailable: markersUnavailable,
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

          {!isLoading && !error && markersUnavailable && (
            <div
              role="status"
              className="mx-1 rounded-lg border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground"
            >
              We could not check which of your boosts are still active, so rows from boosts you have
              deleted are not marked below. Every run is still listed &mdash; nothing was deleted.
            </div>
          )}

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
