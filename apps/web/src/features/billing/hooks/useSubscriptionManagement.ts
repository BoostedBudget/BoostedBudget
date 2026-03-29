import {
  useGetCurrentSubscription,
  useUpdatePaddleSubscription,
  useCancelPaddleSubscription,
  useGetBillingPortalUrl,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import type {
  UpdateSubscriptionInputBody,
  GetCurrentSubscriptionOutputBody,
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

function getErrorStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined

  const err = error as { response?: { status?: number }; status?: number }
  return err.status ?? err.response?.status
}

function is404Error(error: unknown): boolean {
  return getErrorStatus(error) === 404
}

export function useSubscriptionManagement() {
  // Query hooks
  const subscriptionQuery = useGetCurrentSubscription({
    query: {
      retry: (failureCount: number, error: unknown) => {
        if (is404Error(error)) return false
        return failureCount < 1
      },
    },
  })
  const billingPortalQuery = useGetBillingPortalUrl({
    query: {
      enabled:
        subscriptionQuery.data?.status === 'active' ||
        subscriptionQuery.data?.status === 'trialing',
    },
  })

  // Mutation hooks
  const updateMutation = useUpdatePaddleSubscription()
  const cancelMutation = useCancelPaddleSubscription()

  // Update subscription
  const updateSubscription = (options: UpdateOptions) =>
    updateMutation.mutateAsync({
      data: {
        new_tier: options.newTier,
        billing_interval: options.billingInterval,
        proration_billing_mode: options.prorationBillingMode,
      } as UpdateSubscriptionInputBody,
    })

  // Cancel subscription
  const cancelSubscription = () => cancelMutation.mutateAsync()

  const normalizeError = (error: unknown): SubscriptionError | null => {
    if (!error) return null

    const err = error as { message?: string }
    const status = getErrorStatus(error)
    const message = (err.message?.toLowerCase() ?? '').trim()

    const errorMessages: Record<number, string> = {
      401: 'Please sign in to manage your subscription',
      403: 'You do not have permission to perform this action',
      400: 'Invalid subscription update request',
      404: 'No active subscription found',
      500: 'Subscription service error. Please try again later.',
      503: 'Subscription service temporarily unavailable',
    }

    if (status && status in errorMessages) {
      return { status, message: errorMessages[status] }
    }

    if (message.includes('network') || message.includes('fetch')) {
      return { message: 'Network error. Please check your connection.' }
    }

    return { status, message: err.message || 'Failed to update subscription. Please try again.' }
  }

  const noSubscription = is404Error(subscriptionQuery.error)

  return {
    // Current subscription data
    subscription: subscriptionQuery.data as GetCurrentSubscriptionOutputBody | undefined,
    isLoadingSubscription: subscriptionQuery.isLoading,
    subscriptionError: noSubscription ? null : normalizeError(subscriptionQuery.error),
    hasNoSubscription: noSubscription,
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
