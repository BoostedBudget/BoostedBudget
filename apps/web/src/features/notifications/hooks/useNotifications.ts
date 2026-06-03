// Issue #952: Notification Center — main hook
import { useQueryClient } from '@tanstack/react-query'
import {
  useGetNotifications,
  useGetUnreadNotificationCount,
  useMarkNotificationAsRead,
  useMarkAllNotificationsAsRead,
  getGetNotificationsQueryKey,
  getGetUnreadNotificationCountQueryKey,
} from '@boostedbudget/api-client/endpoints/notifications/notifications'
import type { Notification } from '@boostedbudget/api-client/models'

export const NOTIFICATIONS_LIMIT = 50

export interface UseNotificationsResult {
  notifications: Notification[]
  isLoading: boolean
  unreadCount: number
  markAsRead: (id: string) => void
  markAllAsRead: () => void
  isMarkingAllRead: boolean
  invalidateNotifications: () => void
}

export function useNotifications(): UseNotificationsResult {
  const queryClient = useQueryClient()

  const notificationsQuery = useGetNotifications({ limit: NOTIFICATIONS_LIMIT })

  const unreadCountQuery = useGetUnreadNotificationCount({
    query: {
      refetchInterval: 30000,
      refetchIntervalInBackground: false,
    },
  })

  const invalidateNotifications = () => {
    queryClient.invalidateQueries({
      queryKey: getGetNotificationsQueryKey({ limit: NOTIFICATIONS_LIMIT }),
    })
    queryClient.invalidateQueries({
      queryKey: getGetUnreadNotificationCountQueryKey(),
    })
  }

  const markReadMutation = useMarkNotificationAsRead({
    mutation: { onSuccess: invalidateNotifications },
  })

  const markAllReadMutation = useMarkAllNotificationsAsRead({
    mutation: { onSuccess: invalidateNotifications },
  })

  return {
    notifications: notificationsQuery.data ?? [],
    isLoading: notificationsQuery.isLoading,
    unreadCount: unreadCountQuery.data?.count ?? 0,
    markAsRead: (id: string) => markReadMutation.mutate({ id }),
    markAllAsRead: () => markAllReadMutation.mutate(),
    isMarkingAllRead: markAllReadMutation.isPending,
    invalidateNotifications,
  }
}
