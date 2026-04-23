import { useUser } from '@clerk/react-router'
import {
  useGetCurrentSubscription,
  useGetSubscriptionTiers,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import type { SubscriptionTier } from '../../subscription/types'
import type { TierName } from '../../subscription/types'

export type PricingState =
  | { status: 'loading' }
  | { status: 'signed-out' }
  | { status: 'inactive' }
  | { status: 'trial'; tier: TierName; daysRemaining: number; trialEndsAt: string }
  | { status: 'trialing'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string }
  | { status: 'active'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string; cancelAtPeriodEnd: boolean; scheduledChangeAt?: string }
  | { status: 'cancelled'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string; cancelAtPeriodEnd: boolean; scheduledChangeAt?: string }
  | { status: 'past_due'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string }

export interface UsePricingContextResult {
  pricingState: PricingState
  tiers: Record<TierName, SubscriptionTier> | null
}

function getErrorStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined
  const err = error as { status?: number; response?: { status?: number } }
  return err.status ?? err.response?.status
}

export function usePricingContext(): UsePricingContextResult {
  const { isLoaded, isSignedIn } = useUser()

  const subscriptionQuery = useGetCurrentSubscription({
    query: {
      enabled: !!isSignedIn,
      retry: (failureCount: number, error: unknown) => {
        if (getErrorStatus(error) === 404) return false
        return failureCount < 1
      },
    },
  })

  const tiersQuery = useGetSubscriptionTiers({
    query: { staleTime: 24 * 60 * 60 * 1000 },
  })

  const tiers = tiersQuery.data?.tiers
    ? (tiersQuery.data.tiers as Record<TierName, SubscriptionTier>)
    : null

  if (!isLoaded || tiersQuery.isLoading) {
    return { pricingState: { status: 'loading' }, tiers }
  }

  if (!isSignedIn) {
    return { pricingState: { status: 'signed-out' }, tiers }
  }

  if (getErrorStatus(subscriptionQuery.error) === 404) {
    return { pricingState: { status: 'inactive' }, tiers }
  }

  if (subscriptionQuery.isLoading) {
    return { pricingState: { status: 'loading' }, tiers }
  }

  const sub = subscriptionQuery.data
  if (!sub) {
    // isLoading is false here — this means a non-404 error or empty response
    return { pricingState: { status: 'inactive' }, tiers }
  }

  const tier = (sub.tier as TierName) ?? 'premium'
  const billingInterval = (sub.billing_interval as 'month' | 'year') ?? 'month'

  switch (sub.status) {
    case 'trial':
      return {
        pricingState: {
          status: 'trial',
          tier,
          daysRemaining: sub.trial_days_remaining ?? 0,
          trialEndsAt: sub.trial_ends_at ?? '',
        },
        tiers,
      }

    case 'trialing':
      return {
        pricingState: {
          status: 'trialing',
          tier,
          billingInterval,
          subscriptionId: sub.subscription_id ?? '',
        },
        tiers,
      }

    case 'active':
      return {
        pricingState: {
          status: 'active',
          tier,
          billingInterval,
          subscriptionId: sub.subscription_id ?? '',
          cancelAtPeriodEnd: sub.cancel_at_period_end,
          scheduledChangeAt: sub.scheduled_change_at,
        },
        tiers,
      }

    case 'cancelled':
      return {
        pricingState: {
          status: 'cancelled',
          tier,
          billingInterval,
          subscriptionId: sub.subscription_id ?? '',
          cancelAtPeriodEnd: sub.cancel_at_period_end,
          scheduledChangeAt: sub.scheduled_change_at,
        },
        tiers,
      }

    case 'past_due':
      return {
        pricingState: {
          status: 'past_due',
          tier,
          billingInterval,
          subscriptionId: sub.subscription_id ?? '',
        },
        tiers,
      }

    default:
      return { pricingState: { status: 'loading' }, tiers }
  }
}
