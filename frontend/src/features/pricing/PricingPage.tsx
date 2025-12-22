import { useAuth0 } from '@auth0/auth0-react'
import { Star, Award, Gem, Check, X } from 'lucide-react'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import type { TierName, SubscriptionTier } from '../subscription/types'
import './PricingPage.css'


/**
 * Format price for display
 */
function formatPrice(price: number | undefined): string {
  if (price === undefined || price === null || price === 0) {
    return 'Free'
  }
  return `$${price.toFixed(2)}`
}

/**
 * Build feature list with included/excluded status
 */
function buildFeatureList(
  tierData: SubscriptionTier
): Array<{ feature: string; included: boolean }> {
  const features: Array<{ feature: string; included: boolean }> = []
  const { limits, features: featureFlags } = tierData

  // Account limit
  if (limits.accounts === -1) {
    features.push({ feature: 'Unlimited bank accounts', included: true })
  } else {
    features.push({
      feature: `${limits.accounts} bank accounts`,
      included: true,
    })
  }

  // Rules limit
  if (limits.rules === -1) {
    features.push({ feature: 'Unlimited automation rules', included: true })
  } else {
    features.push({
      feature: `${limits.rules} automation rule${limits.rules > 1 ? 's' : ''}`,
      included: true,
    })
  }

  // Execution frequency
  const execsPerDay = limits.executions_per_day
  if (execsPerDay >= 144) {
    features.push({ feature: 'Rules run every 10 minutes', included: true })
  } else if (execsPerDay === 1) {
    features.push({ feature: 'Rules run once daily', included: true })
  } else {
    const hours = Math.round(24 / execsPerDay)
    features.push({ feature: `Rules run every ${hours} hours`, included: true })
  }

  // Feature flags
  features.push({
    feature: 'Ad-free experience',
    included: !featureFlags.ads_enabled,
  })
  features.push({
    feature: 'AI-powered insights',
    included: featureFlags.ai_insights || false,
  })
  features.push({
    feature: 'Priority support',
    included: featureFlags.priority_support || false,
  })
  features.push({
    feature: 'Advanced reports',
    included: featureFlags.advanced_reports || false,
  })
  features.push({
    feature: 'API access',
    included: featureFlags.api_access || false,
  })

  return features
}

interface TierCardProps {
  tierKey: TierName
  tierData: SubscriptionTier
  isCurrentTier: boolean
}

function TierCard({ tierKey, tierData, isCurrentTier }: TierCardProps) {
  const features = buildFeatureList(tierData)
  const isFreeTier = tierKey === 'free'
  const isPremiumPlus = tierKey === 'premium_plus'

  // Get the icon component based on tier
  const renderTierIcon = () => {
    const iconProps = { className: 'pricing-tier-icon', 'aria-hidden': true as const }
    switch (tierKey) {
      case 'free':
        return <Star {...iconProps} />
      case 'premium_plus':
        return <Gem {...iconProps} />
      default:
        return <Award {...iconProps} />
    }
  }

  return (
    <div
      className={`pricing-card ${isCurrentTier ? 'current' : ''} ${isPremiumPlus ? 'featured' : ''}`}
    >
      {isCurrentTier && <div className="current-badge">Current Plan</div>}
      {isPremiumPlus && !isCurrentTier && (
        <div className="featured-badge">Best Value</div>
      )}

      <div className="pricing-card-header">
        {renderTierIcon()}
        <h2 className="pricing-tier-name">{tierData.display_name}</h2>
        <p className="pricing-tier-description">{tierData.description}</p>
      </div>

      <div className="pricing-card-price">
        <span className="price-amount">{formatPrice(tierData.pricing.price_monthly)}</span>
        {tierData.pricing.price_monthly > 0 && (
          <span className="price-period">/month</span>
        )}
      </div>

      <ul className="pricing-features">
        {features.map((item, index) => (
          <li key={index} className={item.included ? 'included' : 'excluded'}>
            {item.included ? (
              <Check className="feature-icon included" aria-hidden="true" />
            ) : (
              <X className="feature-icon excluded" aria-hidden="true" />
            )}
            <span>{item.feature}</span>
          </li>
        ))}
      </ul>

      <div className="pricing-card-action">
        {isCurrentTier ? (
          <button className="btn-current" disabled>
            Current Plan
          </button>
        ) : isFreeTier ? (
          <button className="btn-downgrade">Downgrade</button>
        ) : (
          <button className="btn-select">
            {tierKey === 'premium_plus' ? 'Go Premium+' : 'Select Plan'}
          </button>
        )}
      </div>
    </div>
  )
}

export function PricingPage() {
  const { user } = useAuth0()
  const { tiers, loading, error, refetch } = useSubscriptionTiers()

  // Get current user's tier from user metadata or default to 'free'
  // Note: In production, this would come from your user state/context
  const currentTier: TierName =
    (user?.['https://boostedbudget.com/subscription_tier'] as TierName) || 'free'

  if (loading) {
    return (
      <div className="pricing-page">
        <div className="pricing-loading">Loading pricing plans...</div>
      </div>
    )
  }

  if (error || !tiers) {
    return (
      <div className="pricing-page">
        <div className="pricing-error">
          <p>Failed to load pricing plans.</p>
          <button onClick={refetch} className="btn-retry">
            Try Again
          </button>
        </div>
      </div>
    )
  }

  // Order tiers for display
  const tierOrder: TierName[] = ['free', 'premium_lite', 'premium', 'premium_plus']

  return (
    <div className="pricing-page">
      <div className="pricing-header">
        <h1>Choose Your Plan</h1>
        <p>Select the plan that best fits your financial goals</p>
      </div>

      <div className="pricing-grid">
        {tierOrder.map((tierKey) => {
          const tierData = tiers.tiers[tierKey]
          if (!tierData) return null
          return (
            <TierCard
              key={tierKey}
              tierKey={tierKey}
              tierData={tierData}
              isCurrentTier={tierKey === currentTier}
            />
          )
        })}
      </div>

      <div className="pricing-faq">
        <h2>Frequently Asked Questions</h2>
        <div className="faq-grid">
          <div className="faq-item">
            <h3>Can I change plans anytime?</h3>
            <p>
              Yes! You can upgrade or downgrade your plan at any time. Changes
              take effect immediately.
            </p>
          </div>
          <div className="faq-item">
            <h3>What payment methods do you accept?</h3>
            <p>
              We accept all major credit cards through our secure payment
              processor, Stripe.
            </p>
          </div>
          <div className="faq-item">
            <h3>Is there a free trial?</h3>
            <p>
              Yes! Premium plans come with a 14-day free trial. No credit card
              required to start.
            </p>
          </div>
          <div className="faq-item">
            <h3>Can I cancel anytime?</h3>
            <p>
              Absolutely. You can cancel your subscription at any time with no
              penalty.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
