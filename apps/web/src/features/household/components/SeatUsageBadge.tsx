/**
 * SeatUsageBadge Component
 *
 * Issue #1370: Household feature frontend scaffold
 * Badge component for displaying household seat usage.
 * Shows used/total seats and applies state variant based on usage level.
 */

import React from 'react'
import { cn } from '@/lib/utils'
import {
  householdBadgeVariants,
  type HouseholdBadgeVariants,
} from './householdBadgeVariants'

export interface SeatUsageBadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    Omit<HouseholdBadgeVariants, 'variant'> {
  /** Number of seats currently used */
  used: number
  /** Total number of seats available */
  total: number
}

/**
 * Determines the seat usage state based on used and total counts.
 *
 * @param used - Current seat usage count
 * @param total - Total available seats
 * @returns State variant: 'available' | 'full' | 'over-limit'
 */
function determineSeatState(
  used: number,
  total: number
): 'available' | 'full' | 'over-limit' {
  if (used > total) {
    return 'over-limit'
  }
  if (used === total) {
    return 'full'
  }
  return 'available'
}

/**
 * SeatUsageBadge
 * Displays a badge showing household seat usage (e.g., "2/3").
 * State variant changes based on usage: available (has free seats),
 * full (all seats used), or over-limit (more used than total).
 *
 * @param used - Number of seats currently used
 * @param total - Total number of seats available
 * @param size - Badge size variant (default, sm, lg)
 * @param className - Additional CSS classes to merge
 */
export function SeatUsageBadge({
  used,
  total,
  size = 'default',
  className,
  ...props
}: SeatUsageBadgeProps) {
  const variant = determineSeatState(used, total)
  const label = `${used}/${total}`

  return (
    <span
      data-slot="seat-usage-badge"
      className={cn(householdBadgeVariants({ variant, size }), className)}
      {...props}
    >
      {label}
    </span>
  )
}
