/**
 * COMMITTED FILE - Edit YAML, not this file
 *
 * Generated from: config/subscription_tiers.yaml
 * To regenerate: npm run generate:types
 *
 * This file is committed to git and validated by tests.
 * If YAML changes without regeneration, CI will fail.
 */

export type SubscriptionTier = 'free' | 'premium_lite' | 'premium' | 'premium_plus'

export interface TierLimits {
  accounts: number
  rules: number
  executions_per_day: number
  min_execution_interval_minutes: number
  api_calls_per_month?: number
  data_export_requests_per_month?: number
}

export interface TierFeatures {
  ads_enabled: boolean
  ai_insights: boolean
  api_access?: boolean
  priority_support?: boolean
  custom_categories?: boolean
  advanced_reports?: boolean
}

export interface TierConfig {
  display_name: string
  description: string
  price_monthly: number
  price_yearly: number
  stripe_price_id_monthly?: string | null
  stripe_price_id_yearly?: string | null
  limits: TierLimits
  features: TierFeatures
}

export interface SubscriptionTiersConfig {
  free: TierConfig
  premium_lite: TierConfig
  premium: TierConfig
  premium_plus: TierConfig
}

export const SUBSCRIPTION_TIERS = {
  free: {
    name: 'Free',
    description: 'Full budgeting with basic automation',
    priceMonthly: '$0',
    priceYearly: '$0',
    features: ['3 bank accounts', '1 automation rules', 'Rules run 1x per day', 'Ad-supported'],
    limits: {
      accounts: 3,
      rules: 1,
      executionsPerDay: 1,
      minExecutionIntervalMinutes: 1440,
    },
  },
  premium_lite: {
    name: 'Premium Lite',
    description: 'Perfect for couples and light automation',
    priceMonthly: '$4.99',
    priceYearly: '$49.99',
    features: ['15 bank accounts', '5 automation rules', 'Rules run every 12 hours', 'No ads'],
    limits: {
      accounts: 15,
      rules: 5,
      executionsPerDay: 2,
      minExecutionIntervalMinutes: 720,
    },
  },
  premium: {
    name: 'Premium',
    description: 'Power users with complex finances',
    priceMonthly: '$9.99',
    priceYearly: '$99.99',
    features: ['30 bank accounts', '15 automation rules', 'Rules run every 4 hours', 'No ads', 'Priority support', 'Advanced reports'],
    limits: {
      accounts: 30,
      rules: 15,
      executionsPerDay: 6,
      minExecutionIntervalMinutes: 240,
    },
  },
  premium_plus: {
    name: 'Premium+',
    description: 'Unlimited everything with AI and API access',
    priceMonthly: '$19.99',
    priceYearly: '$199.99',
    features: ['Unlimited bank accounts', 'Unlimited automation rules', 'Rules run every 10 minutes', 'No ads', 'AI-powered insights', 'Priority support', 'Advanced reports', 'API access'],
    limits: {
      accounts: -1,
      rules: -1,
      executionsPerDay: 144,
      minExecutionIntervalMinutes: 10,
    },
  }
} as const
