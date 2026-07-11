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

// Invite panel
export { InvitePanel } from './components/InvitePanel'

// Household management components (Task 4a)
export { CreateHouseholdForm, type CreateHouseholdFormProps } from './components/CreateHouseholdForm'
export { HouseholdCard, type HouseholdCardProps } from './components/HouseholdCard'
export { LeaveHouseholdDialog, type LeaveHouseholdDialogProps } from './components/LeaveHouseholdDialog'
export { DisbandHouseholdDialog, type DisbandHouseholdDialogProps } from './components/DisbandHouseholdDialog'

// Types
export type {
  Household,
  HouseholdMember,
  HouseholdMemberView,
  HouseholdInvite,
  HouseholdEntitlementSummary,
  HouseholdAccountShare,
} from './types'
