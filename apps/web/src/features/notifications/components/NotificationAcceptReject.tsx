// Issue #952: Notification Center — Accept/Reject for transfer suggestions
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Check, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { useAcceptTransferSuggestion, useRejectTransferSuggestion, getGetTransferSuggestionsQueryKey } from '@boostedbudget/api-client/endpoints/transfer-suggestions/transfer-suggestions'
import { getGetNotificationsQueryKey, getGetUnreadNotificationCountQueryKey } from '@boostedbudget/api-client/endpoints/notifications/notifications'
import { NOTIFICATIONS_LIMIT } from '../hooks/useNotifications'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'

interface NotificationAcceptRejectProps {
  suggestionId: string
  amount: number
  currencyCode: string
}

type ActionStatus = 'pending' | 'accepted' | 'rejected'

const formatAmount = (amount: number, currencyCode: string) =>
  new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: currencyCode,
    maximumFractionDigits: 0,
  }).format(amount)

export function NotificationAcceptReject({
  suggestionId,
  amount,
  currencyCode,
}: NotificationAcceptRejectProps) {
  const [status, setStatus] = useState<ActionStatus>('pending')
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()

  const invalidateAll = () => {
    queryClient.invalidateQueries({ queryKey: getGetNotificationsQueryKey({ limit: NOTIFICATIONS_LIMIT }) })
    queryClient.invalidateQueries({ queryKey: getGetUnreadNotificationCountQueryKey() })
    queryClient.invalidateQueries({ queryKey: getGetTransferSuggestionsQueryKey() })
  }

  const acceptMutation = useAcceptTransferSuggestion({
    mutation: {
      onSuccess: (data) => {
        setStatus('accepted')
        invalidateAll()
        const savedAmount = data?.suggestion?.amount ?? amount
        const savedCurrency = data?.suggestion?.currency_code ?? currencyCode
        toast.success(`Saved ${formatAmount(savedAmount, savedCurrency)}!`)
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_ACCEPTED, {
          amount: savedAmount,
          source: 'notification_center',
        })
      },
    },
  })

  const rejectMutation = useRejectTransferSuggestion({
    mutation: {
      onSuccess: () => {
        setStatus('rejected')
        invalidateAll()
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_REJECTED, {
          amount,
          source: 'notification_center',
        })
      },
    },
  })

  if (status === 'accepted') {
    return (
      <Badge variant="success" size="sm">
        Accepted
      </Badge>
    )
  }

  if (status === 'rejected') {
    return (
      <Badge variant="secondary" size="sm">
        Rejected
      </Badge>
    )
  }

  return (
    <div className="flex items-center gap-2 mt-2">
      <Button
        variant="default"
        size="sm"
        disabled={acceptMutation.isPending || rejectMutation.isPending}
        onClick={() => acceptMutation.mutate({ id: suggestionId, data: {} })}
        aria-label="Accept transfer suggestion"
      >
        <Check className="h-3.5 w-3.5 mr-1" />
        Accept
      </Button>
      <Button
        variant="outline"
        size="sm"
        disabled={acceptMutation.isPending || rejectMutation.isPending}
        onClick={() => rejectMutation.mutate({ id: suggestionId, data: {} })}
        aria-label="Reject transfer suggestion"
      >
        <X className="h-3.5 w-3.5 mr-1" />
        Reject
      </Button>
    </div>
  )
}
