/**
 * LogContributionDialog
 * Issue #1300 M21: Manual savings goal contribution form
 *
 * Allows users to manually log a confirmed contribution for a savings goal.
 * Amount must be positive; date defaults to today.
 */

import { useState, useCallback } from 'react'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { NumberField, DateField } from './form'
import { useLogSavingsGoalContribution } from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

export interface LogContributionDialogProps {
  currencyCode?: string
  goalId: string
  isOpen: boolean
  onClose: () => void
  onLogged: () => void
}

function todayLocalDate(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function LogContributionDialog({ goalId, isOpen, onClose, onLogged, currencyCode }: LogContributionDialogProps) {
  const trackEvent = useTrackEvent()
  const { mutateAsync, isPending } = useLogSavingsGoalContribution()

  const [amount, setAmount] = useState('')
  const [occurredOn, setOccurredOn] = useState(todayLocalDate())
  const [amountError, setAmountError] = useState<string | undefined>()
  const [submitError, setSubmitError] = useState<string | undefined>()

  const handleClose = useCallback(() => {
    setAmount('')
    setOccurredOn(todayLocalDate())
    setAmountError(undefined)
    setSubmitError(undefined)
    onClose()
  }, [onClose])

  const handleSubmit = useCallback(async () => {
    const parsed = parseFloat(amount)
    if (!amount || isNaN(parsed) || parsed <= 0) {
      setAmountError('Amount must be greater than zero')
      return
    }
    setAmountError(undefined)
    setSubmitError(undefined)

    try {
      await mutateAsync({
        goalID: goalId,
        data: {
          amount: parsed,
          occurred_on: occurredOn,
        },
      })
      trackEvent(ANALYTICS_EVENTS.SAVINGS_CONTRIBUTION_LOGGED, { goal_id: goalId, amount: parsed })
      handleClose()
      onLogged()
    } catch {
      setSubmitError('Failed to log contribution. Please try again.')
    }
  }, [amount, occurredOn, goalId, mutateAsync, trackEvent, handleClose, onLogged])

  return (
    <Dialog open={isOpen} onOpenChange={(open) => { if (!open) handleClose() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Log contribution</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          <NumberField
            name="amount"
            label={currencyCode ? `Amount (${currencyCode})` : 'Amount'}
            value={amount}
            onChange={(e) => { setAmount(e.target.value); setAmountError(undefined) }}
            error={amountError}
            placeholder="0.00"
            min="0.01"
            step="0.01"
            prefix={new Intl.NumberFormat('en-US', { style: 'currency', currency: currencyCode ?? 'USD' }).formatToParts(0).find((part) => part.type === 'currency')?.value}
          />

          <DateField
            name="occurred_on"
            label="Date"
            value={occurredOn}
            onChange={(e) => setOccurredOn(e.target.value)}
          />

          {submitError && (
            <p className="text-sm text-destructive">{submitError}</p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={isPending}>
            Cancel
          </Button>
          <Button onClick={() => void handleSubmit()} disabled={isPending} aria-label="Log contribution">
            {isPending ? 'Saving…' : 'Log contribution'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
