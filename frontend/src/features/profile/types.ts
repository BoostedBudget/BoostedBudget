// User profile types matching backend domain/user.go

export interface User {
  id: string
  auth0_user_id: string
  email: string
  email_verified: boolean
  subscription_tier: 'free' | 'premium' | 'premium+'
  subscription_status: string
  primary_region: string
  is_active: boolean
  is_deleted: boolean
  created_at: string
  updated_at: string
  last_login_at: string | null
}

export interface UpdateProfileRequest {
  email?: string
  primary_region?: string
}

// Note: Region validation now uses Fly.io IATA codes (iad, lhr, syd)
// See src/config/regions.ts for full region configuration
export const VALID_REGIONS = ['iad', 'lhr', 'syd'] as const
export type ValidRegion = typeof VALID_REGIONS[number]

export const SUBSCRIPTION_TIERS = {
  free: {
    name: 'Free',
    price: '$0',
    features: ['3 automation rules', '2 linked accounts', 'Ad-supported'],
  },
  premium: {
    name: 'Premium',
    price: '$9.99/mo',
    features: ['Unlimited rules', 'Unlimited accounts', 'No ads'],
  },
  'premium+': {
    name: 'Premium+',
    price: '$19.99/mo',
    features: ['Everything in Premium', 'AI-powered insights', 'Priority support'],
  },
} as const

// Privacy settings types matching backend domain/privacy.go

export interface PrivacySettings {
  id: string
  user_id: string
  gdpr_necessary_consent: boolean
  gdpr_analytics_consent: boolean
  gdpr_advertising_consent: boolean
  gdpr_consent_timestamp: string
  marketing_emails_enabled: boolean
  product_updates_enabled: boolean
  security_alerts_enabled: boolean
  data_sharing_enabled: boolean
  created_at: string
  updated_at: string
}

export interface UpdatePrivacySettingsRequest {
  gdpr_necessary_consent?: boolean
  gdpr_analytics_consent?: boolean
  gdpr_advertising_consent?: boolean
  marketing_emails_enabled?: boolean
  product_updates_enabled?: boolean
  security_alerts_enabled?: boolean
  data_sharing_enabled?: boolean
}
