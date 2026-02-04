import { useState } from 'react';
import {
  useGetCurrentSubscription,
  useUpdatePaddleSubscription,
  useCancelPaddleSubscription,
  useGetBillingPortalUrl,
} from '@boostedbudget/api-client/endpoints/subscription/subscription';
import './SubscriptionManagementPage.css';

type NotificationType = 'success' | 'error';

interface Notification {
  type: NotificationType;
  message: string;
}

export function SubscriptionManagementPage() {
  const [showCancelModal, setShowCancelModal] = useState(false);
  const [notification, setNotification] = useState<Notification | null>(null);

  const { data: subscription, isLoading, error, refetch } = useGetCurrentSubscription();
  const { data: portalData } = useGetBillingPortalUrl();

  const { mutateAsync: updateSubscription, isPending: isUpdating } = useUpdatePaddleSubscription();
  const { mutateAsync: cancelSubscription, isPending: isCancelling } = useCancelPaddleSubscription();

  const showNotification = (type: NotificationType, message: string) => {
    setNotification({ type, message });
    setTimeout(() => setNotification(null), 3000);
  };

  const handleUpgrade = async (newTier: string) => {
    try {
      await updateSubscription({
        data: {
          new_tier: newTier,
          billing_interval: 'month',
          proration_billing_mode: 'prorated_immediately',
        },
      });
      showNotification('success', 'Subscription updated successfully');
      refetch();
    } catch {
      showNotification('error', 'Failed to upgrade subscription');
    }
  };

  const handleCancel = async () => {
    try {
      await cancelSubscription({});
      setShowCancelModal(false);
      showNotification('success', 'Subscription cancelled successfully');
      refetch();
    } catch {
      showNotification('error', 'Failed to cancel subscription');
    }
  };

  if (isLoading) {
    return (
      <div className="subscription-management-page">
        <div className="loading">Loading subscription details...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="subscription-management-page">
        <div className="error">Failed to load subscription details</div>
      </div>
    );
  }

  return (
    <div className="subscription-management-page">
      <h1>Subscription Management</h1>

      {/* Notification Toast */}
      {notification && (
        <div className={`notification notification-${notification.type}`}>
          {notification.message}
        </div>
      )}

      {/* Current Subscription Card */}
      <section className="current-subscription">
        <h2>Current Subscription</h2>
        {subscription && (
          <div className="subscription-details">
            <div className="detail-row">
              <span className="label">Tier:</span>
              <span className="value">{subscription.tier}</span>
            </div>
            <div className="detail-row">
              <span className="label">Status:</span>
              <span className={`value status-${subscription.status}`}>
                {subscription.status}
              </span>
            </div>
            <div className="detail-row">
              <span className="label">Billing Interval:</span>
              <span className="value">{subscription.billing_interval}</span>
            </div>
            <div className="detail-row">
              <span className="label">Amount:</span>
              <span className="value">
                {subscription.currency} {subscription.amount}
              </span>
            </div>
            <div className="detail-row">
              <span className="label">Next Billing Date:</span>
              <span className="value">{subscription.next_billing_date}</span>
            </div>
          </div>
        )}
      </section>

      {/* Actions */}
      <section className="subscription-actions">
        <h2>Actions</h2>
        <div className="action-buttons">
          <button
            onClick={() => handleUpgrade('household_2')}
            disabled={isUpdating}
            className="btn btn-primary"
          >
            {isUpdating ? 'Upgrading...' : 'Upgrade to Household 2'}
          </button>

          <button
            onClick={() => setShowCancelModal(true)}
            disabled={isCancelling}
            className="btn btn-danger"
          >
            Cancel Subscription
          </button>

          {portalData?.portal_url && (
            <a
              href={portalData.portal_url}
              target="_blank"
              rel="noopener noreferrer"
              className="btn btn-secondary"
            >
              Manage Payment Methods
            </a>
          )}
        </div>
      </section>

      {/* Cancel Confirmation Modal */}
      {showCancelModal && (
        <div className="modal-overlay" onClick={() => setShowCancelModal(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>Cancel Subscription</h2>
            <p>
              Are you sure you want to cancel your subscription? You'll retain
              access until the end of your current billing period.
            </p>
            <div className="modal-actions">
              <button
                onClick={handleCancel}
                disabled={isCancelling}
                className="btn btn-danger"
              >
                {isCancelling ? 'Cancelling...' : 'Yes, Cancel'}
              </button>
              <button
                onClick={() => setShowCancelModal(false)}
                className="btn btn-secondary"
              >
                Keep Subscription
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
