// Issue #952: Notification Center — Empty state
import { Bell } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'

export function NotificationEmptyState() {
  return (
    <EmptyState
      size="compact"
      icon={<Bell />}
      title="No notifications yet"
      description="You'll see alerts about your accounts, budgets, and suggestions here."
    />
  )
}
