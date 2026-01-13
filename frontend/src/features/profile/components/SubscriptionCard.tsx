import { Link } from 'react-router-dom'
import { Award, Check } from 'lucide-react'
import { useSubscriptionTiers } from '../../subscription/hooks/useSubscriptionTiers'
import type { SubscriptionTier, TierName } from '../../subscription/types'
import './SubscriptionCard.css'

interface SubscriptionCardProps {
  tier: string // User's subscription_tier from API
  status: string // User's subscription_status from API
}

/**
 * Render the icon for a tier (all tiers use Award now)
 */
function renderTierIcon(_tier: TierName) {
  const iconProps = { className: 'tier-icon', 'aria-hidden': true as const }
  return <Award {...iconProps} />
}

/**
 * Format price for display
 */
function formatPrice(price: number | undefined): string {
  if (price === undefined || price === null) {
    return '$0'
  }
  return price === 0 ? '$0' : `$${price.toFixed(2)}`
}

/**
 * Build human-readable feature list from tier config
 */
function buildFeatureList(tierData: SubscriptionTier): string[] {
  const features: string[] = []
  const { limits, features: featureFlags } = tierData

  // Add account limit
  if (limits.accounts === -1) {
    features.push('Unlimited bank accounts')
  } else {
    features.push(`${limits.accounts} bank accounts`)
  }

  // Rules are unlimited for all tiers
  features.push('Unlimited automation rules')

  // Add execution frequency
  const execsPerDay = limits.executions_per_day
  if (execsPerDay === 1) {
    features.push('Rules run 1x per day')
  } else if (execsPerDay >= 144) {
    features.push('Rules run every 10 minutes')
  } else {
    const hours = Math.round(24 / execsPerDay)
    features.push(`Rules run every ${hours} hours`)
  }

  // Add feature flags
  if (featureFlags.ads_enabled) {
    features.push('Ad-supported')
  } else {
    features.push('No ads')
  }

  if (featureFlags.ai_insights) {
    features.push('AI-powered insights')
  }

  if (featureFlags.priority_support) {
    features.push('Priority support')
  }

  if (featureFlags.advanced_reports) {
    features.push('Advanced reports')
  }

  if (featureFlags.api_access) {
    features.push('API access')
  }

  return features
}

export function SubscriptionCard({ tier, status }: SubscriptionCardProps) {
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

  const isPaidTier = true // All tiers are now paid

  // Generate CSS class name for tier (tier names use underscores: premium_plus)
  const tierClassName = tier

  // Format data for display
  const displayName = tierInfo.display_name
  const displayPrice = formatPrice(tierInfo.pricing.price_monthly)
  const features = buildFeatureList(tierInfo)

  return (
    <div className={`subscription-card tier-${tierClassName}`}>
      <div className="subscription-header">
        <div className="tier-info">
          {renderTierIcon(tier as TierName)}
          <h3 className="tier-name">{displayName}</h3>
        </div>
        <div className="tier-pricing">
          <span className="tier-price">{displayPrice}</span>
          {tierInfo.pricing.price_monthly > 0 && (
            <span className="price-period">/month</span>
          )}
        </div>
        <span className={`status-badge status-${status}`}>{status}</span>
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
        {isPaidTier ? (
          <button className="btn-manage">Manage Subscription</button>
        ) : (
          <button className="btn-upgrade">Upgrade to Premium</button>
        )}
        <Link to="/pricing" className="compare-plans-link">
          Compare Plans →
        </Link>
      </div>
    </div>
  )
}
