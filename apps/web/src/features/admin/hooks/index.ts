/**
 * Admin Hooks
 * Issue #642: Admin feature foundation
 */

export { useIsAdmin } from './useIsAdmin'
export type { UseIsAdminResult } from './useIsAdmin'

export { useAdminMode } from './useAdminMode'
export type { UseAdminModeResult } from './useAdminMode'

export { useEnvironment } from './useEnvironment'
export type { UseEnvironmentResult, Environment } from './useEnvironment'

export { useUserFilters } from './useUserFilters'
export type { UseUserFiltersResult, UserFilters } from './useUserFilters'

export { useAdminSubscriptionDetail, useUpdateUserTier } from './useAdminSubscription'
export type { AdminSubscriptionDetailOutputBody } from './useAdminSubscription'
