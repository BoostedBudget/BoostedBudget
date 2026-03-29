/**
 * useRuleTierLimits Hook
 * Issue #39: Rule Builder UI
 *
 * React hook for calculating automation rule tier limits and displaying rule usage.
 * All tiers now have unlimited rules with event triggers available.
 */

export interface RuleTierLimitsResult {
  ruleLimit: number
  isUnlimited: boolean
  currentRuleCount: number
  isAtLimit: boolean
  canUseEventTriggers: boolean
  shouldShowUpgradeBanner: boolean
  rulesCountDisplay: string
}

export interface UseRuleTierLimitsParams {
  currentRuleCount: number
}

/**
 * Hook for calculating rule tier limits and usage display
 *
 * All tiers now have unlimited rules, so this hook returns simplified values.
 *
 * @param currentRuleCount - Number of automation rules user currently has
 * @returns Object containing limit calculations and display strings
 *
 * @example
 * ```tsx
 * function RulesPage() {
 *   const { rules } = useGetAutomationRules()
 *   const {
 *     rulesCountDisplay,
 *   } = useRuleTierLimits({
 *     currentRuleCount: rules?.length ?? 0,
 *   })
 *
 *   return (
 *     <div>
 *       <span>{rulesCountDisplay}</span>
 *     </div>
 *   )
 * }
 * ```
 */
export function useRuleTierLimits({
  currentRuleCount,
}: UseRuleTierLimitsParams): RuleTierLimitsResult {
  // All tiers now have unlimited rules
  const rulesCountDisplay = `${currentRuleCount} ${currentRuleCount === 1 ? 'rule' : 'rules'}`

  return {
    ruleLimit: -1,
    isUnlimited: true,
    currentRuleCount,
    isAtLimit: false,
    canUseEventTriggers: true,
    shouldShowUpgradeBanner: false,
    rulesCountDisplay,
  }
}
