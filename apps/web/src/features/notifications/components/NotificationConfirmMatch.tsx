// Issue #972: Savings Transfer Automation — confirm fuzzy-matched transfer amount
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Check, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  useConfirmTransferSuggestionMatch,
  getGetTransferSuggestionsQueryKey,
} from '@boostedbudget/api-client/endpoints/transfer-suggestions/transfer-suggestions'
import { getGetNotificationsQueryKey, getGetUnreadNotificationCountQueryKey } from '@boostedbudget/api-client/endpoints/notifications/notifications'
import { NOTIFICATIONS_LIMIT } from '../hooks/useNotifications'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

interface NotificationConfirmMatchProps {
  suggestionId: string
  onDismiss: () => void
  onConfirm?: () => void
}

type ActionStatus = 'pending' | 'confirmed'

export function NotificationConfirmMatch({ suggestionId, onDismiss, onConfirm }: NotificationConfirmMatchProps) {
  const [amount, setAmount] = useState<string>('')
  const [validationError, setValidationError] = useState<string | null>(null)
  const [status, setStatus] = useState<ActionStatus>('pending')
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()

  const confirmMutation = useConfirmTransferSuggestionMatch({
    mutation: {
      onSuccess: () => {
        setStatus('confirmed')
        queryClient.invalidateQueries({ queryKey: getGetNotificationsQueryKey({ limit: NOTIFICATIONS_LIMIT }) })
        queryClient.invalidateQueries({ queryKey: getGetUnreadNotificationCountQueryKey() })
        queryClient.invalidateQueries({ queryKey: getGetTransferSuggestionsQueryKey() })
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_VALIDATED)
        onConfirm?.()
      },
      onError: () => {
        toast.error('Failed to confirm transfer. Please try again.')
      },
    },
  })

  if (status === 'confirmed') {
    return (
      <Badge variant="success" size="sm">
        Confirmed
      </Badge>
    )
  }

  function handleConfirm() {
    const parsed = parseFloat(amount)
    if (!amount || isNaN(parsed) || parsed <= 0) {
      setValidationError('Amount must be greater than 0')
      return
    }
    setValidationError(null)
    confirmMutation.mutate({ id: suggestionId, data: { actual_amount: parsed } })
  }

  return (
    <div className="mt-2 space-y-2">
      <label className="block text-xs text-muted-foreground font-medium">
        Actual amount transferred
      </label>
      <input
        type="number"
        min="0.01"
        step="0.01"
        placeholder="0.00"
        value={amount}
        onChange={(e) => {
          setAmount(e.target.value)
          if (validationError) setValidationError(null)
        }}
        data-ph-mask
        className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:opacity-60"
        disabled={confirmMutation.isPending}
        aria-label="Actual amount transferred"
      />
      {validationError && (
        <p className="text-xs text-destructive">{validationError}</p>
      )}
      <div className="flex items-center gap-2">
        <Button
          variant="default"
          size="sm"
          disabled={confirmMutation.isPending}
          onClick={handleConfirm}
          aria-label="Confirm transfer match"
        >
          <Check className="h-3.5 w-3.5 mr-1" />
          Confirm
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={confirmMutation.isPending}
          onClick={onDismiss}
          aria-label="Dismiss"
        >
          <X className="h-3.5 w-3.5 mr-1" />
          Dismiss
        </Button>
      </div>
    </div>
  )
}
