/**
 * RoleBadge Component
 *
 * Issue #1370: Household feature frontend scaffold
 * Badge component for displaying household member roles.
 * Renders owner or member role labels with appropriate styling.
 */

import React from 'react'
import { cn } from '@/lib/utils'
import {
  householdBadgeVariants,
  type HouseholdBadgeVariants,
} from './householdBadgeVariants'

export interface RoleBadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    Omit<HouseholdBadgeVariants, 'variant'> {
  /** Household member role: owner or member */
  role: 'owner' | 'member'
}

/**
 * RoleBadge
 * Displays a badge for household member roles (owner or member).
 *
 * @param role - 'owner' or 'member'
 * @param size - Badge size variant (default, sm, lg)
 * @param className - Additional CSS classes to merge
 */
export function RoleBadge({
  role,
  size = 'default',
  className,
  ...props
}: RoleBadgeProps) {
  const label = role === 'owner' ? 'Owner' : 'Member'

  return (
    <span
      data-slot="role-badge"
      className={cn(householdBadgeVariants({ variant: role, size }), className)}
      {...props}
    >
      {label}
    </span>
  )
}
