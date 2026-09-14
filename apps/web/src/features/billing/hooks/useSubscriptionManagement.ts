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
import { useSubscriptionPresentation } from '../../subscription/hooks/useSubscriptionPresentation'
import { presentationFromSubscription } from '../../subscription/lib/subscriptionPresentation'
import { useAuth } from '@clerk/react-router'

interface UpdateOptions {
  newTier: string
  billingInterval: 'month' | 'year'
  currentBillingInterval?: 'month' | 'year'
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

export function useSubscriptionManagement() {
  const { userId } = useAuth()
  // Issue #1413: admin-comped users get real access with no Paddle subscription behind it —
  // OR the profile-derived presentation's isComped in as a resilience fallback, in case the
  // subscription endpoint hasn't been redeployed with the is_comped field yet (or 404s outright).
  const profilePresentation = useSubscriptionPresentation()

  // Query hooks
  const subscriptionQuery = useGetCurrentSubscription({
    query: {
      queryKey: ['/api/v1/subscription/current', userId],
      enabled: !!userId,
      retry: (failureCount: number, error: unknown) => {
        if (getErrorStatus(error) === 404) return false
        return failureCount < 1
      },
    },
  })

  const subPresentation = subscriptionQuery.data
    ? presentationFromSubscription(subscriptionQuery.data)
    : null
  const isComped = (subPresentation?.isComped ?? false) || profilePresentation.isComped
  // Profile is the last resort for the 404-fallback path (two-deploy resilience).
  const compEndsAt = subPresentation?.compEndsAt ?? profilePresentation.compEndsAt

  const billingPortalQuery = useGetBillingPortalUrl({
    query: {
      queryKey: ['/api/v1/subscription/portal', userId],
      enabled: !!subscriptionQuery.data?.subscription_id,
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
        current_billing_interval: options.currentBillingInterval,
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

  const noSubscription404 = getErrorStatus(subscriptionQuery.error) === 404
  // A comped user always has a real subscription (in the "you have access" sense), even when the
  // endpoint 404s because the backend hasn't caught up yet.
  const hasNoSubscription = noSubscription404 && !isComped

  return {
    // Current subscription data
    subscription: subscriptionQuery.data as GetCurrentSubscriptionOutputBody | undefined,
    isLoadingSubscription: subscriptionQuery.isLoading,
    subscriptionError: noSubscription404 ? null : normalizeError(subscriptionQuery.error),
    hasNoSubscription,
    isComped,
    compEndsAt,
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
