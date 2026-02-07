import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useSubscriptionManagement } from './hooks/useSubscriptionManagement'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { BillingDetails } from './components/BillingDetails'
import { CurrentSubscriptionDisplay } from './components/CurrentSubscriptionDisplay'
import { UpgradeModal } from './components/UpgradeModal'
import { DowngradeModal } from './components/DowngradeModal'
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
  const [showUpgradeModal, setShowUpgradeModal] = useState(false)
  const [showDowngradeModal, setShowDowngradeModal] = useState(false)
  const [showCancelModal, setShowCancelModal] = useState(false)
  const [selectedTargetTier, setSelectedTargetTier] = useState<string | null>(null)

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
  const availableTiers = tiers?.tiers ? Object.entries(tiers.tiers).map(([name, tier]) => ({ name, ...tier })) : []

  // Helper to format tier features for display
  const formatTierFeatures = (tierData: typeof currentTierData) =>
    tierData ? Object.entries(tierData.features).map(([key, value]) => ({
      name: key.replace(/_/g, ' '),
      enabled: Boolean(value),
    })) : []

  const handleUpgrade = async (targetTier: string, billingInterval: 'month' | 'year') => {
    try {
      await updateSubscription({
        newTier: targetTier,
        billingInterval,
        prorationBillingMode: 'prorated_immediately',
      })
      toast.success('Subscription upgraded successfully!')
      setShowUpgradeModal(false)
      refetchSubscription()
    } catch {
      // Error handled by useEffect
    }
  }

  const handleDowngrade = async (targetTier: string) => {
    try {
      await updateSubscription({
        newTier: targetTier,
        billingInterval: subscription.billing_interval as 'month' | 'year',
        prorationBillingMode: 'full_next_billing_period',
      })
      toast.success('Subscription will downgrade at the end of your billing period')
      setShowDowngradeModal(false)
      refetchSubscription()
    } catch {
      // Error handled by useEffect
    }
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

  if (subscription.status === 'cancelled' || subscription.status === 'canceled') {
    return (
      <div className="subscription-management-page">
        <div className="cancelled-banner">
          <h2>Subscription Cancelled</h2>
          <p>Your subscription has been cancelled. You can resubscribe at any time.</p>
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
          onClick={() => setShowUpgradeModal(true)}
          className="btn-secondary"
          disabled={isUpdating}
        >
          Upgrade Plan
        </button>

        <button
          onClick={() => setShowDowngradeModal(true)}
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

      {/* Upgrade Modal */}
      {showUpgradeModal && currentTierData && (
        <UpgradeModal
          open={showUpgradeModal}
          currentTier={{
            name: subscription.tier,
            display_name: currentTierData.display_name,
            pricing: currentTierData.pricing,
            features: currentTierData.features,
            limits: currentTierData.limits,
          }}
          availableTiers={availableTiers.map(tier => ({
            name: tier.name,
            display_name: tier.display_name,
            pricing: tier.pricing,
            features: tier.features,
            limits: tier.limits,
          }))}
          onUpgrade={(tier, interval) => handleUpgrade(tier, interval)}
          onClose={() => setShowUpgradeModal(false)}
        />
      )}

      {/* Downgrade Modal */}
      {showDowngradeModal && currentTierData && selectedTargetTier && (
        <DowngradeModal
          open={showDowngradeModal}
          currentTier={{
            name: subscription.tier,
            display_name: currentTierData.display_name,
            features: currentTierData.features,
          }}
          downgradeTier={{
            name: selectedTargetTier,
            display_name: tiers?.tiers[selectedTargetTier as keyof typeof tiers.tiers]?.display_name || selectedTargetTier,
            features: tiers?.tiers[selectedTargetTier as keyof typeof tiers.tiers]?.features || {},
          }}
          nextBillingDate={subscription.next_billing_date || subscription.current_period_end}
          onDowngrade={(tier) => handleDowngrade(tier)}
          onClose={() => {
            setShowDowngradeModal(false)
            setSelectedTargetTier(null)
          }}
        />
      )}

      {/* Cancel Modal */}
      {showCancelModal && (
        <CancelConfirmationModal
          open={showCancelModal}
          subscription={{
            tier: subscription.tier,
            billing_interval: subscription.billing_interval,
            amount: subscription.amount,
            currency: subscription.currency,
            next_billing_date: subscription.next_billing_date || subscription.current_period_end,
          }}
          onConfirm={handleCancel}
          onClose={() => setShowCancelModal(false)}
        />
      )}
    </div>
  )
}
