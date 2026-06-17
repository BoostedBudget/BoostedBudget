import { Badge } from '@/components/ui/badge'
import type { ActivityType } from '@/features/transactions/types'
import { ACTIVITY_TYPE_CONFIG, ACTIVITY_TYPE_FALLBACK } from './activityTypeConfig'

interface ActivityTypeChipProps {
  activityType: ActivityType | string | null | undefined
  size?: 'sm' | 'default'
}

/**
 * ActivityTypeChip — renders an investment activity type as an accessible icon + label + color chip.
 *
 * Color is never used alone — every variant pairs a badge color with an icon and text
 * for WCAG AA compliance.
 *
 * Falls back to the 'Other' config for unknown/null/undefined activity type values.
 */
export function ActivityTypeChip({ activityType, size = 'sm' }: ActivityTypeChipProps) {
  const config = (activityType != null && activityType in ACTIVITY_TYPE_CONFIG)
    ? ACTIVITY_TYPE_CONFIG[activityType as ActivityType]
    : ACTIVITY_TYPE_FALLBACK
  const { label, variant, Icon } = config

  return (
    <Badge variant={variant} size={size}>
      <Icon className="size-3" aria-hidden="true" />
      {label}
    </Badge>
  )
}
