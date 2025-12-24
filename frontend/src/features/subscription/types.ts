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
} from '@/api/generated/models'

// TierName type for strict tier key typing
export type TierName = 'free' | 'premium_lite' | 'premium' | 'premium_plus'
