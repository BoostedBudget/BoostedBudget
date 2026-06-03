// Issue #952: Notification Center — UI types
import type { LucideIcon } from 'lucide-react'

export type InteractionPattern =
  | 'informational'
  | 'action_required'
  | 'acknowledgeable'
  | 'accept_reject'

export type NotificationUrgency = 'info' | 'low' | 'medium' | 'high' | 'critical'

export interface NotificationTypeConfig {
  icon: LucideIcon
  urgency: NotificationUrgency
  category: 'banking' | 'budget' | 'billing' | 'suggestion' | 'system'
  defaultLabel: string
}
