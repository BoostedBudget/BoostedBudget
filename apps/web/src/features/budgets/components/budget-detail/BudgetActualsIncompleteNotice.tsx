/**
 * BudgetActualsIncompleteNotice
 * Issue #1685: surface the persisted incomplete-actuals reasons on the budget page.
 *
 * `budgets.actuals_incomplete_reasons` is written by the last actuals publish (on both the
 * RETAIN and the WITHHOLD path) and read back by every user-facing budget read, so this notice
 * is a pure function of SERVER state: it renders again after a reload and offers no dismissal.
 * A persisted dismissal would contradict that guarantee, and a session-scoped one would still
 * hide the notice from a reload in the same tab — so there is none.
 *
 * Renders reason CODES only. The reason list is server-provided text and is masked for session
 * replay; the copy carries no financial amount.
 */

import { useEffect, useMemo } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

export interface BudgetActualsIncompleteNoticeProps {
  /** `budget.actuals_incomplete_reasons` — the reasons the last publish persisted. */
  reasons?: string[] | null
}

/** Distinct, non-blank reasons, in the order the server reported them. */
function distinctReasons(reasons?: string[] | null): string[] {
  if (!reasons || reasons.length === 0) return []

  const distinct: string[] = []
  for (const reason of reasons) {
    const trimmed = reason.trim()
    if (trimmed.length === 0 || distinct.includes(trimmed)) continue
    distinct.push(trimmed)
  }
  return distinct
}

export function BudgetActualsIncompleteNotice({ reasons }: BudgetActualsIncompleteNoticeProps) {
  const trackEvent = useTrackEvent()
  const displayed = useMemo(() => distinctReasons(reasons), [reasons])
  const reasonCount = displayed.length
  // Display identity: re-fire only when the rendered SET changes, not on a refetch that returns
  // an equal set under a new array identity.
  const reasonSignature = useMemo(() => displayed.join('\n'), [displayed])

  useEffect(() => {
    if (reasonCount === 0) return
    trackEvent(ANALYTICS_EVENTS.BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED, {
      reason_count: reasonCount,
    })
  }, [trackEvent, reasonCount, reasonSignature])

  if (reasonCount === 0) return null

  return (
    <Alert variant="warning" data-testid="budget-actuals-incomplete-notice" className="mb-6">
      <AlertTitle>Some budget lines kept their previous amounts</AlertTitle>
      <AlertDescription>
        <p className="m-0">
          Your changes were saved. Activity data for the lines below could not be matched with
          certainty, so they were left untouched rather than risk an inaccurate total.
        </p>
        <ul
          className="mt-2 mb-0 list-disc space-y-1 pl-5"
          data-ph-mask
          data-testid="budget-actuals-incomplete-reasons"
          aria-label="Why these lines were left untouched"
        >
          {displayed.map((reason) => (
            <li key={reason}>{reason}</li>
          ))}
        </ul>
      </AlertDescription>
    </Alert>
  )
}
