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
} from '@/api/generated/models'

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
        price_monthly: 9.99,
        price_yearly: 99.99,
        stripe_price_id_monthly: 'price_premium_monthly',
        stripe_price_id_yearly: 'price_premium_yearly',
      },
      limits: {
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
      },
      features: {
        ads_enabled: false,
        ai_insights: false,
        api_access: false,
        priority_support: false,
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
        price_monthly: 14.99,
        price_yearly: 149.99,
        stripe_price_id_monthly: 'price_household_2_monthly',
        stripe_price_id_yearly: 'price_household_2_yearly',
      },
      limits: {
        accounts: 20,
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
        ai_insights: false,
        api_access: false,
        priority_support: false,
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
        price_monthly: 19.99,
        price_yearly: 199.99,
        stripe_price_id_monthly: 'price_household_3_monthly',
        stripe_price_id_yearly: 'price_household_3_yearly',
      },
      limits: {
        accounts: 30,
        rules: -1,
        executions_per_day: 24,
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
        api_access: false,
        priority_support: false,
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
        price_monthly: 24.99,
        price_yearly: 249.99,
        stripe_price_id_monthly: 'price_household_4_monthly',
        stripe_price_id_yearly: 'price_household_4_yearly',
      },
      limits: {
        accounts: 40,
        rules: -1,
        executions_per_day: 48,
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
        priority_support: false,
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
        price_monthly: 29.99,
        price_yearly: 299.99,
        stripe_price_id_monthly: 'price_household_5_monthly',
        stripe_price_id_yearly: 'price_household_5_yearly',
      },
      limits: {
        accounts: -1,
        rules: -1,
        executions_per_day: 144,
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
  return {
    name: 'test',
    display_name: 'Test Tier',
    description: 'Test tier description',
    pricing: createMockPricing(overrides.pricing),
    limits: createMockLimits(overrides.limits),
    features: createMockFeatures(overrides.features),
    ...overrides,
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
export function getMockTier(tierName: 'premium' | 'household_2' | 'household_3' | 'household_4' | 'household_5'): SubscriptionTier {
  return mockSubscriptionTiersResponse.tiers[tierName]
}
