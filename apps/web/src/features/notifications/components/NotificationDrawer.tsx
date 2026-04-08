// Issue #952: Notification Center — Sheet-based notification panel
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { NotificationItem } from './NotificationItem'
import { NotificationEmptyState } from './NotificationEmptyState'
import { useNotifications } from '../hooks/useNotifications'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { useEffect } from 'react'

interface NotificationDrawerProps {
  open: boolean
  onClose: () => void
}

export function NotificationDrawer({ open, onClose }: NotificationDrawerProps) {
  const trackEvent = useTrackEvent()
  const { notifications, unreadCount, markAllAsRead, isMarkingAllRead } = useNotifications()

  useEffect(() => {
    if (open) {
      trackEvent(ANALYTICS_EVENTS.NOTIFICATION_DRAWER_OPENED)
    }
  }, [open, trackEvent])

  const handleMarkAllAsRead = () => {
    markAllAsRead()
    trackEvent(ANALYTICS_EVENTS.NOTIFICATION_ALL_READ, { count: unreadCount })
  }

  return (
    <Sheet open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <SheetContent side="right" className="w-full sm:max-w-md flex flex-col p-0">
        <SheetHeader className="px-4 pt-4 pb-3 border-b flex-row items-center justify-between">
          <SheetTitle>Notifications</SheetTitle>
          {unreadCount > 0 && (
            <Button
              variant="ghost"
              size="sm"
              onClick={handleMarkAllAsRead}
              disabled={isMarkingAllRead}
              className="text-sm font-normal"
            >
              Mark all as read
            </Button>
          )}
        </SheetHeader>

        <ScrollArea className="flex-1">
          {notifications.length === 0 ? (
            <div className="p-4">
              <NotificationEmptyState />
            </div>
          ) : (
            <div className="flex flex-col gap-1 p-2">
              {notifications.map((notification) => (
                <NotificationItem key={notification.id} notification={notification} />
              ))}
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}
