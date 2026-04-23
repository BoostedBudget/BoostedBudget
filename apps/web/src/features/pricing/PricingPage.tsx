import { useState, useEffect } from 'react'
import { useLocation } from 'react-router-dom'
import { Award, Gem, Check, X, AlertTriangle } from 'lucide-react'
import type { TierName, SubscriptionTier } from '../subscription/types'
import { usePaddleEvents } from '../billing/hooks/usePaddleEvents'
import {
  useGetBillingPortalUrl,
  useGetSubscriptionTiers,
  useUpdatePaddleSubscription,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { PlanChangeModal } from '../billing/components/PlanChangeModal'
import { TierActionButton } from './components/TierActionButton'
import { ResumeButton } from './components/ResumeButton'
import { usePricingContext } from './hooks/usePricingContext'
import type { PricingState } from './hooks/usePricingContext'
import { usePostSignInCheckoutIntent } from './hooks/usePostSignInCheckoutIntent'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'

type BillingPeriod = 'monthly' | 'yearly'

function formatPrice(price: number | undefined): string {
  if (!price) return 'Free'
  return `$${price.toFixed(2)}`
}

function calculateSavingsPercent(tierData: SubscriptionTier): number {
  const monthlyTotal = tierData.pricing.price_monthly * 12
  const yearlyTotal = tierData.pricing.price_yearly
  if (monthlyTotal === 0) return 0
  return Math.round(((monthlyTotal - yearlyTotal) / monthlyTotal) * 100)
}

function buildFeatureList(tierData: SubscriptionTier): Array<{ feature: string; included: boolean }> {
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
    <div
      className="flex items-center bg-info/10 border border-blue-400 border-l-4 border-l-info rounded-lg px-6 py-4 mb-8"
      role="status"
    >
      <div className="flex-1">
        <p className="text-[0.9375rem] text-info m-0 leading-relaxed">
          You're on a free trial &mdash; <strong>{daysRemaining} day{daysRemaining === 1 ? '' : 's'} remaining</strong>
        </p>
      </div>
    </div>
  )
}

function FrozenAccountBanner() {
  return (
    <div
      className="flex items-start gap-4 bg-gradient-to-r from-warning/20 to-warning/30 border border-warning border-l-4 border-l-warning rounded-lg px-6 py-5 mb-8"
      role="alert"
    >
      <AlertTriangle className="shrink-0 w-6 h-6 text-warning mt-0.5" aria-hidden="true" />
      <div className="flex-1">
        <h2 className="text-base font-semibold text-warning m-0 mb-1.5">Your subscription has expired</h2>
        <p className="text-[0.9375rem] text-warning/90 m-0 leading-relaxed">
          Subscribe to continue using BoostedBudget. Your data is safe and will be
          available once you resubscribe.
        </p>
      </div>
    </div>
  )
}

function hasTier(state: PricingState): state is Extract<PricingState, { tier: TierName }> {
  return 'tier' in state
}

interface TierCardProps {
  tierKey: TierName
  tierData: SubscriptionTier
  pricingState: PricingState
  tiers: Record<TierName, SubscriptionTier> | null
  billingPeriod: BillingPeriod
  billingPortalUrl: string | undefined
  isLoadingPortal: boolean
  onChangePlan: (tier: TierName, interval: 'month' | 'year') => void
}

function TierCard({
  tierKey,
  tierData,
  pricingState,
  tiers,
  billingPeriod,
  billingPortalUrl,
  isLoadingPortal,
  onChangePlan,
}: TierCardProps) {
  const features = buildFeatureList(tierData)
  const isHousehold5 = tierKey === 'household_5'
  const billingInterval = billingPeriod === 'monthly' ? 'month' : 'year'

  const isCurrentTier =
    ['active', 'trialing', 'cancelled', 'past_due'].includes(pricingState.status) &&
    hasTier(pricingState) &&
    pricingState.tier === tierKey

  const shouldShowResume =
    (pricingState.status === 'cancelled' || pricingState.status === 'past_due') &&
    hasTier(pricingState) &&
    pricingState.tier === tierKey

  const currentPrice = billingPeriod === 'monthly'
    ? tierData.pricing.price_monthly
    : tierData.pricing.price_yearly
  const savingsPercent = calculateSavingsPercent(tierData)

  const TierIcon = isHousehold5 ? Gem : Award

  return (
    <div
      className={`pricing-card${isHousehold5 ? ' featured' : ''} relative flex flex-col rounded-2xl p-8 transition-all duration-300 border-2 bg-card hover:shadow-lg ${
        isCurrentTier
          ? 'border-primary bg-gradient-to-b from-card to-primary/5'
          : isHousehold5
            ? 'border-warning bg-gradient-to-b from-card to-warning/5 hover:border-warning/80'
            : 'border-border hover:border-muted-foreground'
      }`}
    >
      {isCurrentTier && (
        <div className="absolute -top-3 left-1/2 -translate-x-1/2 px-4 py-1.5 rounded-full text-xs font-semibold uppercase tracking-wide whitespace-nowrap bg-primary text-primary-foreground">
          Current Plan
        </div>
      )}
      {isHousehold5 && !isCurrentTier && (
        <div className="absolute -top-3 left-1/2 -translate-x-1/2 px-4 py-1.5 rounded-full text-xs font-semibold uppercase tracking-wide whitespace-nowrap bg-warning text-white">
          Best Value
        </div>
      )}

      <div className="text-center mb-6">
        <TierIcon
          className={`w-10 h-10 mb-3 mx-auto ${
            isCurrentTier ? 'text-primary' : isHousehold5 ? 'text-warning' : 'text-muted-foreground'
          }`}
          aria-hidden="true"
        />
        <h2 className="text-2xl font-bold text-foreground m-0 mb-2">{tierData.display_name}</h2>
        <p className="text-sm text-muted-foreground m-0 leading-snug">{tierData.description}</p>
      </div>

      <div className="text-center mb-6 pb-6 border-b border-muted">
        {billingPeriod === 'yearly' && currentPrice > 0 && (
          <span className="inline-block bg-success text-white text-xs font-semibold px-3 py-1 rounded-full mb-2 animate-[fadeSlideIn_0.3s_ease_forwards]">
            Save {savingsPercent}%
          </span>
        )}
        <div>
          <span className="text-4xl font-bold text-foreground">{formatPrice(currentPrice)}</span>
          {currentPrice > 0 && (
            <span className="text-base text-muted-foreground font-normal">/{billingPeriod === 'monthly' ? 'month' : 'year'}</span>
          )}
        </div>
        {billingPeriod === 'yearly' && currentPrice > 0 && (
          <span className="block text-sm text-muted-foreground mt-1">
            ${(tierData.pricing.price_yearly / 12).toFixed(2)}/mo, billed annually
          </span>
        )}
      </div>

      <ul className="list-none p-0 m-0 mb-6 flex-grow">
        {features.map((item) => (
          <li
            key={item.feature}
            className={`flex items-center gap-3 py-2 text-[0.9375rem] ${item.included ? 'text-foreground' : 'text-muted-foreground'}`}
          >
            {item.included ? (
              <Check className="w-4 h-4 text-success shrink-0" aria-hidden="true" />
            ) : (
              <X className="w-4 h-4 text-border shrink-0" aria-hidden="true" />
            )}
            <span>{item.feature}</span>
          </li>
        ))}
      </ul>

      <div className="mt-auto">
        {shouldShowResume ? (
          <ResumeButton billingPortalUrl={billingPortalUrl} isLoadingPortal={isLoadingPortal} />
        ) : (
          <TierActionButton
            tierKey={tierKey}
            billingInterval={billingInterval}
            pricingState={pricingState}
            tiers={tiers}
            onChangePlan={onChangePlan}
          />
        )}
      </div>
    </div>
  )
}

const TIER_ORDER: TierName[] = ['premium', 'household_2', 'household_3', 'household_4', 'household_5']

export function PricingPage() {
  const location = useLocation()
  const { pricingState, tiers } = usePricingContext()
  const [billingPeriod, setBillingPeriod] = useState<BillingPeriod>('monthly')
  const [planChangeModal, setPlanChangeModal] = useState<{
    open: boolean
    initialTier?: TierName
    initialInterval?: 'month' | 'year'
  }>({ open: false })

  usePostSignInCheckoutIntent()
  usePaddleEvents()

  const trackEvent = useTrackEvent()
  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.PRICING_VIEWED)
  }, [trackEvent])

  const updateMutation = useUpdatePaddleSubscription()

  const billingPortalQuery = useGetBillingPortalUrl({
    query: {
      enabled: pricingState.status === 'cancelled' || pricingState.status === 'past_due',
    },
  })

  // For error-state retry (shared cache with usePricingContext's internal query)
  const { refetch: refetchTiers } = useGetSubscriptionTiers({
    query: { staleTime: 24 * 60 * 60 * 1000 },
  })

  const isFrozen =
    (location.state as { frozen?: boolean } | null)?.frozen === true ||
    pricingState.status === 'past_due'

  if (pricingState.status === 'loading') {
    return (
      <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
        <div className="text-center py-16 px-8 text-muted-foreground">Loading pricing plans...</div>
      </div>
    )
  }

  if (tiers === null) {
    return (
      <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
        <div className="text-center py-16 px-8 text-destructive">
          <p>Failed to load pricing plans.</p>
          <button
            onClick={() => refetchTiers()}
            className="mt-4 bg-primary text-primary-foreground border-none px-6 py-3 rounded-md text-base cursor-pointer hover:opacity-90 transition-opacity"
          >
            Try Again
          </button>
        </div>
      </div>
    )
  }

  const handleChangePlan = (tier: TierName, interval: 'month' | 'year') => {
    setPlanChangeModal({ open: true, initialTier: tier, initialInterval: interval })
  }

  const handleUpdateSubscription = (
    tier: string,
    interval: 'month' | 'year',
    prorationMode: 'prorated_immediately' | 'full_next_billing_period',
  ) => {
    updateMutation.mutateAsync({
      data: {
        new_tier: tier,
        billing_interval: interval,
        proration_billing_mode: prorationMode,
      },
    }).then(() => {
      setPlanChangeModal({ open: false })
    }).catch(() => {
      // Error state visible via updateMutation.error
    })
  }

  const showPlanChangeModal = ['active', 'trialing', 'cancelled'].includes(pricingState.status)
  const currentTierForModal = hasTier(pricingState) ? pricingState.tier : 'premium'
  const currentIntervalForModal = (pricingState as { billingInterval?: 'month' | 'year' }).billingInterval ?? 'month'

  return (
    <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
      {isFrozen && <FrozenAccountBanner />}
      {pricingState.status === 'trial' && (
        <TrialBanner daysRemaining={pricingState.daysRemaining} />
      )}

      <div className="text-center mb-12">
        <h1 className="text-4xl font-bold text-foreground mb-2">Choose Your Plan</h1>
        <p className="text-lg text-muted-foreground">Select the plan that best fits your financial goals</p>
      </div>

      <div
        className="flex justify-center mb-8 relative bg-muted rounded-3xl p-1 w-fit mx-auto"
        role="radiogroup"
        aria-label="Billing period"
      >
        <button
          className={`relative z-10 border-none px-6 py-2.5 text-[0.9375rem] font-medium rounded-2xl cursor-pointer transition-all duration-300 ${
            billingPeriod === 'monthly'
              ? 'text-foreground bg-card shadow-sm'
              : 'text-muted-foreground bg-transparent hover:text-foreground'
          }`}
          onClick={() => setBillingPeriod('monthly')}
          aria-checked={billingPeriod === 'monthly'}
          role="radio"
        >
          Monthly
        </button>
        <button
          className={`relative z-10 border-none px-6 py-2.5 text-[0.9375rem] font-medium rounded-2xl cursor-pointer transition-all duration-300 ${
            billingPeriod === 'yearly'
              ? 'text-foreground bg-card shadow-sm'
              : 'text-muted-foreground bg-transparent hover:text-foreground'
          }`}
          onClick={() => setBillingPeriod('yearly')}
          aria-checked={billingPeriod === 'yearly'}
          role="radio"
        >
          {tiers.premium
            ? `Yearly (Save ${calculateSavingsPercent(tiers.premium)}%)`
            : 'Yearly'}
        </button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-6 mb-16">
        {TIER_ORDER.map((tierKey) => {
          const tierData = tiers[tierKey]
          if (!tierData) return null
          return (
            <TierCard
              key={tierKey}
              tierKey={tierKey}
              tierData={tierData}
              pricingState={pricingState}
              tiers={tiers}
              billingPeriod={billingPeriod}
              billingPortalUrl={billingPortalQuery.data?.portal_url}
              isLoadingPortal={billingPortalQuery.isLoading}
              onChangePlan={handleChangePlan}
            />
          )
        })}
      </div>

      {showPlanChangeModal && (
        <PlanChangeModal
          open={planChangeModal.open}
          currentTier={currentTierForModal}
          currentBillingInterval={currentIntervalForModal}
          tiers={tiers as Record<string, SubscriptionTier>}
          isUpdating={updateMutation.isPending}
          onChangePlan={handleUpdateSubscription}
          onClose={() => setPlanChangeModal({ open: false })}
          initialTier={planChangeModal.initialTier}
          initialInterval={planChangeModal.initialInterval}
        />
      )}

      <div className="mt-16 pt-12 border-t border-border">
        <h2 className="text-center text-3xl font-bold text-foreground mb-8">Frequently Asked Questions</h2>
        <div className="grid grid-cols-1 sm:grid-cols-[repeat(auto-fit,minmax(280px,1fr))] gap-8">
          <div className="bg-muted/50 rounded-xl p-6">
            <h3 className="text-base font-semibold text-foreground m-0 mb-3">Can I change plans anytime?</h3>
            <p className="text-[0.9375rem] text-muted-foreground m-0 leading-relaxed">
              Yes! You can upgrade or downgrade your plan at any time. Changes
              take effect immediately.
            </p>
          </div>
          <div className="bg-muted/50 rounded-xl p-6">
            <h3 className="text-base font-semibold text-foreground m-0 mb-3">What payment methods do you accept?</h3>
            <p className="text-[0.9375rem] text-muted-foreground m-0 leading-relaxed">
              We accept all major credit cards through our secure payment
              processor, Paddle.
            </p>
          </div>
          <div className="bg-muted/50 rounded-xl p-6">
            <h3 className="text-base font-semibold text-foreground m-0 mb-3">Is there a free trial?</h3>
            <p className="text-[0.9375rem] text-muted-foreground m-0 leading-relaxed">
              Yes! Premium plans come with a 60-day free trial. No credit card
              required to start.
            </p>
          </div>
          <div className="bg-muted/50 rounded-xl p-6">
            <h3 className="text-base font-semibold text-foreground m-0 mb-3">Can I cancel anytime?</h3>
            <p className="text-[0.9375rem] text-muted-foreground m-0 leading-relaxed">
              Absolutely. You can cancel your subscription at any time with no
              penalty.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
