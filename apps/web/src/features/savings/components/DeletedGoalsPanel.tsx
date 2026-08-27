/**
 * DeletedGoalsPanel — Issue #1569 Task 12, gap 2.
 *
 * Deleting a savings goal no longer destroys anything (Task 8): the goal is tombstoned and every
 * contribution STILL logged against it — including amounts the user typed in by hand, which no
 * provider can supply again — is retained. "Still logged" is the whole qualifier: DELETE
 * /savings-goals/{goalID}/contributions/{id} is a hard delete, so a contribution the user
 * explicitly deleted earlier was destroyed then and no tombstone brings it back. Two things
 * followed from that and neither had a UI:
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

import { useId, useState } from 'react'
import { ChevronDown, ChevronRight, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import {
  useGetDeletedSavingsGoals,
  usePurgeSavingsGoal,
} from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import { Button } from '@/components/ui/button'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { ReadOnlyGuard } from '@/auth'
import { mutationFailureMessage } from '@/features/admin/lib/mutationFailureMessage'
import { formatCurrency } from '@/shared/charts/formatters'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { DeletedGoalContributions } from './DeletedGoalContributions'
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
  const panelId = useId()
  const [goalToPurge, setGoalToPurge] = useState<SavingsGoal | null>(null)
  // Which goals' ledgers are open. A Set, not a single id, because comparing two deleted goals
  // is the obvious thing to want to do here.
  const [openLedgers, setOpenLedgers] = useState<ReadonlySet<string>>(() => new Set())
  const trackEvent = useTrackEvent()

  const { data, isLoading, isError, refetch } = useGetDeletedSavingsGoals()
  const purgeMutation = usePurgeSavingsGoal()

  const goals = (data ?? []) as unknown as SavingsGoal[]

  // The listing on screen survived a failed refetch, so it is real but unverified. The one verb on
  // this panel destroys hand-logged money permanently; it stays disarmed until we can refresh.
  // See the degraded notice below for why this is a disarm rather than a re-verify.
  const purgeDisarmed = isError && goals.length > 0

  // Nothing deleted (or nothing known yet) means no panel at all — an empty "Deleted goals (0)"
  // header is clutter that tells the user nothing.
  //
  // ⚠ #1569 R8. `isError` is load-bearing and its absence was the defect: without it a FAILED
  // read collapsed into the same `goals = []` as a successful empty one, and the panel rendered
  // as ABSENT. An absent panel is not "we don't know yet" to the person reading it — it is "you
  // have deleted nothing", a false statement about money we did in fact keep. That is this
  // branch's core defect class, and this was the first surface it built.
  if (isLoading) return null
  if (isError && goals.length === 0) {
    return (
      <section aria-labelledby="deleted-goals-heading" className="mt-8">
        <h2 id="deleted-goals-heading" className="sr-only">
          Deleted goals
        </h2>
        {/*
          role="alert", not a collapsed disclosure: an error hidden behind a closed panel is
          indistinguishable from the absent panel we are fixing. Wording copied from
          DeletedGoalContributions / ClosedAccountBalanceHistory / HoldingsTable — name the
          failure, never imply the retention failed with it. No amounts or names are rendered
          here, so there is nothing to mask.
        */}
        <div
          role="alert"
          className="flex flex-col items-start gap-2 rounded-lg border border-destructive/40 px-4 py-3 text-xs text-destructive"
        >
          <p className="m-0">
            We could not load the goals you have deleted. They are still kept &mdash; try again in
            a moment.
          </p>
          <Button variant="outline" size="sm" onClick={() => void refetch()}>
            Retry
          </Button>
        </div>
      </section>
    )
  }
  if (goals.length === 0) return null

  function toggleLedger(goalId: string) {
    setOpenLedgers((open) => {
      const next = new Set(open)
      if (!next.delete(goalId)) next.add(goalId)
      return next
    })
    // Fired on OPEN only, and outside the updater — a React state updater can be invoked twice
    // under StrictMode, which would double-count. Deliberately carries no goal identity: this is
    // a rate ("did anyone go and look at the money we kept?"), not a record of whose goal it was.
    if (!openLedgers.has(goalId)) trackEvent(ANALYTICS_EVENTS.SAVINGS_GOAL_DELETED_LEDGER_VIEWED)
  }

  async function handlePurge(goal: SavingsGoal) {
    try {
      await purgeMutation.mutateAsync({ id: goal.id })
      trackEvent(ANALYTICS_EVENTS.SAVINGS_GOAL_PURGED)
      setGoalToPurge(null)
      await refetch()
      onPurged?.()
    } catch (err) {
      // Both rejections are reachable: ErrSavingsGoalNotFound (a stale listing — the goal was
      // already purged, e.g. in another tab) and ErrSavingsGoalNotDeleted
      // (savings_goal_repository.go, the already-deleted precondition). Without this the promise
      // rejected unhandled, the dialog stayed open with its button re-armed and no message, and
      // the refetch never ran so the stale row sat there inviting a retry that would fail again.
      //
      // Mirrors AccountsPage.handlePurgeAccount deliberately. mutationFailureMessage is the shared
      // RFC 9457 extractor: the actionable text is at err.response.data.detail, while axios's
      // err.message is the useless "Request failed with status code 409".
      console.error('Failed to purge savings goal:', err)
      toast.error(mutationFailureMessage(err))
      // Close and refetch on failure too — whatever went wrong, the listing on screen is the
      // thing least likely to still be right.
      setGoalToPurge(null)
      await refetch()
    }
  }

  return (
    <section aria-labelledby="deleted-goals-heading" className="mt-8">
      <h2 id="deleted-goals-heading" className="sr-only">
        Deleted goals
      </h2>
      <button
        type="button"
        onClick={() => setIsOpen((v) => !v)}
        aria-expanded={isOpen}
        aria-controls={panelId}
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
        <div id={panelId} className="mt-2 space-y-2">
          {/*
            React Query keeps the last successful listing when a later refetch fails, so `goals`
            can be real rows AND `isError` true at the same time. We keep the rows — they are the
            user's, and blanking them would be the same false "nothing deleted" by another route
            — but we refuse to present them as current, because the one verb on this panel
            destroys money permanently and acting on a listing we know is unverified is exactly
            the mistake to prevent. Same degraded-mode shape as
            BoostAutomationHistoryPanel's markers notice; role="status", not "alert", because
            what is on screen is still usable.

            ⚠ #1569 T8-FIX, finding A. Saying so was only half the decision: the verb stayed armed
            on the very rows we had just declared unverified. `purgeDisarmed` finishes it. Disabling
            rather than re-verifying inside handlePurge is deliberate — a re-verify needs the same
            read that just failed, and the backend precondition (already-deleted) cannot tell a
            stale row from a current one, so it would refuse or succeed for the wrong reason. Retry
            lives in this notice so the user is not stuck.

            ⚠ #1569 T8-FIX, finding C. This notice used to end "Everything you deleted is still
            kept", two lines above this component's own correction that a purge DOES erase a goal
            for good — a NEW false retention claim shipped by the round removing false retention
            claims. The claim is now scoped to what the failure actually did (the LISTING may be
            stale; what is still deleted is still kept) and pinned by a NEGATIVE assertion, because
            a positive check stays green if the unscoped sentence comes back.
          */}
          {isError && (
            <div
              role="status"
              className="mx-1 flex flex-col items-start gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground"
            >
              <p className="m-0">
                We could not refresh this list just now, so it may be out of date &mdash; a goal you
                erased elsewhere could still appear here. Whatever is still deleted is still kept.
                Permanent deletion is paused until we can check again.
              </p>
              <Button variant="outline" size="sm" onClick={() => void refetch()}>
                Retry
              </Button>
            </div>
          )}
          <p className="px-1 text-xs text-muted-foreground">
            Deleting a goal doesn&apos;t throw anything away on its own &mdash; every contribution
            still logged against it is kept here, so you can look back at what you put aside. Open a
            goal to see them. A contribution you deleted yourself was erased at that point and is
            not here. Two things do erase the rest for good: the button below, and permanently
            deleting the account the goal was tracking.
          </p>
          {/*
            N identical "Delete permanently" buttons are indistinguishable in a screen reader's
            element list, on the one control in this app that destroys hand-logged money — so each
            button's name says WHICH goal.

            Composed with aria-labelledby rather than aria-label, and that is a PII decision: the
            goal name is user-entered, and an aria-label is an ATTRIBUTE — session replay records
            attribute values verbatim and no mask can cover them. Pointing at the already-masked
            name element gives assistive tech the same string without writing it into the DOM as
            an attribute value.

            This verb span is rendered ONCE and shared by every row; one id per row would be N
            duplicate ids.
          */}
          <span id={`${panelId}-action`} className="sr-only">
            Delete permanently:
          </span>
          {/*
            Same PII reasoning as the verb span above, for the ledger disclosure: N identical
            "Contributions" buttons are indistinguishable in a screen reader's element list, and
            the goal name must reach assistive tech through aria-labelledby rather than an
            aria-label, because session replay records attribute values verbatim and no mask can
            cover them. Rendered ONCE and shared by every row — one id per row would be N
            duplicate ids.
          */}
          <span id={`${panelId}-ledger-action`} className="sr-only">
            Contributions:
          </span>
          <ul className="space-y-2">
            {goals.map((goal) => (
              <li
                key={goal.id}
                className="rounded-lg border border-border bg-muted/30 px-4 py-3"
              >
                <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <p
                    id={`${panelId}-name-${goal.id}`}
                    className="truncate text-sm font-medium text-foreground"
                    data-ph-mask
                  >
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
                <div className="flex items-center gap-2">
                  {/*
                    #1569 T7 / C2. Without this the retained ledger was reachable only by API:
                    the DELETE description promised the contributions were kept, and from the
                    user's seat that promise was unverifiable. Mounted only while expanded, so a
                    collapsed row issues no request.
                  */}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => toggleLedger(goal.id)}
                    aria-expanded={openLedgers.has(goal.id)}
                    aria-controls={`${panelId}-ledger-${goal.id}`}
                    aria-labelledby={`${panelId}-ledger-action ${panelId}-name-${goal.id}`}
                  >
                    <ChevronRight
                      aria-hidden="true"
                      className={`size-3.5 transition-transform motion-reduce:transition-none ${
                        openLedgers.has(goal.id) ? 'rotate-90' : ''
                      }`}
                    />
                    <span aria-hidden="true">Contributions</span>
                  </Button>
                  <ReadOnlyGuard>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={purgeDisarmed}
                      onClick={() => setGoalToPurge(goal)}
                      className="text-destructive hover:bg-destructive/10"
                      aria-labelledby={`${panelId}-action ${panelId}-name-${goal.id}`}
                    >
                      <Trash2 className="size-3.5" aria-hidden="true" />
                      <span aria-hidden="true">Delete permanently</span>
                    </Button>
                  </ReadOnlyGuard>
                </div>
                </div>
                {openLedgers.has(goal.id) && (
                  <div className="mt-3">
                    <DeletedGoalContributions
                      goalId={goal.id}
                      progressBasis={goal.progress_basis}
                      regionId={`${panelId}-ledger-${goal.id}`}
                      labelledBy={`${panelId}-name-${goal.id}`}
                    />
                  </div>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {/*
        ⚠ `!purgeDisarmed` is the second half of the disarm, and leaving it out was the hole the
        first draft of that fix left behind: the dialog can already be OPEN when the refetch fails,
        and its confirm button is not the one the disabled prop touches. The confirmation closes
        with the listing it was raised from — a confirmation is a statement about a row we have
        just stopped vouching for.
      */}
      <ConfirmationDialog
        open={goalToPurge !== null && !purgeDisarmed}
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
