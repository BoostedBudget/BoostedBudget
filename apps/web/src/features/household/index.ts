/**
 * Household Feature Module
 *
 * Issue #1370: Household feature frontend scaffold
 * Public API for household feature components and types.
 */

// Badge components
export { RoleBadge, type RoleBadgeProps } from './components/RoleBadge'
export { SeatUsageBadge, type SeatUsageBadgeProps } from './components/SeatUsageBadge'

// Badge variants
export { householdBadgeVariants, type HouseholdBadgeVariants } from './components/householdBadgeVariants'

// Types
export type {
  Household,
  HouseholdMember,
  HouseholdMemberView,
  HouseholdInvite,
  HouseholdEntitlementSummary,
  HouseholdAccountShare,
} from './types'
