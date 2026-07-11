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

// Members roster + composing tab (Task 4b)
export { MembersList, type MembersListProps } from './components/MembersList'
export { HouseholdTab, type HouseholdTabProps } from './HouseholdTab'

// Invite-accept standalone page (Task 6)
export { AcceptInvitePage } from './components/AcceptInvitePage'
export { ConsentPreview, type ConsentPreviewProps } from './components/ConsentPreview'

// Hooks (Task 3: ProfilePage integration)
export { useHousehold, type UseHouseholdResult } from './hooks/useHousehold'
export { useAcceptInvite, type UseAcceptInviteResult, type AcceptInviteErrorKind } from './hooks/useAcceptInvite'

// Hooks (Task 8: AccountCustomizationModal integration)
export {
  useHouseholdVisibility,
  type UseHouseholdVisibilityResult,
  type ShareVisibility,
} from './hooks/useHouseholdVisibility'

// Types
export type {
  Household,
  HouseholdMember,
  HouseholdMemberView,
  HouseholdInvite,
  HouseholdEntitlementSummary,
  HouseholdAccountShare,
} from './types'
