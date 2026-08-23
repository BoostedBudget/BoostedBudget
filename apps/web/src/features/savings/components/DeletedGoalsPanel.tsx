/**
 * DeletedGoalsPanel — Issue #1569 Task 12, gap 2.
 *
 * Deleting a savings goal no longer destroys anything (Task 8): the goal is tombstoned and every
 * contribution logged against it — including amounts the user typed in by hand, which no provider
 * can supply again — is retained. Two things followed from that and neither had a UI:
 *
 *   1. The user is never told their data was kept, so from where they sit the retention may as
 *      well not exist.
 *   2. POST /savings-goals/{id}/purge shipped, tested, with no caller. It refuses unless the goal
 *      is ALREADY deleted, and until Task 12 added GET /savings-goals/deleted no client could
 *      even discover which goals satisfy that. A user who wanted their row genuinely gone had no
 *      way to get it.
 *
 * CONFIRMATION MODEL. The backend's gate is a PRECONDITION, not type-to-confirm: the deliberate
 * step already happened when the user deleted the goal, and the purge is the second half of a
 * two-step. So this dialog is one plain destructive confirmation. Adding type-to-confirm here
 * would imply a stronger gate than the API has, and this project's rule is that destructive
 * affordances should feel deliberate rather than alarming — the deliberation is already spent.
 * (Contrast the ACCOUNT purge, which does use type-to-confirm: that one is reachable in one step
 * from a live account and destroys a whole institution's history.)
 *
 * Collapsed by default. This is a tidy-up surface, not part of the savings workflow, and a list
 * of things the user has already decided against should not compete with their live goals.
 */

import { useState } from 'react'
import { ChevronDown, Trash2 } from 'lucide-react'
import {
  useGetDeletedSavingsGoals,
  usePurgeSavingsGoal,
} from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import { Button } from '@/components/ui/button'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { ReadOnlyGuard } from '@/auth'
import { formatCurrency } from '@/shared/charts/formatters'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import type { SavingsGoal } from '../types'

function formatDeletedOn(iso: string | undefined): string {
  if (!iso) return 'an unknown date'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return 'an unknown date'
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
}

interface DeletedGoalsPanelProps {
  /** Called after a successful purge so the caller can refresh anything that counted the goal. */
  onPurged?: () => void
}

export function DeletedGoalsPanel({ onPurged }: DeletedGoalsPanelProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [goalToPurge, setGoalToPurge] = useState<SavingsGoal | null>(null)
  const trackEvent = useTrackEvent()

  const { data, isLoading, refetch } = useGetDeletedSavingsGoals()
  const purgeMutation = usePurgeSavingsGoal()

  const goals = (data ?? []) as unknown as SavingsGoal[]

  // Nothing deleted (or nothing known yet) means no panel at all — an empty "Deleted goals (0)"
  // header is clutter that tells the user nothing.
  if (isLoading || goals.length === 0) return null

  async function handlePurge(goal: SavingsGoal) {
    await purgeMutation.mutateAsync({ id: goal.id })
    trackEvent(ANALYTICS_EVENTS.SAVINGS_GOAL_PURGED)
    setGoalToPurge(null)
    await refetch()
    onPurged?.()
  }

  return (
    <section aria-labelledby="deleted-goals-heading" className="mt-8">
      <button
        type="button"
        id="deleted-goals-heading"
        onClick={() => setIsOpen((v) => !v)}
        aria-expanded={isOpen}
        className="flex w-full items-center justify-between rounded-lg border border-border px-4 py-3 text-left transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="text-sm font-medium text-foreground">
          Deleted goals ({goals.length})
        </span>
        <ChevronDown
          aria-hidden="true"
          className={`size-4 text-muted-foreground transition-transform motion-reduce:transition-none ${isOpen ? 'rotate-180' : ''}`}
        />
      </button>

      {isOpen && (
        <div className="mt-2 space-y-2">
          <p className="px-1 text-xs text-muted-foreground">
            Deleting a goal doesn&apos;t throw anything away. Every contribution you logged is
            still saved, so you can look back at what you put aside. Erase one for good below if
            you want it gone.
          </p>
          <ul className="space-y-2">
            {goals.map((goal) => (
              <li
                key={goal.id}
                className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-muted/30 px-4 py-3"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-foreground" data-ph-mask>
                    {goal.name}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Deleted on {formatDeletedOn(goal.deleted_at)} ·{' '}
                    <span data-ph-mask>
                      {formatCurrency(goal.current_amount)} of{' '}
                      {formatCurrency(goal.target_amount)}
                    </span>{' '}
                    saved
                  </p>
                </div>
                <ReadOnlyGuard>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setGoalToPurge(goal)}
                    className="text-destructive hover:bg-destructive/10"
                  >
                    <Trash2 className="size-3.5" aria-hidden="true" />
                    Delete permanently
                  </Button>
                </ReadOnlyGuard>
              </li>
            ))}
          </ul>
        </div>
      )}

      <ConfirmationDialog
        open={goalToPurge !== null}
        onOpenChange={(open) => {
          if (!open) setGoalToPurge(null)
        }}
        onConfirm={() => {
          if (goalToPurge) void handlePurge(goalToPurge)
        }}
        title="Erase this goal for good?"
        description={
          <span>
            You already deleted <strong data-ph-mask>{goalToPurge?.name}</strong>, so it is out of
            your way. Erasing it also destroys every contribution logged against it, including the
            ones you entered by hand — nothing can fetch those back. This can&apos;t be undone.
          </span>
        }
        variant="destructive"
        confirmButtonText="Delete forever"
        loading={purgeMutation.isPending}
      />
    </section>
  )
}
