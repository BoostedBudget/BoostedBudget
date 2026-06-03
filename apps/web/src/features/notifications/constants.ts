// Issue #952: Notification Center — type → config map
import {
  AlertTriangle,
  AlertCircle,
  Clock,
  ShieldOff,
  TrendingUp,
  CreditCard,
  CheckCircle,
  Star,
  XCircle,
  RefreshCw,
  ArrowRightLeft,
} from 'lucide-react'
import type { NotificationTypeConfig } from './types'

export const NOTIFICATION_TYPE_CONFIG: Record<string, NotificationTypeConfig> = {
  login_required: {
    icon: AlertTriangle,
    urgency: 'high',
    category: 'banking',
    defaultLabel: 'Bank Login Required',
  },
  pending_expiration: {
    icon: Clock,
    urgency: 'medium',
    category: 'banking',
    defaultLabel: 'Expiring Soon',
  },
  permission_revoked: {
    icon: ShieldOff,
    urgency: 'high',
    category: 'banking',
    defaultLabel: 'Access Revoked',
  },
  item_error: {
    icon: AlertCircle,
    urgency: 'medium',
    category: 'banking',
    defaultLabel: 'Connection Error',
  },
  budget_alert: {
    icon: TrendingUp,
    urgency: 'medium',
    category: 'budget',
    defaultLabel: 'Budget Alert',
  },
  payment_failed: {
    icon: CreditCard,
    urgency: 'critical',
    category: 'billing',
    defaultLabel: 'Payment Failed',
  },
  subscription_past_due: {
    icon: CreditCard,
    urgency: 'critical',
    category: 'billing',
    defaultLabel: 'Payment Overdue',
  },
  transaction_past_due: {
    icon: CreditCard,
    urgency: 'critical',
    category: 'billing',
    defaultLabel: 'Payment Past Due',
  },
  subscription_activated: {
    icon: CheckCircle,
    urgency: 'info',
    category: 'billing',
    defaultLabel: 'Subscription Active',
  },
  subscription_resumed: {
    icon: CheckCircle,
    urgency: 'info',
    category: 'billing',
    defaultLabel: 'Subscription Resumed',
  },
  subscription_trialing: {
    icon: Star,
    urgency: 'info',
    category: 'billing',
    defaultLabel: 'Trial Started',
  },
  subscription_cancelled: {
    icon: XCircle,
    urgency: 'low',
    category: 'billing',
    defaultLabel: 'Subscription Cancelled',
  },
  transaction_sync: {
    icon: RefreshCw,
    urgency: 'info',
    category: 'system',
    defaultLabel: 'Sync Update',
  },
  transfer_suggestion: {
    icon: ArrowRightLeft,
    urgency: 'medium',
    category: 'suggestion',
    defaultLabel: 'Transfer Suggestion',
  },
}

// Urgency → icon color class
export const URGENCY_COLOR_CLASS: Record<string, string> = {
  critical: 'text-destructive',
  high: 'text-destructive',
  medium: 'text-warning',
  low: 'text-muted-foreground',
  info: 'text-info',
}
