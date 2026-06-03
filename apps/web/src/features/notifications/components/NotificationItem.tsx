// Issue #952: Notification Center — Single notification item (all 4 patterns)
import { Link } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/shared/dates/formatRelativeTime'
import { NOTIFICATION_TYPE_CONFIG, URGENCY_COLOR_CLASS } from '../constants'
import { NotificationAcceptReject } from './NotificationAcceptReject'
import { NOTIFICATIONS_LIMIT } from '../hooks/useNotifications'
import {
  useMarkNotificationAsRead,
  getGetNotificationsQueryKey,
  getGetUnreadNotificationCountQueryKey,
} from '@boostedbudget/api-client/endpoints/notifications/notifications'
import { useQueryClient } from '@tanstack/react-query'
import type { Notification } from '@boostedbudget/api-client/models'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'

interface NotificationItemProps {
  notification: Notification
}

export function NotificationItem({ notification }: NotificationItemProps) {
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()

  const markReadMutation = useMarkNotificationAsRead({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetNotificationsQueryKey({ limit: NOTIFICATIONS_LIMIT }) })
        queryClient.invalidateQueries({ queryKey: getGetUnreadNotificationCountQueryKey() })
        // Track after confirmed success
        trackEvent(ANALYTICS_EVENTS.NOTIFICATION_READ, {
          type: notification.type,
          interaction_pattern: notification.interaction_pattern,
        })
      },
    },
  })

  const config = NOTIFICATION_TYPE_CONFIG[notification.type]
  const Icon = config?.icon
  const iconColorClass = config ? URGENCY_COLOR_CLASS[config.urgency] : 'text-muted-foreground'

  const hasActionCTA = (notification.interaction_pattern === 'action_required' ||
    notification.interaction_pattern === 'acknowledgeable') &&
    notification.action_url &&
    notification.action_label

  return (
    <div
      data-testid="notification-item"
      className={cn(
        'flex gap-3 p-3 rounded-lg cursor-pointer transition-colors',
        'hover:bg-muted/50',
        !notification.read && 'bg-accent border-l-2 border-l-primary'
      )}
      onClick={() => { if (!notification.read) markReadMutation.mutate({ id: notification.id }) }}
      role="article"
    >
      {/* Icon */}
      {Icon && (
        <div className={cn('mt-0.5 shrink-0', iconColorClass)} aria-hidden="true">
          <Icon className="h-4 w-4" />
        </div>
      )}

      {/* Content */}
      <div className="flex-1 min-w-0">
        <div className="flex items-start justify-between gap-2">
          <p className="text-sm font-medium leading-tight">{notification.title}</p>
          <time
            className="text-xs text-muted-foreground whitespace-nowrap shrink-0"
            dateTime={notification.created_at}
          >
            {formatRelativeTime(notification.created_at)}
          </time>
        </div>

        <p className="text-sm text-muted-foreground mt-0.5 leading-snug">
          {notification.message}
        </p>

        {/* action_required / acknowledgeable: CTA link button */}
        {hasActionCTA && (
          <Button
            variant="outline"
            size="sm"
            className="mt-2"
            asChild
            onClick={(e) => {
              e.stopPropagation()
              trackEvent(ANALYTICS_EVENTS.NOTIFICATION_ACTION_CLICKED, {
                type: notification.type,
                action_url: notification.action_url,
              })
            }}
          >
            <Link to={notification.action_url!}>{notification.action_label}</Link>
          </Button>
        )}

        {/* accept_reject: transfer suggestion buttons */}
        {notification.interaction_pattern === 'accept_reject' &&
          notification.transfer_suggestion_id && (
            <div onClick={(e) => e.stopPropagation()}>
              <NotificationAcceptReject
                suggestionId={notification.transfer_suggestion_id}
                amount={0}
                currencyCode="USD"
              />
            </div>
          )}
      </div>
    </div>
  )
}
