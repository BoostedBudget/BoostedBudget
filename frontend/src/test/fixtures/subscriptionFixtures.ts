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
 */
export const mockSubscriptionTiersResponse: SubscriptionTiersResponse = {
  tiers: {
    free: {
      name: 'free',
      display_name: 'Free',
      description: 'Basic plan with limited features',
      pricing: {
        price_monthly: 0,
        price_yearly: 0,
        stripe_price_id_monthly: undefined,
        stripe_price_id_yearly: undefined,
      },
      limits: {
        accounts: 3,
        rules: 1,
        executions_per_day: 1,
        min_execution_interval_minutes: 1440,
        accounts_synced_per_month: 100,
        api_calls_per_month: 0,
        custom_categories_max: 5,
        custom_tags_max: 10,
        data_export_requests_per_month: 1,
        transaction_history_days: 30,
      },
      features: {
        ads_enabled: true,
        ai_insights: false,
        api_access: false,
        priority_support: false,
        custom_categories: false,
        custom_tags: false,
        advanced_reports: false,
      },
    },
    premium_lite: {
      name: 'premium_lite',
      display_name: 'Premium Lite',
      description: 'Enhanced features for casual users',
      pricing: {
        price_monthly: 4.99,
        price_yearly: 49.99,
        stripe_price_id_monthly: 'price_lite_monthly',
        stripe_price_id_yearly: 'price_lite_yearly',
      },
      limits: {
        accounts: 15,
        rules: 5,
        executions_per_day: 2,
        min_execution_interval_minutes: 720,
        accounts_synced_per_month: 500,
        api_calls_per_month: 100,
        custom_categories_max: 20,
        custom_tags_max: 50,
        data_export_requests_per_month: 5,
        transaction_history_days: 365,
      },
      features: {
        ads_enabled: false,
        ai_insights: false,
        api_access: false,
        priority_support: false,
        custom_categories: true,
        custom_tags: true,
        advanced_reports: false,
      },
    },
    premium: {
      name: 'premium',
      display_name: 'Premium',
      description: 'Full-featured budgeting experience',
      pricing: {
        price_monthly: 9.99,
        price_yearly: 99.99,
        stripe_price_id_monthly: 'price_premium_monthly',
        stripe_price_id_yearly: 'price_premium_yearly',
      },
      limits: {
        accounts: 30,
        rules: 15,
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
    premium_plus: {
      name: 'premium_plus',
      display_name: 'Premium+',
      description: 'Unlimited access with AI insights and API',
      pricing: {
        price_monthly: 19.99,
        price_yearly: 199.99,
        stripe_price_id_monthly: 'price_plus_monthly',
        stripe_price_id_yearly: 'price_plus_yearly',
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
  default_tier: 'free',
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
    accounts: 3,
    rules: 1,
    executions_per_day: 1,
    min_execution_interval_minutes: 1440,
    accounts_synced_per_month: 100,
    api_calls_per_month: 0,
    custom_categories_max: 5,
    custom_tags_max: 10,
    data_export_requests_per_month: 1,
    transaction_history_days: 30,
    ...overrides,
  }
}

/**
 * Helper to create mock features with defaults
 */
export function createMockFeatures(overrides: Partial<SubscriptionFeatures> = {}): SubscriptionFeatures {
  return {
    ads_enabled: true,
    ai_insights: false,
    api_access: false,
    priority_support: false,
    custom_categories: false,
    custom_tags: false,
    advanced_reports: false,
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
export function getMockTier(tierName: 'free' | 'premium_lite' | 'premium' | 'premium_plus'): SubscriptionTier {
  return mockSubscriptionTiersResponse.tiers[tierName]
}
