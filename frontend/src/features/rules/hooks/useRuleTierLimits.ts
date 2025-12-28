/**
 * useRuleTierLimits Hook
 * Issue #39: Rule Builder UI
 *
 * React hook for calculating automation rule tier limits and displaying rule usage.
 * Based on useTierLimits pattern but specific to automation rules.
 *
 * Key differences from account limits:
 * - Free tier: 1 rule, schedule triggers only
 * - Premium tiers: More rules, event triggers allowed
 */

import type { User } from '@/features/profile/types'
import type { SubscriptionTiersResponse } from '@/features/subscription/types'

export interface RuleTierLimitsResult {
  ruleLimit: number | null
  isUnlimited: boolean
  currentRuleCount: number
  isAtLimit: boolean
  canUseEventTriggers: boolean
  shouldShowUpgradeBanner: boolean
  rulesCountDisplay: string
}

export interface UseRuleTierLimitsParams {
  user: User | null
  tiers: SubscriptionTiersResponse | null
  currentRuleCount: number
}

/**
 * Hook for calculating rule tier limits and usage display
 *
 * @param user - Current user object with subscription tier
 * @param tiers - Subscription tiers configuration from API
 * @param currentRuleCount - Number of automation rules user currently has
 * @returns Object containing limit calculations and display strings
 *
 * @example
 * ```tsx
 * function RulesPage() {
 *   const { user } = useProfile()
 *   const { tiers } = useSubscriptionTiers()
 *   const { rules } = useGetAutomationRules()
 *   const {
 *     isAtLimit,
 *     canUseEventTriggers,
 *     rulesCountDisplay,
 *   } = useRuleTierLimits({
 *     user,
 *     tiers,
 *     currentRuleCount: rules?.length ?? 0,
 *   })
 *
 *   return (
 *     <div>
 *       <span>{rulesCountDisplay}</span>
 *       {isAtLimit && <UpgradeBanner />}
 *     </div>
 *   )
 * }
 * ```
 */
export function useRuleTierLimits({
  user,
  tiers,
  currentRuleCount,
}: UseRuleTierLimitsParams): RuleTierLimitsResult {
  // Calculate rule limit based on user tier
  const ruleLimit = user && tiers
    ? tiers.tiers[user.subscription_tier]?.limits.rules
    : null

  // Check if limit is unlimited (-1)
  const isUnlimited = ruleLimit === -1

  // Check if user is at rule limit
  const isAtLimit = !isUnlimited && ruleLimit !== null && currentRuleCount >= ruleLimit

  // Check if user can use event triggers (Free tier cannot)
  // Free tier only has access to scheduled triggers
  // Premium tiers (premium_lite, premium, premium_plus) can use event triggers
  // Require both user and tiers to be present for a conservative approach
  const canUseEventTriggers = user && tiers
    ? user.subscription_tier !== 'free'
    : false

  // Determine if upgrade banner should be shown
  const shouldShowUpgradeBanner = isAtLimit && user?.subscription_tier !== 'premium_plus'

  // Format rule count display based on tier limits
  const rulesCountDisplay = isUnlimited
    ? `${currentRuleCount} ${currentRuleCount === 1 ? 'rule' : 'rules'}`
    : `${currentRuleCount}/${ruleLimit} ${currentRuleCount === 1 ? 'rule' : 'rules'} used`

  return {
    ruleLimit,
    isUnlimited,
    currentRuleCount,
    isAtLimit,
    canUseEventTriggers,
    shouldShowUpgradeBanner,
    rulesCountDisplay,
  }
}
