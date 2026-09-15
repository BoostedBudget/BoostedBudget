import { useEffect } from 'react'
import { useNavigate } from 'react-router'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { CreditCard, Gift } from 'lucide-react'
import { useSubscriptionManagement } from './hooks/useSubscriptionManagement'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { BillingDetails } from './components/BillingDetails'
import { CurrentSubscriptionDisplay } from './components/CurrentSubscriptionDisplay'
import { BillingPortalButton } from './components/BillingPortalButton'
import { SubscriptionManagementSkeleton } from './components/SubscriptionManagementSkeleton'
import { SubscriptionErrorState } from './components/SubscriptionErrorState'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { Alert } from '@/components/ui/alert'
import { getSubscriptionPresentation } from '@/features/subscription/lib/subscriptionPresentation'
import type { GetCurrentSubscriptionOutputBody } from '@boostedbudget/api-client/models'
import type { SubscriptionTier } from '../subscription/types'
import { useGetCurrentSubscription } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { SubscriptionAuthorityDetails } from './components/SubscriptionAuthorityDetails'
import { RevenueCatCheckoutStatus } from './components/RevenueCatCheckoutStatus'
import { useRevenueCatCheckout } from './hooks/useRevenueCatCheckout'
import { useAuth } from '@clerk/react-router'

/**
 * Issue #1413: complimentary-access view for admin-comped users. Reuses CurrentSubscriptionDisplay
 * for the tier block when subscription data is available, but always suppresses BillingDetails —
 * a comped user has no next-bill date, amount, or payment method to show — and offers no Change
 * Plan / Cancel actions (there is nothing to change or cancel), only a link to /pricing.
 *
 * M11 fix: the backend deliberately preserves the *raw* Paddle status on `subscription.status`
 * for a comped user who also has a real (e.g. `past_due`) Paddle subscription behind them — that
 * raw value is meaningful for billing/admin surfaces, but must never be shown here as if it were
 * this user's access status (a comped user always has access, "Past Due" would be a contradictory,
 * alarming lie). So the status passed to CurrentSubscriptionDisplay is the shared
 * `getSubscriptionPresentation` derivation's `badgeLabel` (forced to the 'comped' kind, since this
 * component only ever renders for a comped user) rather than the raw status string.
 */
function ComplimentaryView({
  subscription,
  currentTierData,
  compEndsAt,
  formatFeatures,
  onViewPlans,
}: {
  subscription: GetCurrentSubscriptionOutputBody | undefined
  currentTierData: SubscriptionTier | undefined
  compEndsAt: string | undefined
  formatFeatures: (tierData: SubscriptionTier | undefined) => string[]
  onViewPlans: () => void
}) {
  const endDate = compEndsAt?.slice(0, 10)

  return (
    <div className="max-w-[800px] mx-auto p-8 space-y-8">
      <h1 className="text-3xl font-semibold">Manage Subscription</h1>

      {subscription && currentTierData && (
        <CurrentSubscriptionDisplay
          tier={subscription.tier}
          tierName={currentTierData.display_name}
          status={getSubscriptionPresentation({ status: subscription.status, isComped: true }).badgeLabel}
          billingInterval={subscription.billing_interval}
          amount={subscription.amount}
          currency={subscription.currency}
          features={formatFeatures(currentTierData)}
        />
      )}

      <Alert variant="info">
        <Gift className="h-4 w-4" aria-hidden="true" />
        <span>
          {endDate
            ? `Complimentary access ends ${endDate}.`
            : 'Complimentary Premium, provided by BoostedBudget — no end date.'}
        </span>
      </Alert>

      <Button onClick={onViewPlans} variant="outline" className="w-full">
        View Plans
      </Button>
    </div>
  )
}

/**
 * SubscriptionManagementPage - Complete subscription management interface
 * Issue #772: GAP 2 - Wire up subscription management components
 */
export function SubscriptionManagementPage() {
  const { userId } = useAuth()
  const { data, refetch, error } = useGetCurrentSubscription({ query: { queryKey: ['/api/v1/subscription/current', userId], enabled: !!userId } })
  const { retry, isLoading } = useRevenueCatCheckout()
  return <>
    <SubscriptionAuthorityDetails summary={data?.authority} />
    <div className="max-w-[800px] mx-auto px-8 pt-4">
      {data && error && <p role="status">Subscription details could not be refreshed. Showing the last observed details.</p>}
      <RevenueCatCheckoutStatus />
      <Button variant="outline" disabled={isLoading} onClick={async () => { await retry(); await refetch() }}>Refresh subscription status</Button>
    </div>
    <SubscriptionManagementContent />
  </>
}

function SubscriptionManagementContent() {
  const navigate = useNavigate()
  const trackEvent = useTrackEvent()
  const {
    subscription,
    isLoadingSubscription,
    subscriptionError,
    hasNoSubscription,
    isComped,
    compEndsAt,
    billingPortalUrl,
    isLoadingBillingPortal,
    billingPortalError,
    refetchSubscription,
  } = useSubscriptionManagement()

  const { tiers, loading: tiersLoading } = useSubscriptionTiers()

  // Track page view on mount
  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.SUBSCRIPTION_MANAGED)
  }, [trackEvent])

  if (isLoadingSubscription || tiersLoading) {
    return <SubscriptionManagementSkeleton />
  }

  if (subscriptionError && !subscription) {
    return (
      <SubscriptionErrorState
        message={subscriptionError.message}
        onRetry={() => refetchSubscription()}
      />
    )
  }

  // Helper to format tier features for display (defined before the branches below — both the
  // comped view and the regular subscription flows use it).
  const formatTierFeatures = (tierData: SubscriptionTier | undefined): string[] => {
    if (!tierData) return []

    const featureLabels: Record<string, string> = {
      priority_support: 'Priority support',
      advanced_reports: 'Advanced reports',
      api_access: 'API access',
      custom_categories: 'Custom categories',
      custom_tags: 'Custom tags',
    }

    return Object.entries(tierData.features)
      .filter(([, value]) => Boolean(value))
      .map(([key]) => featureLabels[key] || key.replace(/_/g, ' '))
  }

  // Issue #1413: admin-comped users get real access, even when the subscription endpoint 404s
  // (checked before hasNoSubscription/!subscription below — an isComped user should never see the
  // "No Active Subscription" empty state).
  if (isComped && !subscription?.subscription_id) {
    const compedTierData = subscription
      ? tiers?.tiers[subscription.tier as keyof typeof tiers.tiers]
      : undefined
    return (
      <ComplimentaryView
        subscription={subscription}
        currentTierData={compedTierData}
        compEndsAt={subscription?.comp_ends_at ?? compEndsAt}
        formatFeatures={formatTierFeatures}
        onViewPlans={() => navigate('/pricing')}
      />
    )
  }

  if (subscription?.status === 'unresolved') {
    return <SubscriptionErrorState message="Subscription status could not be confirmed. Refresh subscription status to try again." onRetry={() => refetchSubscription()} />
  }

  if (hasNoSubscription || !subscription || (!subscription.subscription_id && subscription.status !== 'trial')) {
    return (
      <div className="max-w-[800px] mx-auto p-8">
        <EmptyState
          icon={<CreditCard />}
          title="No Active Subscription"
          description="You don't have an active subscription yet. Choose a plan to get started."
          action={<Button onClick={() => navigate('/pricing')}>View Plans</Button>}
        />
      </div>
    )
  }

  const currentTierData = tiers?.tiers[subscription.tier as keyof typeof tiers.tiers]

  const nextBillingDate =
    subscription.next_billing_date ?? subscription.current_period_end ?? ''

  // #1652: the single self-service action. Plan changes, cancellation and payment-method updates
  // all happen in the provider's own portal — the in-app Paddle mutations are gone.
  const manageSubscription = (
    <BillingPortalButton
      billingPortalUrl={billingPortalUrl}
      isLoadingPortal={isLoadingBillingPortal}
      error={billingPortalError}
    />
  )

  // Render based on subscription status
  if (subscription.status === 'trial') {
    return (
      <div className="max-w-[800px] mx-auto p-8 space-y-8">
        <div className="rounded-lg bg-warning/10 border border-warning/30 px-4 py-3 text-center">
          <h2 className="font-semibold text-warning">Free Trial Active</h2>
          <p className="text-sm text-warning">
            You have {subscription.trial_days_remaining} days remaining in your trial.
            Subscribe to continue using BoostedBudget after your trial ends.
          </p>
        </div>

        {currentTierData && (
          <CurrentSubscriptionDisplay
            tier={subscription.tier}
            tierName={currentTierData.display_name}
            status={subscription.status}
            billingInterval={subscription.billing_interval}
            amount={subscription.amount}
            currency={subscription.currency}
            features={formatTierFeatures(currentTierData)}
          />
        )}

        <div>
          <Button onClick={() => navigate('/pricing')} className="w-full">
            Choose a Plan
          </Button>
        </div>
      </div>
    )
  }

  // Handle trialing status (subscription purchased during trial)
  if (subscription.status === 'trialing' && subscription.subscription_id) {
    // User has subscribed but subscription hasn't activated yet (still in trial period)
    const actualAmount = currentTierData?.pricing?.price_monthly
      ? currentTierData.pricing.price_monthly.toFixed(2)
      : subscription.amount
    const trialEndDate = subscription.trial_ends_at ? new Date(subscription.trial_ends_at).toLocaleDateString() : 'soon'
    const billingPeriod = subscription.billing_interval === 'year' ? 'year' : 'month'

    return (
      <div className="max-w-[800px] mx-auto p-8 space-y-8">
        <div className="rounded-lg bg-success/10 border border-success/30 px-4 py-3 text-center">
          <h2 className="font-semibold text-success">You're Subscribed!</h2>
          <p className="text-sm text-success">
            Your {currentTierData?.display_name || subscription.tier} subscription will start when your trial ends.
            You have {subscription.trial_days_remaining} days remaining in your trial.
          </p>
        </div>

        {currentTierData && (
          <CurrentSubscriptionDisplay
            tier={subscription.tier}
            tierName={currentTierData.display_name}
            status="Subscribed"
            billingInterval={subscription.billing_interval}
            amount={actualAmount}
            currency={subscription.currency}
            features={formatTierFeatures(currentTierData)}
          />
        )}

        <div className="rounded-lg bg-card p-6 shadow-sm space-y-2">
          <p className="text-sm text-muted-foreground">Your plan will automatically activate after your trial ends.</p>
          <p className="text-sm text-muted-foreground">You'll be charged {subscription.currency} {actualAmount}/{billingPeriod} starting on {trialEndDate}.</p>
        </div>

        {manageSubscription}
      </div>
    )
  }

  if (subscription.status === 'cancelled') {
    return (
      <div className="max-w-[800px] mx-auto p-8 space-y-8">
        <div className="rounded-lg bg-destructive/10 border border-destructive/30 px-4 py-3 text-center">
          <h2 className="font-semibold text-destructive">Subscription Cancelled</h2>
          <p className="text-sm text-destructive">
            Your subscription has been cancelled.
            {subscription.current_period_end && (
              <> You retain access until <strong>{subscription.current_period_end}</strong>.</>
            )}
          </p>
        </div>

        {manageSubscription}
      </div>
    )
  }

  if (subscription.status === 'past_due') {
    return (
      <div className="max-w-[800px] mx-auto p-8 space-y-8">
        <div className="rounded-lg bg-destructive/10 border border-destructive/30 px-4 py-3 text-center">
          <h2 className="font-semibold text-destructive">Payment Past Due</h2>
          <p className="text-sm text-destructive">Your payment is past due. Please update your payment method to continue using BoostedBudget.</p>
        </div>

        {manageSubscription}
      </div>
    )
  }

  // Active subscription
  return (
    <div className="max-w-[800px] mx-auto p-8 space-y-8">
      <h1 className="text-3xl font-semibold">Manage Subscription</h1>

      {currentTierData && (
        <CurrentSubscriptionDisplay
          tier={subscription.tier}
          tierName={currentTierData.display_name}
          status={subscription.status}
          billingInterval={subscription.billing_interval}
          amount={subscription.amount}
          currency={subscription.currency}
          features={formatTierFeatures(currentTierData)}
        />
      )}

      <BillingDetails
        nextBillingDate={nextBillingDate}
        amount={subscription.amount}
        currency={subscription.currency}
        billingInterval={subscription.billing_interval}
        portalUrl={billingPortalUrl}
      />

      {manageSubscription}
    </div>
  )
}
