/**
 * DeletedGoalContributions — Issue #1569 T7, closing C2.
 *
 * Deleting a savings goal tombstones it and destroys nothing: every contribution STILL logged
 * against it is retained, including the amounts the user typed in by hand, which no provider can
 * supply again. Unqualified, that sentence is false — DELETE
 * /savings-goals/{goalID}/contributions/{id} hard-deletes a single contribution, and deleting the
 * goal afterwards cannot bring it back. T2 made those readable at GET /api/v1/savings-goals/deleted/{goalID}/contributions,
 * deliberately as a SEPARATE route rather than a flag on the live listing — the live read stays
 * structurally incapable of returning a tombstoned goal's rows.
 *
 * With no UI, though, that promise was true for an API consumer and still false for the person
 * who typed the amounts in: from their seat the retention may as well not have happened. This
 * component is the seat.
 *
 * MOUNTED ONLY WHEN EXPANDED. The query fires from this component, so a collapsed row costs
 * nothing — DeletedGoalsPanel is itself collapsed by default, and a user with many deleted goals
 * should not pay N requests to look at one.
 *
 * WHY total_confirmed COMES FROM THE SERVER. Summing the rendered rows would be wrong: proposed
 * and rejected rows are in the list (they are part of what was retained) but are excluded from
 * the total. The endpoint already computes the same figure the progress recompute would have.
 *
 * ⚠ IT SITS NEXT TO THE GOAL'S `current_amount`, AND THAT IS UNAVOIDABLE. This block used to
 * claim the total was "deliberately not compared against" the goal's frozen `current_amount` —
 * but DeletedGoalsPanel renders that figure in the row header, in the same visual block, so
 * expanding the ledger puts the two side by side whatever this component does. Declining to
 * compare them was never a mitigation; it just left the user to notice the mismatch unaided.
 *
 * They can legitimately differ: `current_amount` is frozen at the last recompute before deletion
 * and, on a `balance` or `account_percentage` goal, is an account figure rather than a sum of
 * contributions at all. So the UI says which is which, and names the basis the header figure came
 * from — the one field that explains the gap, and which was previously rendered nowhere.
 */

import {
  useListDeletedSavingsGoalContributions,
} from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import type { SavingsGoalContribution } from '@boostedbudget/api-client/models'
import { Badge } from '@/components/ui/badge'
import { formatCurrencyFull } from '@/shared/charts/formatters'
import { formatHistoryLabel } from '@/shared/dates/utils'
import { PROGRESS_BASIS_LABELS } from '../types'
import type { ProgressBasis } from '../types'

interface DeletedGoalContributionsProps {
  goalId: string
  /**
   * How the goal's own "saved" figure — rendered in the row header just above — was derived.
   * Optional because a legacy row may carry none; the reconciliation still renders, just without
   * naming a basis it cannot vouch for.
   */
  progressBasis?: string
  /** Set on the wrapper so the row's disclosure button can point `aria-controls` at it. */
  regionId: string
  /** Ties the region back to the (already-masked) goal-name element for assistive tech. */
  labelledBy: string
}

/**
 * A short, TEXT badge for what this row is. Never colour alone: every distinction here is
 * carried by the word itself, and the badge only borrows a muted background.
 *
 * `source === 'manual'` is the hand-logged tuple that `SavingsContributionRepository.Insert`
 * writes; it is the one class of contribution that cannot be reconstructed from a bank feed, so
 * it is called out by name rather than lumped in with the detected transfers.
 */
function contributionLabel(contribution: SavingsGoalContribution): string {
  if (contribution.status === 'proposed') return 'Proposed'
  if (contribution.status === 'rejected') return 'Rejected'
  // #1625 Task 6: `archived_at` means this contribution's tracking period closed (a
  // progress_basis round-trip) before the goal itself was ever deleted. `total_confirmed` here
  // comes from SumConfirmedByDeletedGoal, which deliberately INCLUDES archived rows so the total
  // agrees with everything ListByDeletedGoal renders (unlike the live SumConfirmedByGoal, which
  // excludes them) — so there is no total-vs-total mismatch to correct on this screen. What is
  // still missing without this branch is the row itself explaining why it was excluded from the
  // goal's `current_amount` while it was still alive.
  if (contribution.archived_at) return 'Closed period'
  if (contribution.source === 'manual') return 'Entered by hand'
  return 'Detected transfer'
}

function basisLabel(basis: string | undefined): string | null {
  if (!basis) return null
  return PROGRESS_BASIS_LABELS[basis as ProgressBasis] ?? null
}

export function DeletedGoalContributions({
  goalId,
  progressBasis,
  regionId,
  labelledBy,
}: DeletedGoalContributionsProps) {
  const { data, isLoading, error } = useListDeletedSavingsGoalContributions(goalId)

  // Fix round 1, item 4: `aria-labelledby` is hoisted onto EVERY branch. The row's toggle points
  // `aria-controls` at this id in all four states, and three of them previously rendered an
  // unnamed region — a screen-reader user following the control landed nowhere identifiable.
  const region = { id: regionId, 'aria-labelledby': labelledBy }

  if (isLoading) {
    return (
      <div {...region} role="status" className="px-4 py-3 text-xs text-muted-foreground">
        Loading contributions…
      </div>
    )
  }

  if (error) {
    // A failed read must never fall through to the empty state: "no contributions were ever
    // logged" is a claim about the user's money, and getting it wrong here says the retention
    // failed when it did not.
    return (
      <div {...region} role="alert" className="px-4 py-3 text-xs text-destructive">
        We could not load the contributions for this goal. They are still kept — try again in a
        moment.
      </div>
    )
  }

  const basis = basisLabel(progressBasis)
  const contributions = data?.contributions ?? []

  if (contributions.length === 0) {
    return (
      <div {...region} className="px-4 py-3 text-xs text-muted-foreground">
        {/*
          Fix round 1, item 3: NOT "were ever logged". A user can hard-delete an individual
          contribution (DELETE /savings-goals/{goalID}/contributions/{id}, wired at
          GoalContributionsSheet.tsx; `DeleteContribution` is in `allowedHistoryDeletes`), so an
          empty ledger can equally mean "you deleted them". Asserting they never existed is a
          false claim about the user's money — on the one surface built to avoid exactly that.
        */}
        No contributions are recorded for this goal.
      </div>
    )
  }

  return (
    <div {...region} className="rounded-lg border border-border bg-background px-4 py-3">
      <ul className="m-0 flex list-none flex-col gap-2 p-0">
        {contributions.map((contribution) => (
          <li
            key={contribution.id}
            className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-xs"
          >
            <span className="text-muted-foreground">
              {formatHistoryLabel(contribution.occurred_on, 'MMM d, yyyy')}
            </span>
            {/*
              Fix round 1, item 5: the shared Badge, not an ad-hoc bg-muted/text-muted-foreground
              pill — that pair computes 4.34:1 at 12px in the light theme, under the WCAG AA 4.5:1
              floor. This pill carries the one distinction that must never ride on colour, so it
              is the last place to accept a sub-AA contrast.
            */}
            <Badge variant="secondary" size="sm">
              {contributionLabel(contribution)}
            </Badge>
            <span className="ml-auto font-medium tabular-nums text-foreground" data-ph-mask>
              {formatCurrencyFull(contribution.amount)}
            </span>
          </li>
        ))}
      </ul>
      <p className="mt-3 flex items-baseline justify-between border-t border-border pt-2 text-xs m-0">
        <span className="text-muted-foreground">Confirmed total</span>
        <span className="font-semibold tabular-nums text-foreground" data-ph-mask>
          {formatCurrencyFull(data?.total_confirmed ?? 0)}
        </span>
      </p>
      {/*
        The row header above shows the goal's own "saved" amount. It is a different figure,
        derived a different way, and it is on screen at the same time — so it is named rather
        than left to look like an arithmetic error. No amounts here: this is a statement about
        where the numbers come from, not a third number.
      */}
      <p className="mt-1 text-[11px] leading-snug text-muted-foreground m-0">
        The amount saved shown above is this goal&apos;s last recorded progress
        {basis ? `, tracked from ${basis}` : ''}, frozen when you deleted it. It need not match
        this total.
      </p>
    </div>
  )
}
