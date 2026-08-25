/**
 * DeletedGoalContributions — Issue #1569 T7, closing C2.
 *
 * Deleting a savings goal tombstones it and destroys nothing: every contribution logged against
 * it is retained, including the amounts the user typed in by hand, which no provider can supply
 * again. T2 made those readable at GET /api/v1/savings-goals/deleted/{goalID}/contributions,
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
 * It is deliberately NOT compared against the goal's `current_amount`, which is frozen at the
 * last recompute before deletion and may be a balance-basis figure rather than a sum of
 * contributions. Two differently-derived numbers under one heading would read as a contradiction.
 */

import {
  useListDeletedSavingsGoalContributions,
} from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import type { SavingsGoalContribution } from '@boostedbudget/api-client/models'
import { formatCurrencyFull } from '@/shared/charts/formatters'
import { formatHistoryLabel } from '@/shared/dates/utils'

interface DeletedGoalContributionsProps {
  goalId: string
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
  if (contribution.source === 'manual') return 'Entered by hand'
  return 'Detected transfer'
}

export function DeletedGoalContributions({
  goalId,
  regionId,
  labelledBy,
}: DeletedGoalContributionsProps) {
  const { data, isLoading, error } = useListDeletedSavingsGoalContributions(goalId)

  if (isLoading) {
    return (
      <div id={regionId} role="status" className="px-4 py-3 text-xs text-muted-foreground">
        Loading contributions…
      </div>
    )
  }

  if (error) {
    // A failed read must never fall through to the empty state: "no contributions were ever
    // logged" is a claim about the user's money, and getting it wrong here says the retention
    // failed when it did not.
    return (
      <div id={regionId} role="alert" className="px-4 py-3 text-xs text-destructive">
        We could not load the contributions for this goal. They are still kept — try again in a
        moment.
      </div>
    )
  }

  const contributions = data?.contributions ?? []

  if (contributions.length === 0) {
    return (
      <div id={regionId} className="px-4 py-3 text-xs text-muted-foreground">
        No contributions were ever logged against this goal.
      </div>
    )
  }

  return (
    <div
      id={regionId}
      aria-labelledby={labelledBy}
      className="rounded-lg border border-border bg-background px-4 py-3"
    >
      <ul className="m-0 flex list-none flex-col gap-2 p-0">
        {contributions.map((contribution) => (
          <li
            key={contribution.id}
            className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-xs"
          >
            <span className="text-muted-foreground">
              {formatHistoryLabel(contribution.occurred_on, 'MMM d, yyyy')}
            </span>
            <span className="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
              {contributionLabel(contribution)}
            </span>
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
    </div>
  )
}
