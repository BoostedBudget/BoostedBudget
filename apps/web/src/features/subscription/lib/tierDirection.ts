import type { SubscriptionTier } from '../types'
import type { TierName } from '../types'

export type TierChangeDirection = 'upgrade' | 'downgrade' | 'interval-switch' | 'same'

export function classifyTierChange(
  currentTier: TierName,
  currentInterval: 'month' | 'year',
  targetTier: TierName,
  targetInterval: 'month' | 'year',
  tiers: Record<TierName, SubscriptionTier>,
): TierChangeDirection {
  if (currentTier === targetTier) {
    return currentInterval === targetInterval ? 'same' : 'interval-switch'
  }

  const currentPrice = tiers[currentTier]?.pricing.price_monthly ?? 0
  const targetPrice = tiers[targetTier]?.pricing.price_monthly ?? 0

  if (targetPrice > currentPrice) return 'upgrade'
  if (targetPrice < currentPrice) return 'downgrade'
  return 'same'
}
