/**
 * useTierLimits Hook
 *
 * React hook for calculating subscription tier limits and displaying account usage.
 * Extracted from AccountsPage for reusability and cleaner code organization.
 *
 * TDD REFACTOR: Extracted during account limit enforcement implementation.
 */

import type { User } from '@/features/profile/types'
import type { SubscriptionTiersResponse } from '../types'

export interface TierLimitsResult {
  accountLimit: number | null
  isUnlimited: boolean
  isAtLimit: boolean
  shouldShowUpgradeBanner: boolean
  accountCountDisplay: string
}

export interface UseTierLimitsParams {
  user: User | null
  tiers: SubscriptionTiersResponse | null
  currentAccountCount: number
}

/**
 * Hook for calculating tier limits and account usage display
 *
 * @param user - Current user object with subscription tier
 * @param tiers - Subscription tiers configuration from API
 * @param currentAccountCount - Number of accounts user currently has
 * @returns Object containing limit calculations and display strings
 *
 * @example
 * ```tsx
 * function AccountsPage() {
 *   const { user } = useProfile()
 *   const { tiers } = useSubscriptionTiers()
 *   const { isAtLimit, accountCountDisplay } = useTierLimits({
 *     user,
 *     tiers,
 *     currentAccountCount: accounts.length,
 *   })
 *
 *   return <div>{accountCountDisplay}</div>
 * }
 * ```
 */
export function useTierLimits({ user, tiers, currentAccountCount }: UseTierLimitsParams): TierLimitsResult {
  // Calculate account limit based on user tier
  const accountLimit = user && tiers
    ? tiers.tiers[user.subscription_tier]?.limits.accounts
    : null

  // Check if limit is unlimited (-1)
  const isUnlimited = accountLimit === -1

  // Check if user is at account limit
  const isAtLimit = !isUnlimited && accountLimit !== null && currentAccountCount >= accountLimit

  // Determine if upgrade banner should be shown
  const shouldShowUpgradeBanner = isAtLimit && user?.subscription_tier !== 'premium_plus'

  // Format account count display based on tier limits
  const accountCountDisplay = isUnlimited
    ? `${currentAccountCount} ${currentAccountCount === 1 ? 'account' : 'accounts'}`
    : `${currentAccountCount}/${accountLimit} ${currentAccountCount === 1 ? 'account' : 'accounts'} used`

  return {
    accountLimit,
    isUnlimited,
    isAtLimit,
    shouldShowUpgradeBanner,
    accountCountDisplay,
  }
}
