import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { CreditCard } from 'lucide-react'
import { useSubscriptionManagement } from './hooks/useSubscriptionManagement'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { BillingDetails } from './components/BillingDetails'
import { CurrentSubscriptionDisplay } from './components/CurrentSubscriptionDisplay'
import { PlanChangeModal } from './components/PlanChangeModal'
import { CancelConfirmationModal } from './components/CancelConfirmationModal'
import { SubscriptionManagementSkeleton } from './components/SubscriptionManagementSkeleton'
import { SubscriptionErrorState } from './components/SubscriptionErrorState'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { toast } from 'sonner'

/**
 * SubscriptionManagementPage - Complete subscription management interface
 * Issue #772: GAP 2 - Wire up subscription management components
 */
export function SubscriptionManagementPage() {
  const navigate = useNavigate()
  const trackEvent = useTrackEvent()
  const {
    subscription,
    isLoadingSubscription,
    subscriptionError,
    hasNoSubscription,
    billingPortalUrl,
    updateSubscription,
    isUpdating,
    updateError,
    cancelSubscription,
    isCanceling,
    cancelError,
    refetchSubscription,
  } = useSubscriptionManagement()

  const { tiers, loading: tiersLoading } = useSubscriptionTiers()

  // Track page view on mount
  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.SUBSCRIPTION_MANAGED)
  }, [trackEvent])

  // Modal state
  const [showPlanChangeModal, setShowPlanChangeModal] = useState(false)
  const [showCancelModal, setShowCancelModal] = useState(false)

  // Handle subscription errors
  useEffect(() => {
    if (updateError) toast.error(updateError.message)
    if (cancelError) toast.error(cancelError.message)
  }, [updateError, cancelError])

  if (isLoadingSubscription || tiersLoading) {
    return <SubscriptionManagementSkeleton />
  }

  if (subscriptionError) {
    return (
      <SubscriptionErrorState
        message={subscriptionError.message}
        onRetry={() => refetchSubscription()}
      />
    )
  }

  if (hasNoSubscription || !subscription) {
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

  // Helper to format tier features for display
  const formatTierFeatures = (tierData: typeof currentTierData): string[] => {
    if (!tierData) return []

    const featureLabels: Record<string, string> = {
      ads_enabled: 'Ad-free experience',
      ai_insights: 'AI-powered insights',
      priority_support: 'Priority support',
      advanced_reports: 'Advanced reports',
      api_access: 'API access',
      custom_categories: 'Custom categories',
      custom_tags: 'Custom tags',
    }

    return Object.entries(tierData.features)
      .filter(([key, value]) => key === 'ads_enabled' ? !value : Boolean(value))
      .map(([key]) => featureLabels[key] || key.replace(/_/g, ' '))
  }

  const handleUpdateSubscription = async (
    targetTier: string,
    billingInterval: 'month' | 'year',
    prorationBillingMode: 'prorated_immediately' | 'full_immediately' | 'full_next_billing_period',
    message: string,
    closeModal: () => void,
  ) => {
    try {
      await updateSubscription({
        newTier: targetTier,
        billingInterval,
        prorationBillingMode,
      })
      toast.success(message)
      closeModal()
      refetchSubscription()
    } catch {
      // Error handled by useEffect
    }
  }

  const handlePlanChange = (
    targetTier: string,
    billingInterval: 'month' | 'year',
    prorationBillingMode: 'prorated_immediately' | 'full_next_billing_period',
  ) => {
    const message = prorationBillingMode === 'prorated_immediately'
      ? 'Subscription upgraded successfully!'
      : 'Subscription will change at the end of your billing period'
    return handleUpdateSubscription(
      targetTier,
      billingInterval,
      prorationBillingMode,
      message,
      () => setShowPlanChangeModal(false),
    )
  }

  const handleCancel = async () => {
    try {
      await cancelSubscription()
      toast.success('Subscription cancelled successfully')
      setShowCancelModal(false)
      refetchSubscription()
    } catch {
      // Error handled by useEffect
    }
  }

  const handleOpenPlanChangeModal = () => {
    trackEvent(ANALYTICS_EVENTS.CHECKOUT_STARTED)
    setShowPlanChangeModal(true)
  }

  // Helper to build subscription data for modals
  const getModalSubscriptionData = () => ({
    tier: subscription.tier,
    status: subscription.status,
    billing_interval: subscription.billing_interval,
    amount: subscription.amount,
    currency: subscription.currency,
    next_billing_date: subscription.next_billing_date || subscription.current_period_end,
    features: formatTierFeatures(currentTierData),
  })

  // Render based on subscription status
  if (subscription.status === 'trial') {
    return (
      <div className="max-w-[800px] mx-auto p-8 space-y-8">
        <div className="rounded-lg bg-amber-50 dark:bg-amber-900/20 px-4 py-3 text-center">
          <h2 className="font-semibold text-amber-900 dark:text-amber-200">Free Trial Active</h2>
          <p className="text-sm text-amber-800 dark:text-amber-300">
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
        <div className="rounded-lg bg-emerald-50 dark:bg-emerald-900/20 px-4 py-3 text-center">
          <h2 className="font-semibold text-emerald-900 dark:text-emerald-200">You're Subscribed!</h2>
          <p className="text-sm text-emerald-800 dark:text-emerald-300">
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

        <div className="rounded-lg bg-white dark:bg-gray-800 p-6 shadow-sm space-y-2">
          <p className="text-sm text-gray-600 dark:text-gray-400">Your plan will automatically activate after your trial ends.</p>
          <p className="text-sm text-gray-600 dark:text-gray-400">You'll be charged {subscription.currency} {actualAmount}/{billingPeriod} starting on {trialEndDate}.</p>
        </div>

        <div className="space-y-3">
          <Button
            onClick={handleOpenPlanChangeModal}
            disabled={isUpdating}
            variant="outline"
            className="w-full"
          >
            Change Plan
          </Button>

          <Button
            onClick={() => setShowCancelModal(true)}
            disabled={isCanceling}
            variant="destructive"
            className="w-full"
          >
            Cancel Subscription
          </Button>

          {billingPortalUrl && (
            <Button
              asChild
              variant="outline"
              className="w-full"
            >
              <a href={billingPortalUrl} target="_blank" rel="noopener noreferrer">
                Manage Billing
              </a>
            </Button>
          )}
        </div>

        {/* Plan Change Modal */}
        {showPlanChangeModal && currentTierData && tiers?.tiers && (
          <PlanChangeModal
            open={showPlanChangeModal}
            currentTier={subscription.tier}
            currentBillingInterval={subscription.billing_interval as 'month' | 'year'}
            tiers={tiers.tiers}
            isUpdating={isUpdating}
            onChangePlan={handlePlanChange}
            onClose={() => setShowPlanChangeModal(false)}
          />
        )}

        {/* Cancel Modal */}
        {showCancelModal && currentTierData && (
          <CancelConfirmationModal
            open={showCancelModal}
            subscription={getModalSubscriptionData()}
            onConfirm={handleCancel}
            onClose={() => setShowCancelModal(false)}
          />
        )}
      </div>
    )
  }

  if (subscription.status === 'cancelled') {
    return (
      <div className="max-w-[800px] mx-auto p-8 space-y-8">
        <div className="rounded-lg bg-red-50 dark:bg-red-900/20 px-4 py-3 text-center">
          <h2 className="font-semibold text-red-900 dark:text-red-200">Subscription Cancelled</h2>
          <p className="text-sm text-red-800 dark:text-red-300">
            Your subscription has been cancelled.
            {subscription.current_period_end && (
              <> You retain access until <strong>{subscription.current_period_end}</strong>.</>
            )}
          </p>
        </div>

        <Button onClick={() => navigate('/pricing')} className="w-full">
          Resubscribe
        </Button>
      </div>
    )
  }

  if (subscription.status === 'past_due') {
    return (
      <div className="max-w-[800px] mx-auto p-8 space-y-8">
        <div className="rounded-lg bg-red-50 dark:bg-red-900/20 px-4 py-3 text-center">
          <h2 className="font-semibold text-red-900 dark:text-red-200">Payment Past Due</h2>
          <p className="text-sm text-red-800 dark:text-red-300">Your payment is past due. Please update your payment method to continue using BoostedBudget.</p>
        </div>

        {billingPortalUrl && (
          <Button
            asChild
            className="w-full"
          >
            <a href={billingPortalUrl} target="_blank" rel="noopener noreferrer">
              Update Payment Method
            </a>
          </Button>
        )}
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
        nextBillingDate={subscription.next_billing_date || subscription.current_period_end}
        amount={subscription.amount}
        currency={subscription.currency}
        billingInterval={subscription.billing_interval}
        portalUrl={billingPortalUrl}
      />

      <div className="space-y-3">
        <Button
          onClick={handleOpenPlanChangeModal}
          disabled={isUpdating}
          variant="outline"
          className="w-full"
        >
          Change Plan
        </Button>

        <Button
          onClick={() => setShowCancelModal(true)}
          disabled={isCanceling}
          variant="destructive"
          className="w-full"
        >
          Cancel Subscription
        </Button>
      </div>

      {/* Plan Change Modal */}
      {showPlanChangeModal && currentTierData && tiers?.tiers && (
        <PlanChangeModal
          open={showPlanChangeModal}
          currentTier={subscription.tier}
          currentBillingInterval={subscription.billing_interval as 'month' | 'year'}
          tiers={tiers.tiers}
          isUpdating={isUpdating}
          onChangePlan={handlePlanChange}
          onClose={() => setShowPlanChangeModal(false)}
        />
      )}

      {/* Cancel Modal */}
      {showCancelModal && currentTierData && (
        <CancelConfirmationModal
          open={showCancelModal}
          subscription={getModalSubscriptionData()}
          onConfirm={handleCancel}
          onClose={() => setShowCancelModal(false)}
        />
      )}
    </div>
  )
}
