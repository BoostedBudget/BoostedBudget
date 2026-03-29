/**
 * Subscription API Types
 *
 * Re-exports generated types from OpenAPI spec for backward compatibility.
 * The backend API spec is the single source of truth.
 */

// Re-export generated types
export type {
  SubscriptionTiersResponse,
  SubscriptionTier,
  SubscriptionLimits as TierLimits,
  SubscriptionFeatures as TierFeatures,
  SubscriptionPricing as TierPricing,
} from '@boostedbudget/api-client/models'

// TierName type for strict tier key typing
// Updated for simplified tiers: premium + household tiers (no free tier)
export type TierName = 'premium' | 'household_2' | 'household_3' | 'household_4' | 'household_5'
