import { useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useUser } from '@clerk/react-router'
import { Award, Gem, Check, X, AlertTriangle } from 'lucide-react'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import type { TierName, SubscriptionTier } from '../subscription/types'
import { PaddleCheckoutButton } from '../billing/components/PaddleCheckoutButton'
import { toast } from 'sonner'
import './PricingPage.css'

/**
 * Billing period type for the toggle
 */
type BillingPeriod = 'monthly' | 'yearly'


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
 * Calculate savings percentage for yearly billing
 */
function calculateSavingsPercent(tierData: SubscriptionTier): number {
  const monthlyTotal = tierData.pricing.price_monthly * 12
  const yearlyTotal = tierData.pricing.price_yearly
  if (monthlyTotal === 0) return 0
  return Math.round(((monthlyTotal - yearlyTotal) / monthlyTotal) * 100)
}

/**
 * Get equivalent monthly price for yearly billing
 */
function getEquivalentMonthly(yearlyPrice: number): string {
  if (yearlyPrice === 0) return 'Free'
  return `$${(yearlyPrice / 12).toFixed(2)}`
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

  // Rules are unlimited for all tiers
  features.push({ feature: 'Unlimited automation rules', included: true })

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

function FrozenAccountBanner() {
  return (
    <div className="frozen-account-banner" role="alert">
      <AlertTriangle className="frozen-account-banner__icon" aria-hidden="true" />
      <div className="frozen-account-banner__content">
        <h2 className="frozen-account-banner__title">Your subscription has expired</h2>
        <p className="frozen-account-banner__message">
          Subscribe to continue using BoostedBudget. Your data is safe and will be
          available once you resubscribe.
        </p>
      </div>
    </div>
  )
}

interface TierCardProps {
  tierKey: TierName
  tierData: SubscriptionTier
  isCurrentTier: boolean
  billingPeriod: BillingPeriod
}

function TierCard({ tierKey, tierData, isCurrentTier, billingPeriod }: TierCardProps) {
  const features = buildFeatureList(tierData)
  const isHousehold5 = tierKey === 'household_5'

  // Get the appropriate price based on billing period
  const currentPrice = billingPeriod === 'monthly'
    ? tierData.pricing.price_monthly
    : tierData.pricing.price_yearly
  const isPaidTier = tierData.pricing.price_monthly > 0

  // Get the icon component based on tier (all tiers use Award now)
  const renderTierIcon = () => {
    const iconProps = { className: 'pricing-tier-icon', 'aria-hidden': true as const }
    if (tierKey === 'household_5') {
      return <Gem {...iconProps} />
    }
    return <Award {...iconProps} />
  }

  return (
    <div
      className={`pricing-card ${isCurrentTier ? 'current' : ''} ${isHousehold5 ? 'featured' : ''}`}
    >
      {isCurrentTier && <div className="current-badge">Current Plan</div>}
      {isHousehold5 && !isCurrentTier && (
        <div className="featured-badge">Best Value</div>
      )}

      <div className="pricing-card-header">
        {renderTierIcon()}
        <h2 className="pricing-tier-name">{tierData.display_name}</h2>
        <p className="pricing-tier-description">{tierData.description}</p>
      </div>

      <div className="pricing-card-price">
        {billingPeriod === 'yearly' && isPaidTier && (
          <span className="savings-badge">Save {calculateSavingsPercent(tierData)}%</span>
        )}
        <span className="price-amount">{formatPrice(currentPrice)}</span>
        {currentPrice > 0 && (
          <span className="price-period">/{billingPeriod === 'monthly' ? 'month' : 'year'}</span>
        )}
        {billingPeriod === 'yearly' && isPaidTier && (
          <span className="price-equivalent">
            {getEquivalentMonthly(tierData.pricing.price_yearly)}/mo, billed annually
          </span>
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
        ) : (
          <PaddleCheckoutButton
            tier={tierKey}
            billingInterval={billingPeriod === 'monthly' ? 'month' : 'year'}
            onSuccess={() => {
              toast.success('Subscription activated! Welcome to ' + tierData.display_name)
            }}
          />
        )}
      </div>
    </div>
  )
}

export function PricingPage() {
  const { user } = useUser()
  const location = useLocation()
  const { tiers, loading, error, refetch } = useSubscriptionTiers()
  const [billingPeriod, setBillingPeriod] = useState<BillingPeriod>('monthly')

  const isFrozen = (location.state as { frozen?: boolean } | null)?.frozen === true
  const currentTier: TierName =
    (user?.publicMetadata?.subscription_tier as TierName) || 'premium'

  if (loading) {
    return (
      <div className="pricing-page">
        <div className="pricing-loading">Loading pricing plans...</div>
      </div>
    )
  }

  // Issue #698: Add defensive check for malformed API response
  // tiers.tiers could be undefined if backend config file is missing
  if (error || !tiers || !tiers.tiers) {
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

  // Order tiers for display (free tier removed, household tiers added)
  const tierOrder: TierName[] = ['premium', 'household_2', 'household_3', 'household_4', 'household_5']

  return (
    <div className="pricing-page">
      {isFrozen && <FrozenAccountBanner />}

      <div className="pricing-header">
        <h1>Choose Your Plan</h1>
        <p>Select the plan that best fits your financial goals</p>
      </div>

      <div className="billing-toggle" role="radiogroup" aria-label="Billing period">
        <button
          className={`billing-toggle__option ${billingPeriod === 'monthly' ? 'active' : ''}`}
          onClick={() => setBillingPeriod('monthly')}
          aria-checked={billingPeriod === 'monthly'}
          role="radio"
        >
          Monthly
        </button>
        <button
          className={`billing-toggle__option ${billingPeriod === 'yearly' ? 'active' : ''}`}
          onClick={() => setBillingPeriod('yearly')}
          aria-checked={billingPeriod === 'yearly'}
          role="radio"
        >
          Yearly
        </button>
        <div className={`billing-toggle__slider ${billingPeriod}`} aria-hidden="true" />
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
              billingPeriod={billingPeriod}
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
