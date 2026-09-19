import {
  useGetCurrentSubscription,
  useGetBillingPortalUrl,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import type {
  GetCurrentSubscriptionOutputBody,
} from '@boostedbudget/api-client/models'
import { useSubscriptionPresentation } from '../../subscription/hooks/useSubscriptionPresentation'
import { presentationFromSubscription } from '../../subscription/lib/subscriptionPresentation'
import { useAuth } from '@clerk/react-router'

interface SubscriptionError {
  status?: number
  message: string
}

function getErrorStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined

  const err = error as { response?: { status?: number }; status?: number }
  return err.status ?? err.response?.status
}

/**
 * Billing self-service is a single action: open the provider's own management portal.
 *
 * #1652 removed the in-app Paddle update/cancel/preview mutations — Paddle stays merchant of
 * record, so plan changes, cancellation and payment-method updates all happen in its portal. The
 * portal URL is resolved by the backend from RevenueCat's `management_url` (see
 * `GET /api/v1/subscription/billing-portal`), so this hook only reads it.
 */
export function useSubscriptionManagement() {
  const { userId } = useAuth()
  // Issue #1413: admin-comped users get real access with no Paddle subscription behind it —
  // OR the profile-derived presentation's isComped in as a resilience fallback, in case the
  // subscription endpoint hasn't been redeployed with the is_comped field yet (or 404s outright).
  const profilePresentation = useSubscriptionPresentation()

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

    return { status, message: err.message || 'Failed to load the billing portal. Please try again.' }
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

    // The single self-service action: the provider's resolved management portal.
    billingPortalUrl: billingPortalQuery.data?.portal_url,
    isLoadingBillingPortal: billingPortalQuery.isLoading,
    billingPortalError: normalizeError(billingPortalQuery.error),
  }
}
