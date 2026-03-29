/**
 * Subscription Test Fixtures
 *
 * Shared mock data for subscription-related tests.
 * These fixtures match the generated types from the OpenAPI spec.
 */

import type {
  SubscriptionTiersResponse,
  SubscriptionTier,
  SubscriptionLimits,
  SubscriptionFeatures,
  SubscriptionPricing,
} from '@boostedbudget/api-client/models'

/**
 * Complete mock subscription tiers response matching the generated types.
 * All tiers have unlimited accounts and executions_per_day (-1).
 * The only differentiator between tiers is users_included.
 */
export const mockSubscriptionTiersResponse: SubscriptionTiersResponse = {
  tiers: {
    premium: {
      name: 'premium',
      display_name: 'Premium',
      description: 'Full-featured budgeting for individuals',
      pricing: {
        price_monthly: 8.99,
        price_yearly: 79.00,
        mobile_price_yearly: 89.99,
        users_included: 1,
      },
      limits: {
        accounts: -1,
        executions_per_day: -1,
        transaction_history_days: 730,
      },
      features: {
        ads_enabled: false,
        ai_insights: true,
        api_access: true,
        priority_support: true,
        custom_categories: true,
        custom_tags: true,
        advanced_reports: true,
      },
    },
    household_2: {
      name: 'household_2',
      display_name: 'Household (2 members)',
      description: 'Perfect for couples or small households',
      pricing: {
        price_monthly: 12.99,
        price_yearly: 119.00,
        mobile_price_yearly: 134.99,
        users_included: 2,
      },
      limits: {
        accounts: -1,
        executions_per_day: -1,
        transaction_history_days: 730,
      },
      features: {
        ads_enabled: false,
        ai_insights: true,
        api_access: true,
        priority_support: true,
        custom_categories: true,
        custom_tags: true,
        advanced_reports: true,
      },
    },
    household_3: {
      name: 'household_3',
      display_name: 'Household (3 members)',
      description: 'Great for growing families',
      pricing: {
        price_monthly: 14.99,
        price_yearly: 139.00,
        mobile_price_yearly: 154.99,
        users_included: 3,
      },
      limits: {
        accounts: -1,
        executions_per_day: -1,
        transaction_history_days: 730,
      },
      features: {
        ads_enabled: false,
        ai_insights: true,
        api_access: true,
        priority_support: true,
        custom_categories: true,
        custom_tags: true,
        advanced_reports: true,
      },
    },
    household_4: {
      name: 'household_4',
      display_name: 'Household (4 members)',
      description: 'Ideal for medium-sized families',
      pricing: {
        price_monthly: 16.99,
        price_yearly: 159.00,
        mobile_price_yearly: 179.99,
        users_included: 4,
      },
      limits: {
        accounts: -1,
        executions_per_day: -1,
        transaction_history_days: 730,
      },
      features: {
        ads_enabled: false,
        ai_insights: true,
        api_access: true,
        priority_support: true,
        custom_categories: true,
        custom_tags: true,
        advanced_reports: true,
      },
    },
    household_5: {
      name: 'household_5',
      display_name: 'Household (5 members)',
      description: 'Best value for large families',
      pricing: {
        price_monthly: 18.99,
        price_yearly: 179.00,
        mobile_price_yearly: 199.99,
        users_included: 5,
      },
      limits: {
        accounts: -1,
        executions_per_day: -1,
        transaction_history_days: 730,
      },
      features: {
        ads_enabled: false,
        ai_insights: true,
        api_access: true,
        priority_support: true,
        custom_categories: true,
        custom_tags: true,
        advanced_reports: true,
      },
    },
  },
  default_tier: 'premium',
}

/**
 * Helper to create a partial tier with defaults filled in
 */
export function createMockTier(overrides: Partial<SubscriptionTier> = {}): SubscriptionTier {
  const { pricing, limits, features, ...rest } = overrides
  return {
    name: 'test',
    display_name: 'Test Tier',
    description: 'Test tier description',
    pricing: createMockPricing(pricing),
    limits: createMockLimits(limits),
    features: createMockFeatures(features),
    ...rest,
  }
}

/**
 * Helper to create mock limits with defaults
 */
export function createMockLimits(overrides: Partial<SubscriptionLimits> = {}): SubscriptionLimits {
  return {
    accounts: -1,
    executions_per_day: -1,
    transaction_history_days: 730,
    ...overrides,
  }
}

/**
 * Helper to create mock features with defaults
 */
export function createMockFeatures(overrides: Partial<SubscriptionFeatures> = {}): SubscriptionFeatures {
  return {
    ads_enabled: false,
    ai_insights: false,
    api_access: false,
    priority_support: false,
    custom_categories: true,
    custom_tags: true,
    advanced_reports: true,
    ...overrides,
  }
}

/**
 * Helper to create mock pricing with defaults
 */
export function createMockPricing(overrides: Partial<SubscriptionPricing> = {}): SubscriptionPricing {
  return {
    price_monthly: 0,
    price_yearly: 0,
    mobile_price_yearly: 0,
    users_included: 1,
    ...overrides,
  }
}

/**
 * Get a specific tier from the mock response
 */
export function getMockTier(tierName: keyof typeof mockSubscriptionTiersResponse.tiers): SubscriptionTier {
  return mockSubscriptionTiersResponse.tiers[tierName]
}
