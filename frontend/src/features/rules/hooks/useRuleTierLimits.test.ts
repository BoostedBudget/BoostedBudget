/**
 * useRuleTierLimits Hook Tests
 * Issue #39: Rule Builder UI
 *
 * Tests the automation rule tier limits calculation hook.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect } from 'vitest'
import { useRuleTierLimits } from './useRuleTierLimits'
import type { User } from '@/features/profile/types'
import type { SubscriptionTiersResponse } from '@/features/subscription/types'

// Mock user factory
function createMockUser(tier: string = 'free'): User {
  return {
    id: 'user-1',
    clerk_user_id: 'user_123',
    email: 'test@example.com',
    email_verified: true,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    subscription_tier: tier,
    subscription_status: 'active',
    primary_region: 'iad',
    is_active: true,
    is_deleted: false,
  }
}

// Mock tiers factory - creates properly typed mock data
function createMockTiers(): SubscriptionTiersResponse {
  const createFeatures = (overrides: Record<string, boolean> = {}) => ({
    ads_enabled: false,
    api_access: false,
    priority_support: false,
    advanced_reports: false,
    ai_insights: false,
    custom_categories: false,
    custom_tags: false,
    event_triggers: false,
    ...overrides,
  })

  const createLimits = (accounts: number, rules: number) => ({
    accounts,
    rules,
    accounts_synced_per_month: 100,
    api_calls_per_month: 1000,
    custom_categories_max: 10,
    custom_tags_max: 20,
    data_export_requests_per_month: 5,
    executions_per_day: 10,
    min_execution_interval_minutes: 60,
    transaction_history_days: 365,
  })

  return {
    default_tier: 'free',
    tiers: {
      free: {
        name: 'Free',
        display_name: 'Free',
        description: 'Basic features',
        limits: createLimits(3, 1),
        features: createFeatures({ ads_enabled: true, event_triggers: false }),
        pricing: { monthly_price: 0, annual_price: 0, currency: 'USD' },
      },
      premium_lite: {
        name: 'Premium Lite',
        display_name: 'Premium Lite',
        description: 'More features',
        limits: createLimits(15, 5),
        features: createFeatures({ custom_categories: true, custom_tags: true, event_triggers: true }),
        pricing: { monthly_price: 4.99, annual_price: 49.99, currency: 'USD' },
      },
      premium: {
        name: 'Premium',
        display_name: 'Premium',
        description: 'Premium features',
        limits: createLimits(30, 15),
        features: createFeatures({ priority_support: true, advanced_reports: true, custom_categories: true, custom_tags: true, event_triggers: true }),
        pricing: { monthly_price: 9.99, annual_price: 99.99, currency: 'USD' },
      },
      premium_plus: {
        name: 'Premium+',
        display_name: 'Premium+',
        description: 'All features',
        limits: createLimits(-1, -1),
        features: createFeatures({ api_access: true, priority_support: true, advanced_reports: true, ai_insights: true, custom_categories: true, custom_tags: true, event_triggers: true }),
        pricing: { monthly_price: 19.99, annual_price: 199.99, currency: 'USD' },
      },
    },
  } as unknown as SubscriptionTiersResponse
}

describe('useRuleTierLimits', () => {
  describe('ruleLimit', () => {
    it('should return rule limit for free tier (1 rule)', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.ruleLimit).toBe(1)
    })

    it('should return rule limit for premium_lite tier (5 rules)', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_lite'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.ruleLimit).toBe(5)
    })

    it('should return rule limit for premium tier (15 rules)', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.ruleLimit).toBe(15)
    })

    it('should return -1 (unlimited) for premium_plus tier', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.ruleLimit).toBe(-1)
    })

    it('should return null when user is null', () => {
      const result = useRuleTierLimits({
        user: null,
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.ruleLimit).toBeNull()
    })

    it('should return null when tiers is null', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: null,
        currentRuleCount: 0,
      })

      expect(result.ruleLimit).toBeNull()
    })
  })

  describe('isUnlimited', () => {
    it('should return true for unlimited tier (-1 limit)', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentRuleCount: 100,
      })

      expect(result.isUnlimited).toBe(true)
    })

    it('should return false for limited tier', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.isUnlimited).toBe(false)
    })

    it('should return false when ruleLimit is null', () => {
      const result = useRuleTierLimits({
        user: null,
        tiers: null,
        currentRuleCount: 0,
      })

      expect(result.isUnlimited).toBe(false)
    })
  })

  describe('isAtLimit', () => {
    it('should return true when at rule limit', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentRuleCount: 1,
      })

      expect(result.isAtLimit).toBe(true)
    })

    it('should return true when over rule limit', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentRuleCount: 3,
      })

      expect(result.isAtLimit).toBe(true)
    })

    it('should return false when below rule limit', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_lite'),
        tiers: createMockTiers(),
        currentRuleCount: 2,
      })

      expect(result.isAtLimit).toBe(false)
    })

    it('should return false for unlimited tier', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentRuleCount: 1000,
      })

      expect(result.isAtLimit).toBe(false)
    })

    it('should return false when ruleLimit is null', () => {
      const result = useRuleTierLimits({
        user: null,
        tiers: null,
        currentRuleCount: 100,
      })

      expect(result.isAtLimit).toBe(false)
    })
  })

  describe('canUseEventTriggers', () => {
    it('should return false for free tier', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.canUseEventTriggers).toBe(false)
    })

    it('should return true for premium_lite tier', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_lite'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.canUseEventTriggers).toBe(true)
    })

    it('should return true for premium tier', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.canUseEventTriggers).toBe(true)
    })

    it('should return true for premium_plus tier', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.canUseEventTriggers).toBe(true)
    })

    it('should return false when user is null', () => {
      const result = useRuleTierLimits({
        user: null,
        tiers: createMockTiers(),
        currentRuleCount: 0,
      })

      expect(result.canUseEventTriggers).toBe(false)
    })

    it('should return false when tiers is null', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium'),
        tiers: null,
        currentRuleCount: 0,
      })

      expect(result.canUseEventTriggers).toBe(false)
    })
  })

  describe('shouldShowUpgradeBanner', () => {
    it('should return true when at limit and not premium_plus', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentRuleCount: 1,
      })

      expect(result.shouldShowUpgradeBanner).toBe(true)
    })

    it('should return true when premium user is at limit', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium'),
        tiers: createMockTiers(),
        currentRuleCount: 15,
      })

      expect(result.shouldShowUpgradeBanner).toBe(true)
    })

    it('should return false for premium_plus (unlimited)', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentRuleCount: 1000,
      })

      expect(result.shouldShowUpgradeBanner).toBe(false)
    })

    it('should return false when not at limit', () => {
      const result = useRuleTierLimits({
        user: createMockUser('premium_lite'),
        tiers: createMockTiers(),
        currentRuleCount: 2,
      })

      expect(result.shouldShowUpgradeBanner).toBe(false)
    })

    it('should return false when user is null', () => {
      const result = useRuleTierLimits({
        user: null,
        tiers: createMockTiers(),
        currentRuleCount: 10,
      })

      expect(result.shouldShowUpgradeBanner).toBe(false)
    })
  })

  describe('rulesCountDisplay', () => {
    describe('limited tiers', () => {
      it('should format with singular rule when count is 1', () => {
        const result = useRuleTierLimits({
          user: createMockUser('free'),
          tiers: createMockTiers(),
          currentRuleCount: 1,
        })

        expect(result.rulesCountDisplay).toBe('1/1 rule used')
      })

      it('should format with plural rules when count > 1', () => {
        const result = useRuleTierLimits({
          user: createMockUser('premium_lite'),
          tiers: createMockTiers(),
          currentRuleCount: 3,
        })

        expect(result.rulesCountDisplay).toBe('3/5 rules used')
      })

      it('should format correctly at limit', () => {
        const result = useRuleTierLimits({
          user: createMockUser('premium'),
          tiers: createMockTiers(),
          currentRuleCount: 15,
        })

        expect(result.rulesCountDisplay).toBe('15/15 rules used')
      })

      it('should format with 0 rules', () => {
        const result = useRuleTierLimits({
          user: createMockUser('free'),
          tiers: createMockTiers(),
          currentRuleCount: 0,
        })

        expect(result.rulesCountDisplay).toBe('0/1 rules used')
      })
    })

    describe('unlimited tier', () => {
      it('should format with singular rule when count is 1', () => {
        const result = useRuleTierLimits({
          user: createMockUser('premium_plus'),
          tiers: createMockTiers(),
          currentRuleCount: 1,
        })

        expect(result.rulesCountDisplay).toBe('1 rule')
      })

      it('should format with plural rules when count > 1', () => {
        const result = useRuleTierLimits({
          user: createMockUser('premium_plus'),
          tiers: createMockTiers(),
          currentRuleCount: 50,
        })

        expect(result.rulesCountDisplay).toBe('50 rules')
      })

      it('should format with 0 rules', () => {
        const result = useRuleTierLimits({
          user: createMockUser('premium_plus'),
          tiers: createMockTiers(),
          currentRuleCount: 0,
        })

        expect(result.rulesCountDisplay).toBe('0 rules')
      })
    })

    describe('null/undefined handling', () => {
      it('should handle null ruleLimit gracefully', () => {
        const result = useRuleTierLimits({
          user: null,
          tiers: null,
          currentRuleCount: 5,
        })

        // When ruleLimit is null, isUnlimited is false, so it uses the limited format
        expect(result.rulesCountDisplay).toBe('5/null rules used')
      })
    })
  })

  describe('currentRuleCount', () => {
    it('should return the current rule count passed in', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentRuleCount: 5,
      })

      expect(result.currentRuleCount).toBe(5)
    })
  })

  describe('edge cases', () => {
    it('should handle unknown tier name', () => {
      const result = useRuleTierLimits({
        user: createMockUser('unknown_tier'),
        tiers: createMockTiers(),
        currentRuleCount: 5,
      })

      // Unknown tier should result in undefined limit
      expect(result.ruleLimit).toBeUndefined()
    })

    it('should handle empty tiers object', () => {
      const result = useRuleTierLimits({
        user: createMockUser('free'),
        tiers: { default_tier: 'free', tiers: {} } as SubscriptionTiersResponse,
        currentRuleCount: 5,
      })

      expect(result.ruleLimit).toBeUndefined()
    })
  })
})
