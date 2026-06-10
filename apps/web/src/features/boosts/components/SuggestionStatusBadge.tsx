import { Badge } from '@/components/ui/badge'
import type { BadgeProps } from '@/components/ui/badge'

type SuggestionStatus = 'pending' | 'accepted' | 'completed' | 'rejected' | 'expired'

const STATUS_CONFIG: Record<SuggestionStatus, { label: string; variant: BadgeProps['variant'] }> = {
  pending: { label: 'Pending', variant: 'secondary' },
  accepted: { label: 'Accepted', variant: 'info' },
  completed: { label: 'Completed', variant: 'success' },
  rejected: { label: 'Rejected', variant: 'outline' },
  expired: { label: 'Expired', variant: 'secondary' },
}

interface SuggestionStatusBadgeProps {
  status: SuggestionStatus
  className?: string
}

export function SuggestionStatusBadge({ status, className }: SuggestionStatusBadgeProps) {
  const { label, variant } = STATUS_CONFIG[status]
  return (
    <Badge variant={variant} size="sm" data-status={status} className={className}>
      {label}
    </Badge>
  )
}
