/**
 * Household Badge Variants
 *
 * Issue #1370: Household feature frontend scaffold
 * CVA for household-specific badge states and roles.
 * Variants: owner, member (roles) and available, full, over-limit (seat usage states).
 */

import { cva, type VariantProps } from 'class-variance-authority'

export const householdBadgeVariants = cva(
  'inline-flex items-center justify-center rounded-full border px-2 py-0.5 text-xs font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
  {
    variants: {
      variant: {
        // Role badges
        owner:      'border-transparent bg-primary text-primary-foreground',
        member:     'border-transparent bg-secondary text-secondary-foreground',
        // Seat usage state badges
        available:  'border-transparent bg-success text-success-foreground',
        full:       'border-transparent bg-warning text-warning-foreground',
        'over-limit': 'border-transparent bg-destructive text-destructive-foreground',
      },
      size: {
        default: 'px-2 py-0.5 text-xs',
        sm:      'px-1.5 py-0 text-[10px]',
        lg:      'px-2.5 py-1 text-sm',
      },
    },
    defaultVariants: {
      variant: 'member',
      size: 'default',
    },
  }
)

export type HouseholdBadgeVariants = VariantProps<typeof householdBadgeVariants>
