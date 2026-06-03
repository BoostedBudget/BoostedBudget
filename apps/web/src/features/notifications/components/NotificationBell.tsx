// Issue #952: Notification Center — Bell icon with unread badge in header
import { Bell } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { useGetUnreadNotificationCount } from '@boostedbudget/api-client/endpoints/notifications/notifications'

interface NotificationBellProps {
  onOpen: () => void
  className?: string
}

export function NotificationBell({ onOpen, className }: NotificationBellProps) {
  const { data } = useGetUnreadNotificationCount({
    query: {
      refetchInterval: 30000,
      refetchIntervalInBackground: false,
    },
  })

  const count = data?.count ?? 0
  const displayCount = count > 99 ? '99+' : String(count)

  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={onOpen}
      aria-label={count > 0 ? `Notifications — ${count} unread` : 'Notifications'}
      className={cn('relative', className)}
    >
      <Bell className="h-5 w-5" />
      {count > 0 && (
        <Badge
          data-testid="unread-badge"
          variant="destructive"
          size="sm"
          className="absolute -top-1 -right-1 min-w-[18px] h-[18px] px-1 text-[10px] leading-none flex items-center justify-center"
        >
          {displayCount}
        </Badge>
      )}
    </Button>
  )
}
