import { useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useUser } from '@clerk/react-router'
import { Award, Gem, Check, X, AlertTriangle } from 'lucide-react'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import type { TierName, SubscriptionTier } from '../subscription/types'
import { PaddleCheckoutButton } from '../billing/components/PaddleCheckoutButton'
import { usePaddleEvents } from '../billing/hooks/usePaddleEvents'
import { useGetCurrentSubscription } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import './PricingPage.css'

type BillingPeriod = 'monthly' | 'yearly'

/**
 * Format price for display
 */
function formatPrice(price: number | undefined): string {
  if (!price) {
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
 * Build feature list with included/excluded status
 */
function buildFeatureList(
  tierData: SubscriptionTier
): Array<{ feature: string; included: boolean }> {
  const { features: featureFlags, pricing } = tierData

  const usersLabel = pricing.users_included === 1
    ? 'For 1 person'
    : `For ${pricing.users_included} household members`

  return [
    { feature: usersLabel, included: true },
    { feature: 'Unlimited bank accounts', included: true },
    { feature: 'Unlimited automation rules', included: true },
    { feature: 'Ad-free experience', included: !featureFlags.ads_enabled },
    { feature: 'AI-powered insights', included: featureFlags.ai_insights || false },
    { feature: 'Priority support', included: featureFlags.priority_support || false },
    { feature: 'Advanced reports', included: featureFlags.advanced_reports || false },
    { feature: 'API access', included: featureFlags.api_access || false },
  ]
}

function TrialBanner({ daysRemaining }: { daysRemaining: number }) {
  return (
    <div className="trial-banner" role="status">
      <div className="trial-banner__content">
        <p className="trial-banner__message">
          You're on a free trial &mdash; <strong>{daysRemaining} day{daysRemaining === 1 ? '' : 's'} remaining</strong>
        </p>
      </div>
    </div>
  )
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
  const savingsPercent = calculateSavingsPercent(tierData)

  const TierIcon = isHousehold5 ? Gem : Award

  return (
    <div
      className={`pricing-card ${isCurrentTier ? 'current' : ''} ${isHousehold5 ? 'featured' : ''}`}
    >
      {isCurrentTier && <div className="current-badge">Current Plan</div>}
      {isHousehold5 && !isCurrentTier && (
        <div className="featured-badge">Best Value</div>
      )}

      <div className="pricing-card-header">
        <TierIcon className="pricing-tier-icon" aria-hidden="true" />
        <h2 className="pricing-tier-name">{tierData.display_name}</h2>
        <p className="pricing-tier-description">{tierData.description}</p>
      </div>

      <div className="pricing-card-price">
        {billingPeriod === 'yearly' && currentPrice > 0 && (
          <span className="savings-badge">Save {savingsPercent}%</span>
        )}
        <span className="price-amount">{formatPrice(currentPrice)}</span>
        {currentPrice > 0 && (
          <span className="price-period">/{billingPeriod === 'monthly' ? 'month' : 'year'}</span>
        )}
        {billingPeriod === 'yearly' && currentPrice > 0 && (
          <span className="price-equivalent">
            ${(tierData.pricing.price_yearly / 12).toFixed(2)}/mo, billed annually
          </span>
        )}
      </div>

      <ul className="pricing-features">
        {features.map((item) => (
          <li key={item.feature} className={item.included ? 'included' : 'excluded'}>
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
  const { data: currentSubscription } = useGetCurrentSubscription()
  const [billingPeriod, setBillingPeriod] = useState<BillingPeriod>('monthly')

  // Listen for Paddle checkout events (completion/cancellation)
  usePaddleEvents()

  const isFrozen = (location.state as { frozen?: boolean } | null)?.frozen === true

  // Trial users (status="trial") get all tiers shown as Subscribe, unlike trialing users
  // who have paid subscriptions and show "Current Plan"
  const isTrialUser = currentSubscription?.status === 'trial'
  const currentTier: TierName | null = isTrialUser
    ? null
    : (user?.publicMetadata?.subscription_tier as TierName) || 'premium'

  if (loading) {
    return (
      <div className="pricing-page">
        <div className="pricing-loading">Loading pricing plans...</div>
      </div>
    )
  }

  // Defensive check for malformed API response
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
      {isTrialUser && currentSubscription?.trial_days_remaining !== undefined && (
        <TrialBanner daysRemaining={currentSubscription.trial_days_remaining} />
      )}

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
          {tiers?.tiers.premium
            ? `Yearly (Save ${calculateSavingsPercent(tiers.tiers.premium)}%)`
            : 'Yearly'}
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
              processor, Paddle.
            </p>
          </div>
          <div className="faq-item">
            <h3>Is there a free trial?</h3>
            <p>
              Yes! Premium plans come with a 60-day free trial. No credit card
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
