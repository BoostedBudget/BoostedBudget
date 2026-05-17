/**
 * Profile API Types
 *
 * Re-exports generated types from OpenAPI spec for backward compatibility.
 * The backend API spec is the single source of truth.
 */

// Re-export generated types
export type {
  User,
  UpdateProfileRequest,
  PrivacySettings,
  UpdatePrivacySettingsRequest,
} from '@boostedbudget/api-client/models'

// Import subscription tier name type from new API-based subscription feature
import type { TierName } from '../subscription/types'

// For backwards compatibility, export as SubscriptionTier
export type SubscriptionTier = TierName

