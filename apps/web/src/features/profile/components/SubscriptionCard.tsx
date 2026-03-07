import { Link } from 'react-router-dom'
import { Award, Check } from 'lucide-react'
import { useSubscriptionTiers } from '../../subscription/hooks/useSubscriptionTiers'
import type { SubscriptionTier } from '../../subscription/types'
import './SubscriptionCard.css'

interface SubscriptionCardProps {
  tier: string // User's subscription_tier from API
  status: string // User's subscription_status from API
  trialDaysRemaining?: number
}


/**
 * Format price for display
 */
function formatPrice(price: number | undefined): string {
  return !price ? '$0' : `$${price.toFixed(2)}`
}

/**
 * Build human-readable feature list from tier config
 */
function buildFeatureList(tierData: SubscriptionTier): string[] {
  const { features: featureFlags, pricing } = tierData

  const usersLabel = pricing.users_included === 1
    ? 'For 1 person'
    : `For ${pricing.users_included} household members`

  const features = [
    usersLabel,
    'Unlimited bank accounts',
    'Unlimited automation rules',
    featureFlags.ads_enabled ? 'Ad-supported' : 'No ads',
  ]

  if (featureFlags.ai_insights) features.push('AI-powered insights')
  if (featureFlags.priority_support) features.push('Priority support')
  if (featureFlags.advanced_reports) features.push('Advanced reports')
  if (featureFlags.api_access) features.push('API access')

  return features
}

export function SubscriptionCard({ tier, status, trialDaysRemaining }: SubscriptionCardProps) {
  const { tiers, loading, error } = useSubscriptionTiers()

  // Handle loading state
  if (loading || !tier || !status) {
    return (
      <div className="subscription-card loading">
        <p>Loading subscription details...</p>
      </div>
    )
  }

  // Handle error state
  if (error) {
    return (
      <div className="subscription-card error">
        <p>Error loading subscription: {error.message}</p>
      </div>
    )
  }

  // Get tier info from API response
  const tierInfo = tiers?.tiers[tier]

  // Handle invalid tier (shouldn't happen, but safety check)
  if (!tierInfo) {
    return (
      <div className="subscription-card error">
        <p>Invalid subscription tier</p>
      </div>
    )
  }

  const displayName = tierInfo.display_name
  const displayPrice = formatPrice(tierInfo.pricing.price_monthly)
  const features = buildFeatureList(tierInfo)

  return (
    <div className={`subscription-card tier-${tier}`}>
      <div className="subscription-header">
        <div className="tier-info">
          <Award className="tier-icon" aria-hidden="true" />
          <h3 className="tier-name">{displayName}</h3>
        </div>
        <div className="tier-pricing">
          <span className="tier-price">{displayPrice}</span>
          {tierInfo.pricing.price_monthly > 0 && (
            <span className="price-period">/month</span>
          )}
        </div>
        <span className={`status-badge status-${status}`}>
          {status === 'trial' && trialDaysRemaining !== undefined
            ? `Trial — ${trialDaysRemaining} day${trialDaysRemaining === 1 ? '' : 's'} remaining`
            : status}
        </span>
      </div>

      <div className="subscription-features">
        <ul>
          {features.map((feature, index) => (
            <li key={index}>
              <Check className="feature-check" aria-hidden="true" />
              <span>{feature}</span>
            </li>
          ))}
        </ul>
      </div>

      <div className="subscription-actions">
        <Link to="/billing/manage" className="btn-manage">
          Manage Subscription
        </Link>
        <Link to="/pricing" className="compare-plans-link">
          Compare Plans →
        </Link>
      </div>
    </div>
  )
}
