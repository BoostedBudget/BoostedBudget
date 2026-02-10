import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useSubscriptionManagement } from './hooks/useSubscriptionManagement'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { BillingDetails } from './components/BillingDetails'
import { CurrentSubscriptionDisplay } from './components/CurrentSubscriptionDisplay'
import { PlanChangeModal } from './components/PlanChangeModal'
import { CancelConfirmationModal } from './components/CancelConfirmationModal'
import { toast } from 'sonner'
import './SubscriptionManagementPage.css'

/**
 * SubscriptionManagementPage - Complete subscription management interface
 * Issue #772: GAP 2 - Wire up subscription management components
 */
export function SubscriptionManagementPage() {
  const navigate = useNavigate()
  const {
    subscription,
    isLoadingSubscription,
    subscriptionError,
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

  // Modal state
  const [showPlanChangeModal, setShowPlanChangeModal] = useState(false)
  const [showCancelModal, setShowCancelModal] = useState(false)

  // Handle subscription errors
  useEffect(() => {
    if (updateError) toast.error(updateError.message)
    if (cancelError) toast.error(cancelError.message)
  }, [updateError, cancelError])

  if (isLoadingSubscription || tiersLoading) {
    return (
      <div className="subscription-management-page">
        <div className="loading-state">Loading subscription details...</div>
      </div>
    )
  }

  if (subscriptionError) {
    return (
      <div className="subscription-management-page">
        <div className="error-state">
          <h2>Failed to load subscription</h2>
          <p>{subscriptionError.message}</p>
          <button onClick={() => refetchSubscription()} className="btn-retry">
            Try Again
          </button>
        </div>
      </div>
    )
  }

  if (!subscription) {
    return (
      <div className="subscription-management-page">
        <div className="no-subscription-state">
          <h2>No Active Subscription</h2>
          <p>You don't have an active subscription yet.</p>
          <button onClick={() => navigate('/pricing')} className="btn-primary">
            View Plans
          </button>
        </div>
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
      <div className="subscription-management-page">
        <div className="trial-banner">
          <h2>Free Trial Active</h2>
          <p>
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

        <div className="trial-actions">
          <button onClick={() => navigate('/pricing')} className="btn-primary">
            Choose a Plan
          </button>
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
      <div className="subscription-management-page">
        <div className="trial-banner success">
          <h2>You're Subscribed!</h2>
          <p>
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

        <div className="subscription-info">
          <p>Your plan will automatically activate after your trial ends.</p>
          <p>You'll be charged {subscription.currency} {actualAmount}/{billingPeriod} starting on {trialEndDate}.</p>
        </div>

        <div className="subscription-actions">
          <button
            onClick={() => setShowPlanChangeModal(true)}
            className="btn-secondary"
            disabled={isUpdating}
          >
            Change Plan
          </button>

          <button
            onClick={() => setShowCancelModal(true)}
            className="btn-danger"
            disabled={isCanceling}
          >
            Cancel Subscription
          </button>

          {billingPortalUrl && (
            <a href={billingPortalUrl} className="btn-secondary" target="_blank" rel="noopener noreferrer">
              Manage Billing
            </a>
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
      <div className="subscription-management-page">
        <div className="cancelled-banner">
          <h2>Subscription Cancelled</h2>
          <p>
            Your subscription has been cancelled.
            {subscription.current_period_end && (
              <> You retain access until <strong>{subscription.current_period_end}</strong>.</>
            )}
          </p>
        </div>

        <div className="cancelled-actions">
          <button onClick={() => navigate('/pricing')} className="btn-primary">
            Resubscribe
          </button>
        </div>
      </div>
    )
  }

  if (subscription.status === 'past_due') {
    return (
      <div className="subscription-management-page">
        <div className="past-due-banner">
          <h2>Payment Past Due</h2>
          <p>Your payment is past due. Please update your payment method to continue using BoostedBudget.</p>
        </div>

        {billingPortalUrl && (
          <div className="past-due-actions">
            <a href={billingPortalUrl} className="btn-primary" target="_blank" rel="noopener noreferrer">
              Update Payment Method
            </a>
          </div>
        )}
      </div>
    )
  }

  // Active subscription
  return (
    <div className="subscription-management-page">
      <h1>Manage Subscription</h1>

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

      <div className="subscription-actions">
        <button
          onClick={() => setShowPlanChangeModal(true)}
          className="btn-secondary"
          disabled={isUpdating}
        >
          Change Plan
        </button>

        <button
          onClick={() => setShowCancelModal(true)}
          className="btn-danger"
          disabled={isCanceling}
        >
          Cancel Subscription
        </button>
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
