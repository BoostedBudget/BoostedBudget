/**
 * Subscription API Types
 *
 * Types matching the backend API response from GET /v1/subscription/tiers
 * These types are runtime-loaded from the backend, not generated from YAML.
 */

export type TierName = 'free' | 'premium_lite' | 'premium' | 'premium_plus'

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

export interface SubscriptionTier {
  display_name: string
  description: string
  price_monthly: number
  price_yearly: number
  stripe_price_id_monthly?: string | null
  stripe_price_id_yearly?: string | null
  limits: TierLimits
  features: TierFeatures
}

export interface SubscriptionTiersResponse {
  tiers: Record<TierName, SubscriptionTier>
  default_tier: TierName
}
