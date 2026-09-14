import { useEffect, useLayoutEffect, useSyncExternalStore } from 'react'
import { useAuth } from '@clerk/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { revenueCatCheckout } from '../revenueCatRuntime'

export function useRevenueCatCheckout() {
  const { userId } = useAuth()
  const state = useSyncExternalStore(revenueCatCheckout.subscribe, revenueCatCheckout.getSnapshot)
  return {
    state,
    isLoading: state === 'loading' || state === 'open',
    checkout: () => {
      revenueCatCheckout.setIdentity(userId ?? null)
      return revenueCatCheckout.start()
    },
    retry: () => revenueCatCheckout.retry(),
  }
}

/** Mounted once in App, including logout and account-switch transitions. */
export function useRevenueCatIdentity() {
  const { userId } = useAuth()
  const state = useSyncExternalStore(revenueCatCheckout.subscribe, revenueCatCheckout.getSnapshot)
  const trackEvent = useTrackEvent()
  const queryClient = useQueryClient()
  useLayoutEffect(() => {
    revenueCatCheckout.setIdentity(userId ?? null)
    return () => revenueCatCheckout.setIdentity(null)
  }, [userId])
  useEffect(() => {
    if (state === 'open') trackEvent(ANALYTICS_EVENTS.CHECKOUT_STARTED)
    if (state === 'cancelled') trackEvent(ANALYTICS_EVENTS.CHECKOUT_CANCELLED)
    if (state === 'pending') trackEvent(ANALYTICS_EVENTS.CHECKOUT_CONFIRMATION_PENDING)
    if (state === 'confirmed') {
      trackEvent(ANALYTICS_EVENTS.CHECKOUT_COMPLETED)
      void queryClient.invalidateQueries()
    }
  }, [state, trackEvent, queryClient])
}
