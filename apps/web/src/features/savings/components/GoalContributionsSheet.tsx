/**
 * GoalContributionsSheet
 * Issue #1300 M21 / R3-7: Contributions review panel for a savings goal
 *
 * Displays all contributions (proposed + confirmed) for a goal in a slide-over sheet.
 * Proposed contributions show Confirm + Reject actions (D6 source-b/c user review).
 *   Reject sets status='rejected' (sticky tombstone — R3-7: prevents re-proposal on sync).
 * Confirmed contributions show a Delete action only.
 * Rejected contributions are not shown (tombstone is server-side only).
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
import { LogContributionDialog } from './LogContributionDialog'

export interface GoalContributionsSheetProps {
  goalId: string
  goalName: string
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Called when any contribution is added, confirmed, or deleted (triggers goal refetch). */
  onContributionChanged: () => void
}

function formatCurrency(amount: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
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
  contribution: SavingsGoalContribution
  goalId: string
  onConfirmed: () => void
  onRejected: () => void
  onDeleted: () => void
}

function ContributionRow({ contribution, goalId, onConfirmed, onRejected, onDeleted }: ContributionRowProps) {
  const trackEvent = useTrackEvent()
  const { mutateAsync: confirmMutate, isPending: confirming } = useConfirmSavingsGoalContribution()
  const { mutateAsync: rejectMutate, isPending: rejecting } = useRejectSavingsGoalContribution()
  const { mutateAsync: deleteMutate, isPending: deleting } = useDeleteSavingsGoalContribution()

  const isProposed = contribution.status === 'proposed'
  const busy = confirming || rejecting || deleting

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
    onDeleted()
  }, [goalId, contribution.id, deleteMutate, trackEvent, onDeleted])

  return (
    <li className="flex items-center justify-between gap-3 py-3 border-b border-border last:border-0">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-foreground" data-ph-mask>
            {formatCurrency(contribution.amount)}
          </span>
          {isProposed && <Badge variant="secondary">Proposed</Badge>}
          {!isProposed && <Badge variant="success">Confirmed</Badge>}
        </div>
        <div className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
          <span>{formatDate(contribution.occurred_on)}</span>
          <span>·</span>
          <span>{sourceLabel(contribution.source)}</span>
        </div>
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
            onClick={() => void handleDelete()}
            disabled={busy}
            aria-label="Delete"
            className="h-auto px-2 py-1 text-xs"
          >
            <Trash2Icon className="size-3" />
            Delete
          </Button>
        )}
      </div>
    </li>
  )
}

export function GoalContributionsSheet({
  goalId,
  goalName,
  open,
  onOpenChange,
  onContributionChanged,
}: GoalContributionsSheetProps) {
  const trackEvent = useTrackEvent()
  const [logDialogOpen, setLogDialogOpen] = useState(false)

  const { data: contributions, isLoading, refetch } = useListSavingsGoalContributions(goalId, {
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

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent className="flex flex-col gap-0 w-full sm:max-w-md">
          <SheetHeader className="pb-4 border-b border-border">
            <SheetTitle>Contributions — <span data-ph-mask>{goalName}</span></SheetTitle>
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

            {!isLoading && (!contributions || contributions.length === 0) && (
              <p className="py-8 text-center text-sm text-muted-foreground">
                No contributions yet. Use "Log contribution" to record one manually,
                or sync a linked account to detect transfers.
              </p>
            )}

            {!isLoading && contributions && contributions.length > 0 && (
              <ul className="divide-y-0" aria-label="Contributions list">
                {/* Proposed first so users see them at top */}
                {contributions
                  .slice()
                  .sort((a, b) => {
                    if (a.status === 'proposed' && b.status !== 'proposed') return -1
                    if (a.status !== 'proposed' && b.status === 'proposed') return 1
                    return b.occurred_on.localeCompare(a.occurred_on)
                  })
                  .map((c) => (
                    <ContributionRow
                      key={c.id}
                      contribution={c}
                      goalId={goalId}
                      onConfirmed={handleChanged}
                      onRejected={handleChanged}
                      onDeleted={handleChanged}
                    />
                  ))}
              </ul>
            )}
          </div>
        </SheetContent>
      </Sheet>

      <LogContributionDialog
        goalId={goalId}
        isOpen={logDialogOpen}
        onClose={() => setLogDialogOpen(false)}
        onLogged={handleChanged}
      />
    </>
  )
}
