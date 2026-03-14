/**
 * useTierLimits Hook Tests - Issue #564/575
 * Tests subscription tier limits calculation hook
 */

import { describe, it, expect } from 'bun:test'
import { useTierLimits } from './useTierLimits'
import type { User } from '@/features/profile/types'
import type { SubscriptionTiersResponse } from '../types'

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

function createMockTiers(): SubscriptionTiersResponse {
  const createFeatures = (overrides: Record<string, boolean> = {}) => ({
    ads_enabled: false,
    api_access: false,
    priority_support: false,
    advanced_reports: false,
    ai_insights: false,
    custom_categories: false,
    custom_tags: false,
    ...overrides,
  })

  const createLimits = (accounts: number, rules: number) => ({
    accounts,
    rules,
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
        features: createFeatures({ ads_enabled: true }),
        pricing: { monthly_price: 0, annual_price: 0, currency: 'USD' },
      },
      premium_lite: {
        name: 'Premium Lite',
        display_name: 'Premium Lite',
        description: 'More features',
        limits: createLimits(15, 5),
        features: createFeatures({ custom_categories: true, custom_tags: true }),
        pricing: { monthly_price: 4.99, annual_price: 49.99, currency: 'USD' },
      },
      premium: {
        name: 'Premium',
        display_name: 'Premium',
        description: 'Premium features',
        limits: createLimits(30, 15),
        features: createFeatures({ priority_support: true, advanced_reports: true, custom_categories: true, custom_tags: true }),
        pricing: { monthly_price: 9.99, annual_price: 99.99, currency: 'USD' },
      },
      premium_plus: {
        name: 'Premium+',
        display_name: 'Premium+',
        description: 'All features',
        limits: createLimits(-1, -1),
        features: createFeatures({ api_access: true, priority_support: true, advanced_reports: true, ai_insights: true, custom_categories: true, custom_tags: true }),
        pricing: { monthly_price: 19.99, annual_price: 199.99, currency: 'USD' },
      },
    },
  } as unknown as SubscriptionTiersResponse
}

describe('useTierLimits', () => {
  describe('accountLimit', () => {
    it('should return account limit for free tier', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentAccountCount: 0,
      })

      expect(result.accountLimit).toBe(3)
    })

    it('should return account limit for premium_lite tier', () => {
      const result = useTierLimits({
        user: createMockUser('premium_lite'),
        tiers: createMockTiers(),
        currentAccountCount: 0,
      })

      expect(result.accountLimit).toBe(15)
    })

    it('should return account limit for premium tier', () => {
      const result = useTierLimits({
        user: createMockUser('premium'),
        tiers: createMockTiers(),
        currentAccountCount: 0,
      })

      expect(result.accountLimit).toBe(30)
    })

    it('should return -1 (unlimited) for premium_plus tier', () => {
      const result = useTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentAccountCount: 0,
      })

      expect(result.accountLimit).toBe(-1)
    })

    it('should return null when user is null', () => {
      const result = useTierLimits({
        user: null,
        tiers: createMockTiers(),
        currentAccountCount: 0,
      })

      expect(result.accountLimit).toBeNull()
    })

    it('should return null when tiers is null', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: null,
        currentAccountCount: 0,
      })

      expect(result.accountLimit).toBeNull()
    })
  })

  describe('isUnlimited', () => {
    it('should return true for unlimited tier (-1 limit)', () => {
      const result = useTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentAccountCount: 100,
      })

      expect(result.isUnlimited).toBe(true)
    })

    it('should return false for limited tier', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentAccountCount: 0,
      })

      expect(result.isUnlimited).toBe(false)
    })

    it('should return false when accountLimit is null', () => {
      const result = useTierLimits({
        user: null,
        tiers: null,
        currentAccountCount: 0,
      })

      expect(result.isUnlimited).toBe(false)
    })
  })

  describe('isAtLimit', () => {
    it('should return true when at account limit', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentAccountCount: 3,
      })

      expect(result.isAtLimit).toBe(true)
    })

    it('should return true when over account limit', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentAccountCount: 5,
      })

      expect(result.isAtLimit).toBe(true)
    })

    it('should return false when below account limit', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentAccountCount: 2,
      })

      expect(result.isAtLimit).toBe(false)
    })

    it('should return false for unlimited tier', () => {
      const result = useTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentAccountCount: 1000,
      })

      expect(result.isAtLimit).toBe(false)
    })

    it('should return false when accountLimit is null', () => {
      const result = useTierLimits({
        user: null,
        tiers: null,
        currentAccountCount: 100,
      })

      expect(result.isAtLimit).toBe(false)
    })
  })

  describe('shouldShowUpgradeBanner', () => {
    it('should return true when at limit and not premium_plus', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentAccountCount: 3,
      })

      expect(result.shouldShowUpgradeBanner).toBe(true)
    })

    it('should return true when premium user is at limit', () => {
      const result = useTierLimits({
        user: createMockUser('premium'),
        tiers: createMockTiers(),
        currentAccountCount: 30,
      })

      expect(result.shouldShowUpgradeBanner).toBe(true)
    })

    it('should return false for premium_plus (unlimited)', () => {
      const result = useTierLimits({
        user: createMockUser('premium_plus'),
        tiers: createMockTiers(),
        currentAccountCount: 1000,
      })

      expect(result.shouldShowUpgradeBanner).toBe(false)
    })

    it('should return false when not at limit', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: createMockTiers(),
        currentAccountCount: 1,
      })

      expect(result.shouldShowUpgradeBanner).toBe(false)
    })

    it('should return false when user is null', () => {
      const result = useTierLimits({
        user: null,
        tiers: createMockTiers(),
        currentAccountCount: 10,
      })

      expect(result.shouldShowUpgradeBanner).toBe(false)
    })
  })

  describe('accountCountDisplay', () => {
    describe('limited tiers', () => {
      it('should format with singular account when count is 1', () => {
        const result = useTierLimits({
          user: createMockUser('free'),
          tiers: createMockTiers(),
          currentAccountCount: 1,
        })

        expect(result.accountCountDisplay).toBe('1/3 account used')
      })

      it('should format with plural accounts when count > 1', () => {
        const result = useTierLimits({
          user: createMockUser('free'),
          tiers: createMockTiers(),
          currentAccountCount: 2,
        })

        expect(result.accountCountDisplay).toBe('2/3 accounts used')
      })

      it('should format correctly at limit', () => {
        const result = useTierLimits({
          user: createMockUser('free'),
          tiers: createMockTiers(),
          currentAccountCount: 3,
        })

        expect(result.accountCountDisplay).toBe('3/3 accounts used')
      })

      it('should format with 0 accounts', () => {
        const result = useTierLimits({
          user: createMockUser('free'),
          tiers: createMockTiers(),
          currentAccountCount: 0,
        })

        expect(result.accountCountDisplay).toBe('0/3 accounts used')
      })
    })

    describe('unlimited tier', () => {
      it('should format with singular account when count is 1', () => {
        const result = useTierLimits({
          user: createMockUser('premium_plus'),
          tiers: createMockTiers(),
          currentAccountCount: 1,
        })

        expect(result.accountCountDisplay).toBe('1 account')
      })

      it('should format with plural accounts when count > 1', () => {
        const result = useTierLimits({
          user: createMockUser('premium_plus'),
          tiers: createMockTiers(),
          currentAccountCount: 50,
        })

        expect(result.accountCountDisplay).toBe('50 accounts')
      })

      it('should format with 0 accounts', () => {
        const result = useTierLimits({
          user: createMockUser('premium_plus'),
          tiers: createMockTiers(),
          currentAccountCount: 0,
        })

        expect(result.accountCountDisplay).toBe('0 accounts')
      })
    })

    describe('null/undefined handling', () => {
      it('should handle null accountLimit gracefully', () => {
        const result = useTierLimits({
          user: null,
          tiers: null,
          currentAccountCount: 5,
        })

        expect(result.accountCountDisplay).toBe('5/null accounts used')
      })
    })
  })

  describe('effective_tier override', () => {
    it('should use effective_tier when set, overriding subscription_tier for limit lookup', () => {
      const user: User = {
        ...createMockUser('premium'),
        effective_tier: 'premium_plus', // admin override to unlimited tier
      }

      const result = useTierLimits({
        user,
        tiers: createMockTiers(),
        currentAccountCount: 50,
      })

      expect(result.accountLimit).toBe(-1) // premium_plus is unlimited
      expect(result.isUnlimited).toBe(true)
    })

    it('should fall back to subscription_tier when effective_tier is undefined', () => {
      const user: User = createMockUser('premium') // no effective_tier

      const result = useTierLimits({
        user,
        tiers: createMockTiers(),
        currentAccountCount: 5,
      })

      expect(result.accountLimit).toBe(30) // premium limit
    })
  })

  describe('edge cases', () => {
    it('should handle unknown tier name', () => {
      const result = useTierLimits({
        user: createMockUser('unknown_tier'),
        tiers: createMockTiers(),
        currentAccountCount: 5,
      })

      expect(result.accountLimit).toBeUndefined()
    })

    it('should handle empty tiers object', () => {
      const result = useTierLimits({
        user: createMockUser('free'),
        tiers: { default_tier: 'free', tiers: {} } as SubscriptionTiersResponse,
        currentAccountCount: 5,
      })

      expect(result.accountLimit).toBeUndefined()
    })
  })
})
