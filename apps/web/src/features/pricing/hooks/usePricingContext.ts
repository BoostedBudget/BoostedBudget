import { useUser } from '@clerk/react-router'
import {
  useGetCurrentSubscription,
  useGetSubscriptionTiers,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { useProfile } from '@/features/profile/hooks/useProfile'
import { useSubscriptionPresentation } from '../../subscription/hooks/useSubscriptionPresentation'
import { presentationFromSubscription } from '../../subscription/lib/subscriptionPresentation'
import type { SubscriptionTier } from '../../subscription/types'
import type { TierName } from '../../subscription/types'

export type PricingState =
  | { status: 'loading' }
  | { status: 'signed-out' }
  | { status: 'inactive' }
  | { status: 'unresolved' }
  /**
   * Issue #1413: admin-comped users get real access with no Paddle subscription behind it.
   * `compEndsAt` is present for a time-boxed comp, absent for a permanent one.
   */
  | { status: 'comped'; tier: TierName; compEndsAt?: string }
  | { status: 'trial'; tier: TierName; daysRemaining: number; trialEndsAt: string }
  | { status: 'trialing'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string }
  | { status: 'active'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string; cancelAtPeriodEnd: boolean; scheduledChangeAt?: string }
  | { status: 'cancelled'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string; cancelAtPeriodEnd: boolean; scheduledChangeAt?: string; currentPeriodEnd?: string }
  | { status: 'past_due'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string }
  | { status: 'paused'; tier: TierName; billingInterval: 'month' | 'year'; subscriptionId: string }

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
  const { isLoaded, isSignedIn, user: clerkUser } = useUser()
  const { user } = useProfile()
  const profilePresentation = useSubscriptionPresentation()

  const subscriptionQuery = useGetCurrentSubscription({
    query: {
      queryKey: ['/api/v1/subscription/current', clerkUser?.id],
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

  // A comped user's tier, for the paths below where the subscription payload has none to read
  // (404, or empty response) — falls back to the profile's effective/raw tier.
  const compedFallbackTier = (): TierName =>
    ((user?.effective_tier ?? user?.subscription_tier) as TierName) ?? 'premium'

  // Compatibility fallback only for an explicit missing subscription response.
  const compedOrInactive = (): PricingState =>
    profilePresentation.isComped
      ? { status: 'comped', tier: compedFallbackTier(), compEndsAt: profilePresentation.compEndsAt }
      : { status: 'inactive' }

  if (getErrorStatus(subscriptionQuery.error) === 404) {
    // Resilience: if backend and frontend deploy separately, /subscription/current may still
    // 404 for a comped user. The profile query is already app-wide cached, so this costs nothing.
    return { pricingState: compedOrInactive(), tiers }
  }

  // A failed refetch can retain old React Query data. It is not current billing
  // evidence, even if that cached response previously allowed a purchase.
  if (subscriptionQuery.error) {
    return { pricingState: { status: 'unresolved' }, tiers }
  }

  if (subscriptionQuery.isLoading) {
    return { pricingState: { status: 'loading' }, tiers }
  }

  const sub = subscriptionQuery.data
  if (!sub) {
    // Unknown billing is not permission to start a second paid relationship.
    return { pricingState: { status: 'unresolved' }, tiers }
  }

  // Route the raw sub.status handling through the shared derivation rather than re-deriving
  // "comped" from the raw payload here — OR profile-derived isComped in too, in case the
  // subscription endpoint hasn't been redeployed with the is_comped field yet.
  const subPresentation = presentationFromSubscription(sub)
  if (!sub.subscription_id && (subPresentation.isComped || profilePresentation.isComped)) {
    return {
      pricingState: {
        status: 'comped',
        tier: (sub.tier as TierName) ?? compedFallbackTier(),
        // Profile is the last resort for the 404-fallback path above (two-deploy resilience).
        compEndsAt: subPresentation.compEndsAt ?? profilePresentation.compEndsAt,
      },
      tiers,
    }
  }

  const tier = (sub.tier as TierName) ?? 'premium'
  const billingInterval = (sub.billing_interval as 'month' | 'year') ?? 'month'

  switch (sub.status) {
    case 'inactive':
      return { pricingState: { status: 'inactive' }, tiers }
    case 'unresolved':
      return { pricingState: { status: 'unresolved' }, tiers }
    case 'paused':
      return { pricingState: { status: 'paused', tier, billingInterval, subscriptionId: sub.subscription_id ?? '' }, tiers }
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
          currentPeriodEnd: sub.current_period_end,
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
      return { pricingState: { status: 'unresolved' }, tiers }
  }
}
