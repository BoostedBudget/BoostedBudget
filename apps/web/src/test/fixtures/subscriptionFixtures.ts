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
 * Complete mock subscription tiers response matching the generated types
 * Simplified tier structure: premium + household tiers (2-5 members)
 * All tiers have unlimited rules
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
        stripe_price_id_monthly: undefined,
        stripe_price_id_yearly: undefined,
      },
      limits: {
        accounts: 30,
        rules: -1,
        executions_per_day: 6,
        min_execution_interval_minutes: 240,
        accounts_synced_per_month: 1000,
        api_calls_per_month: 500,
        custom_categories_max: 50,
        custom_tags_max: 100,
        data_export_requests_per_month: 10,
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
        stripe_price_id_monthly: undefined,
        stripe_price_id_yearly: undefined,
      },
      limits: {
        accounts: 60,
        rules: -1,
        executions_per_day: 12,
        min_execution_interval_minutes: 120,
        accounts_synced_per_month: 2000,
        api_calls_per_month: 1000,
        custom_categories_max: 100,
        custom_tags_max: 200,
        data_export_requests_per_month: 20,
        transaction_history_days: 1095,
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
        stripe_price_id_monthly: undefined,
        stripe_price_id_yearly: undefined,
      },
      limits: {
        accounts: 90,
        rules: -1,
        executions_per_day: 18,
        min_execution_interval_minutes: 60,
        accounts_synced_per_month: 3000,
        api_calls_per_month: 1500,
        custom_categories_max: 150,
        custom_tags_max: 300,
        data_export_requests_per_month: 30,
        transaction_history_days: 1460,
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
        stripe_price_id_monthly: undefined,
        stripe_price_id_yearly: undefined,
      },
      limits: {
        accounts: 120,
        rules: -1,
        executions_per_day: 24,
        min_execution_interval_minutes: 30,
        accounts_synced_per_month: 4000,
        api_calls_per_month: 2000,
        custom_categories_max: 200,
        custom_tags_max: 400,
        data_export_requests_per_month: 40,
        transaction_history_days: 1825,
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
        stripe_price_id_monthly: undefined,
        stripe_price_id_yearly: undefined,
      },
      limits: {
        accounts: 150,
        rules: -1,
        executions_per_day: 30,
        min_execution_interval_minutes: 10,
        accounts_synced_per_month: -1,
        api_calls_per_month: -1,
        custom_categories_max: -1,
        custom_tags_max: -1,
        data_export_requests_per_month: -1,
        transaction_history_days: -1,
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
    accounts: 10,
    rules: -1,
    executions_per_day: 6,
    min_execution_interval_minutes: 240,
    accounts_synced_per_month: 1000,
    api_calls_per_month: 500,
    custom_categories_max: 50,
    custom_tags_max: 100,
    data_export_requests_per_month: 10,
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
    stripe_price_id_monthly: undefined,
    stripe_price_id_yearly: undefined,
    ...overrides,
  }
}

/**
 * Get a specific tier from the mock response
 */
export function getMockTier(tierName: keyof typeof mockSubscriptionTiersResponse.tiers): SubscriptionTier {
  return mockSubscriptionTiersResponse.tiers[tierName]
}
