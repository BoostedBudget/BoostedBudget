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

  const normalizeError = (error: unknown): SubscriptionError | null => {
    if (!error) return null

    const err = error as { status?: number; response?: { status?: number }; message?: string }
    const status = err.status || err.response?.status
    const message = err.message?.toLowerCase() || ''

    const errorMessages: Record<number, string> = {
      401: 'Please sign in to manage your subscription',
      403: 'You do not have permission to perform this action',
      400: 'Invalid subscription update request',
      500: 'Subscription service error. Please try again later.',
      503: 'Subscription service temporarily unavailable',
    }

    if (status && status in errorMessages) {
      return { status, message: errorMessages[status] }
    }

    if (message.includes('network') || message.includes('fetch')) {
      return { status: 0, message: 'Network error. Please check your connection.' }
    }

    return { status, message: err.message || 'Failed to update subscription. Please try again.' }
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
