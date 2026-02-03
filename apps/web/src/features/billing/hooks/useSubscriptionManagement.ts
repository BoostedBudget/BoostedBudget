import {
  useGetCurrentSubscription,
  useUpdatePaddleSubscription,
  useCancelPaddleSubscription,
  useGetBillingPortalUrl,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import type {
  UpdateSubscriptionInputBody,
  GetCurrentSubscriptionOutput,
} from '@boostedbudget/api-client/models'

interface UpdateOptions {
  newTier: string
  billingInterval: 'month' | 'year'
  prorationBillingMode?: 'prorated_immediately' | 'full_immediately' | 'full_next_billing_period'
}

interface SubscriptionError {
  status?: number
  message: string
}

export function useSubscriptionManagement() {
  // Query hooks
  const subscriptionQuery = useGetCurrentSubscription()
  const billingPortalQuery = useGetBillingPortalUrl()

  // Mutation hooks
  const updateMutation = useUpdatePaddleSubscription()
  const cancelMutation = useCancelPaddleSubscription()

  // Update subscription
  const updateSubscription = async (options: UpdateOptions) => {
    return updateMutation.mutateAsync({
      data: {
        new_tier: options.newTier,
        billing_interval: options.billingInterval,
        proration_billing_mode: options.prorationBillingMode,
      } as UpdateSubscriptionInputBody,
    })
  }

  // Cancel subscription
  const cancelSubscription = async () => {
    return cancelMutation.mutateAsync({})
  }

  // Error normalization
  const normalizeError = (error: unknown): SubscriptionError | null => {
    if (!error) return null

    const err = error as { status?: number; response?: { status?: number }; message?: string }
    const status = err.status || err.response?.status

    switch (status) {
      case 401:
        return { status, message: 'Please sign in to manage your subscription' }
      case 403:
        return { status, message: 'You do not have permission to perform this action' }
      case 400:
        return { status, message: 'Invalid subscription update request' }
      case 500:
        return { status, message: 'Subscription service error. Please try again later.' }
      case 503:
        return { status, message: 'Subscription service temporarily unavailable' }
      default: {
        const lowerMessage = err.message?.toLowerCase() || ''
        if (lowerMessage.includes('network') || lowerMessage.includes('fetch')) {
          return { status: 0, message: 'Network error. Please check your connection.' }
        }
        return {
          status,
          message: err.message || 'Failed to update subscription. Please try again.',
        }
      }
    }
  }

  return {
    // Current subscription data
    subscription: subscriptionQuery.data as GetCurrentSubscriptionOutput | undefined,
    isLoadingSubscription: subscriptionQuery.isLoading,
    subscriptionError: normalizeError(subscriptionQuery.error),
    refetchSubscription: subscriptionQuery.refetch,

    // Billing portal URL
    billingPortalUrl: billingPortalQuery.data?.portal_url,
    isLoadingBillingPortal: billingPortalQuery.isLoading,

    // Update subscription
    updateSubscription,
    isUpdating: updateMutation.isPending,
    isUpdateSuccess: updateMutation.isSuccess,
    updateError: normalizeError(updateMutation.error),
    resetUpdate: updateMutation.reset,

    // Cancel subscription
    cancelSubscription,
    isCanceling: cancelMutation.isPending,
    isCancelSuccess: cancelMutation.isSuccess,
    cancelError: normalizeError(cancelMutation.error),
    resetCancel: cancelMutation.reset,
  }
}
