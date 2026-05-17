import { describe, it, expect } from 'bun:test'
import { classifyTierChange } from './tierDirection'
import type { SubscriptionTier } from '../types'
import type { TierName } from '../types'

function makeTier(priceMonthly: number): SubscriptionTier {
  return {
    name: 'premium',
    display_name: 'Premium',
    description: '',
    limits: { accounts: 5, transaction_history_days: 365, executions_per_day: 50 },
    features: {
      ads_enabled: false,
      ai_insights: false,
      api_access: false,
      priority_support: false,
      custom_categories: true,
      custom_tags: true,
      advanced_reports: false,
    },
    pricing: {
      price_monthly: priceMonthly,
      price_yearly: priceMonthly * 10,
      mobile_price_yearly: priceMonthly * 10,
      users_included: 1,
    },
  }
}

const tiers: Record<TierName, SubscriptionTier> = {
  premium: makeTier(8.99),
  household_2: makeTier(12.99),
  household_3: makeTier(14.99),
  household_4: makeTier(16.99),
  household_5: makeTier(18.99),
}

describe('classifyTierChange', () => {
  it('returns same when tier and interval are identical', () => {
    expect(classifyTierChange('premium', 'month', 'premium', 'month', tiers)).toBe('same')
  })

  it('returns interval-switch when tier is the same but interval differs (month→year)', () => {
    expect(classifyTierChange('premium', 'month', 'premium', 'year', tiers)).toBe('interval-switch')
  })

  it('returns interval-switch when tier is the same but interval differs (year→month)', () => {
    expect(classifyTierChange('household_3', 'year', 'household_3', 'month', tiers)).toBe('interval-switch')
  })

  it('returns upgrade when moving to a more expensive tier', () => {
    expect(classifyTierChange('premium', 'month', 'household_2', 'month', tiers)).toBe('upgrade')
    expect(classifyTierChange('premium', 'month', 'household_5', 'month', tiers)).toBe('upgrade')
    expect(classifyTierChange('household_2', 'year', 'household_4', 'year', tiers)).toBe('upgrade')
  })

  it('returns downgrade when moving to a less expensive tier', () => {
    expect(classifyTierChange('household_5', 'month', 'premium', 'month', tiers)).toBe('downgrade')
    expect(classifyTierChange('household_3', 'year', 'premium', 'year', tiers)).toBe('downgrade')
  })

  it('classifies cross-interval tier changes by price only (upgrade)', () => {
    expect(classifyTierChange('premium', 'month', 'household_3', 'year', tiers)).toBe('upgrade')
  })

  it('classifies cross-interval tier changes by price only (downgrade)', () => {
    expect(classifyTierChange('household_5', 'year', 'premium', 'month', tiers)).toBe('downgrade')
  })
})
