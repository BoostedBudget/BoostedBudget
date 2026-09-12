/**
 * GoalContributionsSheet
 * Issue #1300 M21 / R3-7: Contributions review panel for a savings goal
 *
 * Displays all contributions (proposed + confirmed) for a goal in a slide-over sheet.
 * Proposed contributions show Confirm + Reject actions (D6 source-b/c user review).
 *   Reject sets status='rejected' (sticky tombstone — R3-7: prevents re-proposal on sync).
 * Confirmed contributions show a Delete action only, behind a confirmation dialog: Delete is a
 *   PERMANENT hard delete (repository DeleteContribution — `DELETE FROM
 *   savings_goal_contributions`, no tombstone), and #1569 R18 T1 found it was the only
 *   explicit-destruction path on this branch reachable in a single click with no confirmation.
 * Rejected contributions are not shown (tombstone is server-side only).
 * #1625 Task 6: a contribution can also be ARCHIVED (`archived_at` set) when the goal's
 *   progress_basis round-trips between manual/contributions — it is retained and still shown
 *   here, but excluded from the goal's current_amount. Such rows are marked "Closed period"
 *   instead of Proposed/Confirmed, never offer Confirm/Reject (both 404 an archived row
 *   server-side), and still offer Delete — retained history a user chooses to destroy is
 *   destroyed whatever period it belongs to.
 * All amounts are masked with data-ph-mask (PII policy).
 */

import { useCallback, useEffect, useState } from 'react'
import { CheckIcon, Trash2Icon, PlusIcon, XIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  useListSavingsGoalContributions,
  useConfirmSavingsGoalContribution,
  useDeleteSavingsGoalContribution,
  useRejectSavingsGoalContribution,
} from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import type { SavingsGoalContribution } from '@boostedbudget/api-client/models'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { ErrorState } from '@/components/ui/error-state'
import { LogContributionDialog } from './LogContributionDialog'

export interface GoalContributionsSheetProps {
  currencyCode?: string
  contextNote?: string
  goalId: string
  goalName: string
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Called when any contribution is added, confirmed, or deleted (triggers goal refetch). */
  onContributionChanged: () => void
}

function formatCurrency(amount: number, currencyCode: string): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: currencyCode,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount)
}

function formatDate(dateStr: string): string {
  const [year, month, day] = dateStr.split('-').map(Number)
  return new Date(year, month - 1, day).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

// `archived_at` is a full ISO timestamp (unlike `occurred_on`, a plain `YYYY-MM-DD`), so it needs
// its own formatter rather than reusing `formatDate`'s date-only parsing — `formatDate` splits on
// `-` and would choke on the trailing `THH:MM:SSZ`.
function formatArchivedDate(isoTimestamp: string): string {
  return new Date(isoTimestamp).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

function sourceLabel(source: string): string {
  switch (source) {
    case 'manual': return 'Manual'
    case 'boost_match': return 'Boost match'
    case 'confirmed_transfer': return 'Transfer'
    case 'sync': return 'Auto-detected'
    default: return source
  }
}

interface ContributionRowProps {
  currencyCode: string
  contribution: SavingsGoalContribution
  goalId: string
  onConfirmed: () => void
  onRejected: () => void
  onDeleted: () => void
  /**
   * The listing this row came from survived a failed refetch, so it is real but unverified
   * (#1569 T9-FIX item 7). Every verb here writes against a contribution id we can no longer
   * vouch for — Reject leaves a sticky tombstone and Delete destroys a hand-logged amount — so
   * all three are disarmed rather than the rows hidden. Same call DeletedGoalsPanel made in
   * round 8, for the same reason.
   */
  disarmed?: boolean
}

function ContributionRow({ contribution, goalId, onConfirmed, onRejected, onDeleted, disarmed, currencyCode }: ContributionRowProps) {
  const trackEvent = useTrackEvent()
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const { mutateAsync: confirmMutate, isPending: confirming } = useConfirmSavingsGoalContribution()
  const { mutateAsync: rejectMutate, isPending: rejecting } = useRejectSavingsGoalContribution()
  const { mutateAsync: deleteMutate, isPending: deleting } = useDeleteSavingsGoalContribution()

  // #1625 Task 6: `archived_at` is set when a goal's progress_basis round-trips and the prior
  // period's contributions are retained but excluded from `current_amount`
  // (savings_contribution_repository.go ~1265). An archived row is never actionable via
  // Confirm/Reject regardless of its underlying `status` — both endpoints now 404 an archived
  // contribution (see ConfirmContribution's `archived_at IS NULL` leg above in this file's
  // backend counterpart) — so an archived *proposed* row falls through to the Delete-only
  // action area below, same as a confirmed one. Delete stays reachable: destroying your own
  // retained data is meaningful whatever period it belongs to (#1569 R18 T1's reasoning), and
  // nothing about being archived changes that.
  const isArchived = !!contribution.archived_at
  const isProposed = contribution.status === 'proposed' && !isArchived
  const busy = confirming || rejecting || deleting || disarmed === true

  const handleConfirm = useCallback(async () => {
    await confirmMutate({ goalID: goalId, id: contribution.id })
    trackEvent(ANALYTICS_EVENTS.SAVINGS_CONTRIBUTION_CONFIRMED, { goal_id: goalId, contribution_id: contribution.id })
    onConfirmed()
  }, [goalId, contribution.id, confirmMutate, trackEvent, onConfirmed])

  const handleReject = useCallback(async () => {
    await rejectMutate({ goalID: goalId, id: contribution.id })
    trackEvent(ANALYTICS_EVENTS.SAVINGS_CONTRIBUTION_REJECTED, { goal_id: goalId, contribution_id: contribution.id })
    onRejected()
  }, [goalId, contribution.id, rejectMutate, trackEvent, onRejected])

  const handleDelete = useCallback(async () => {
    await deleteMutate({ goalID: goalId, id: contribution.id })
    trackEvent(ANALYTICS_EVENTS.SAVINGS_CONTRIBUTION_DELETED, { goal_id: goalId, contribution_id: contribution.id })
    setConfirmingDelete(false)
    onDeleted()
  }, [goalId, contribution.id, deleteMutate, trackEvent, onDeleted])

  return (
    <li className="flex items-center justify-between gap-3 py-3 border-b border-border last:border-0">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-foreground" data-ph-mask>
            {formatCurrency(contribution.amount, currencyCode)}
          </span>
          {/* Archived wins over Proposed/Confirmed: a closed-period row is never "live",
              whatever status it was left in when its period closed. */}
          {isArchived && <Badge variant="outline">Closed period</Badge>}
          {!isArchived && isProposed && <Badge variant="secondary">Proposed</Badge>}
          {!isArchived && !isProposed && <Badge variant="success">Confirmed</Badge>}
        </div>
        <div className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
          <span>{formatDate(contribution.occurred_on)}</span>
          <span>·</span>
          <span>{sourceLabel(contribution.source)}</span>
        </div>
        {/*
          Same visual language as the auto-categorization rule card's quarantine explanation:
          a distinct badge plus a plain-language sentence, not colour alone. The date is the one
          piece of `archived_at` worth surfacing -- it says WHEN the period closed, which is the
          fact a user needs to reconcile this row against a progress figure that no longer
          includes it. It is fixed vocabulary plus a formatted date, never user content, so it
          stays OUTSIDE data-ph-mask.
        */}
        {isArchived && (
          <p className="mt-1 text-xs text-muted-foreground" role="status">
            Closed on {formatArchivedDate(contribution.archived_at as string)} when this goal's
            tracking basis changed. It does not count toward the current progress total.
          </p>
        )}
      </div>

      <div className="flex items-center gap-1 shrink-0">
        {isProposed ? (
          // Proposed rows: Confirm + Reject (R3-7: Reject creates sticky tombstone)
          <>
            <button
              className="flex items-center gap-1 px-2 py-1 text-xs font-medium text-success-foreground bg-success/90 border border-success/30 rounded-md hover:bg-success transition-colors disabled:opacity-50"
              onClick={() => void handleConfirm()}
              disabled={busy}
              aria-label="Confirm"
            >
              <CheckIcon className="size-3" />
              Confirm
            </button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => void handleReject()}
              disabled={busy}
              aria-label="Reject"
              className="h-auto px-2 py-1 text-xs text-destructive hover:text-destructive"
            >
              <XIcon className="size-3" />
              Reject
            </Button>
          </>
        ) : (
          // Confirmed rows: Delete only
          <Button
            size="sm"
            variant="destructive"
            onClick={() => setConfirmingDelete(true)}
            disabled={busy}
            aria-label="Delete"
            className="h-auto px-2 py-1 text-xs"
          >
            <Trash2Icon className="size-3" />
            Delete
          </Button>
        )}
      </div>

      {/*
        `&& !disarmed` is the second half of the existing disarm, and leaving it out is the hole
        DeletedGoalsPanel's first draft left behind: the dialog can already be OPEN when the
        refetch fails, and its confirm button is not the one the row's `disabled` prop touches.

        The copy names the amount and the date and says only what the endpoint does. It must NOT
        say the contribution can be restored — nothing on this branch restores one, and a false
        retention claim is the exact class #1569 keeps having to remove. It also must not
        editorialise about knock-on effects (goal progress, other contributions) that have not
        been verified here.
      */}
      <ConfirmationDialog
        open={confirmingDelete && !disarmed}
        onOpenChange={(open) => {
          if (!open) setConfirmingDelete(false)
        }}
        onConfirm={() => void handleDelete()}
        title="Delete this contribution?"
        description={
          // #1625 Task 6: an archived row already counts toward nothing, so the plain "this
          // permanently deletes the contribution" copy below reads as if deleting it changes the
          // goal's progress -- it does not, and the correction has to say so without implying
          // deletion is somehow less permanent for this row. It still is: Delete stays reachable
          // and irrevocable for an archived contribution (#1569 R18 T1's reasoning covers it too).
          isArchived ? (
            <span>
              This permanently deletes the{' '}
              <strong data-ph-mask>{formatCurrency(contribution.amount, currencyCode)}</strong> contribution
              recorded on {formatDate(contribution.occurred_on)}. It already belongs to a closed
              tracking period and does not count toward this goal's current progress — but
              deleting it is still permanent. The amount is not kept anywhere afterwards, and
              there is no way to bring it back.
            </span>
          ) : (
            <span>
              This permanently deletes the{' '}
              <strong data-ph-mask>{formatCurrency(contribution.amount, currencyCode)}</strong> contribution
              recorded on {formatDate(contribution.occurred_on)}. The amount is not kept anywhere
              afterwards, and there is no way to bring it back.
            </span>
          )
        }
        variant="destructive"
        confirmButtonText="Delete forever"
        loading={deleting}
      />
    </li>
  )
}

export function GoalContributionsSheet({
  currencyCode = 'USD',
  contextNote,
  goalId,
  goalName,
  open,
  onOpenChange,
  onContributionChanged,
}: GoalContributionsSheetProps) {
  const trackEvent = useTrackEvent()
  const [logDialogOpen, setLogDialogOpen] = useState(false)

  const { data: contributions, isLoading, error, refetch } = useListSavingsGoalContributions(goalId, {
    query: { enabled: open },
  })

  // Fire analytics once when sheet opens
  useEffect(() => {
    if (open) {
      trackEvent(ANALYTICS_EVENTS.SAVINGS_CONTRIBUTIONS_VIEWED, { goal_id: goalId })
    }
  }, [open, goalId, trackEvent])

  const handleChanged = useCallback(async () => {
    await refetch()
    onContributionChanged()
  }, [refetch, onContributionChanged])

  // R4-2: Exclude rejected tombstones from display. The row is retained server-side
  // (R3-7) as a sticky tombstone so syncs don't re-propose the same transfer-in.
  // Only proposed + confirmed rows are shown to the user.
  const visibleContributions = (contributions ?? []).filter((c) => c.status !== 'rejected')

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent className="flex flex-col gap-0 w-full sm:max-w-md">
          <SheetHeader className="pb-4 border-b border-border">
            <SheetTitle>Contributions — <span data-ph-mask>{goalName}</span></SheetTitle>
            {contextNote && <p className="text-sm text-muted-foreground">{contextNote}</p>}
          </SheetHeader>

          <div className="flex justify-end pt-4 pb-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setLogDialogOpen(true)}
              className="flex items-center gap-1.5"
            >
              <PlusIcon className="size-3.5" />
              Log contribution
            </Button>
          </div>

          <div className="flex-1 overflow-y-auto">
            {isLoading && (
              <div role="status" aria-label="Loading contributions" className="flex justify-center py-8">
                <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-primary" />
              </div>
            )}

            {/*
              Issue #1569 T9.5 — a failed read must not fall through to "No contributions yet".
              This sheet's twin, DeletedGoalContributions, already refuses to; the live ledger
              did not. The copy says the read failed and offers a retry — it makes no claim
              either way about what the ledger holds, which the client cannot know.
            */}
            {!isLoading && error && visibleContributions.length === 0 && (
              <ErrorState
                title="We couldn't load these contributions"
                description="The request failed. Try again in a moment."
                onRetry={() => void refetch()}
                className="border-0 bg-transparent"
              />
            )}

            {/*
              #1569 T9-FIX item 7. React Query keeps the last successful listing when a later
              refetch fails, so rows and `error` coexist. The rows stay — they are the user's, and
              blanking them is the same false "nothing here" by another route — but they are
              presented as UNVERIFIED, and role="status" rather than "alert" because what is on
              screen is still readable. The full ErrorState above is for the case where there is
              nothing to read at all.

              The claim is scoped to what the failure actually did: the LISTING may be stale. An
              unscoped "everything is still kept" here would be the new false retention claim this
              branch keeps having to remove.
            */}
            {!isLoading && error && visibleContributions.length > 0 && (
              <div
                role="status"
                className="mx-1 mt-2 flex flex-col items-start gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground"
              >
                <p className="m-0">
                  We could not refresh this list just now, so it may be out of date. Confirming,
                  rejecting and deleting are paused until we can check again.
                </p>
                <Button size="sm" variant="outline" onClick={() => void refetch()}>
                  Try again
                </Button>
              </div>
            )}

            {!isLoading && !error && visibleContributions.length === 0 && (
              <p className="py-8 text-center text-sm text-muted-foreground">
                No contributions yet. Use "Log contribution" to record one manually,
                or sync a linked account to detect transfers.
              </p>
            )}

            {!isLoading && visibleContributions.length > 0 && (
              <ul className="divide-y-0" aria-label="Contributions list">
                {/* Proposed first so users see them at top */}
                {visibleContributions
                  .slice()
                  .sort((a, b) => {
                    if (a.status === 'proposed' && b.status !== 'proposed') return -1
                    if (a.status !== 'proposed' && b.status === 'proposed') return 1
                    return b.occurred_on.localeCompare(a.occurred_on)
                  })
                  .map((c) => (
                    <ContributionRow
                      currencyCode={currencyCode}
                      key={c.id}
                      contribution={c}
                      goalId={goalId}
                      onConfirmed={handleChanged}
                      onRejected={handleChanged}
                      onDeleted={handleChanged}
                      disarmed={!!error}
                    />
                  ))}
              </ul>
            )}
          </div>
        </SheetContent>
      </Sheet>

      <LogContributionDialog
        currencyCode={currencyCode}
        goalId={goalId}
        isOpen={logDialogOpen}
        onClose={() => setLogDialogOpen(false)}
        onLogged={handleChanged}
      />
    </>
  )
}
