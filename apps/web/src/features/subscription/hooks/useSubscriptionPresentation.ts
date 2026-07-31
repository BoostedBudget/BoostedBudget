/**
 * useSubscriptionPresentation Hook
 *
 * Issue #1413: Composes the existing `useProfile()` with `presentationFromUser` to give consumers
 * the single shared derivation of subscription status -> UI meaning (write access, the /pricing
 * redirect, billing nags, status badge) without each screen re-deriving "expired" from the raw
 * status string itself.
 */

import { useProfile } from '@/features/profile/hooks/useProfile'
import { getSubscriptionPresentation, presentationFromUser } from '../lib/subscriptionPresentation'
import type { SubscriptionPresentation } from '../lib/subscriptionPresentation'

export function useSubscriptionPresentation(): SubscriptionPresentation {
  const { user } = useProfile()

  // While the profile is still loading (or failed to load), fail closed: no write access rather
  // than briefly granting it based on stale/absent data.
  if (!user) {
    return getSubscriptionPresentation({ status: 'unknown' })
  }

  return presentationFromUser(user)
}
