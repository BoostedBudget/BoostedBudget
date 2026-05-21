import { useEffect, useRef } from 'react'
import { useUser } from '@clerk/react-router'
import { posthog } from '../lib/posthog'
import { ANALYTICS_EVENTS } from '../analytics'

/**
 * Identifies the current Clerk user in PostHog.
 * Fires sign_in_completed / sign_up_completed on sign-in and
 * sign_out_completed on sign-out, using ref-based transition detection.
 */
export function usePostHogIdentify(): void {
  const { user } = useUser()
  const prevUserRef = useRef<typeof user>(undefined)

  useEffect(() => {
    const prevUser = prevUserRef.current
    prevUserRef.current = user

    if (!prevUser && user) {
      // Transition: signed out → signed in
      posthog.identify(user.id, {
        subscription_tier: user.publicMetadata?.subscription_tier,
        primary_region: user.publicMetadata?.primary_region,
      })
      posthog.capture(ANALYTICS_EVENTS.SIGN_IN_COMPLETED)

      // 60-second heuristic for sign-up detection: Clerk's user.createdAt timestamp
      // is compared against the current time to determine if this sign-in is part of
      // a new account activation. Chosen because most real sign-ups complete within
      // this window. Limitations: multi-step Clerk flows (email verification, MFA)
      // may take longer; client-server clock skew can cause false negatives.
      // This is an intentional trade-off (analytics, not billing) — silent misfire is acceptable.
      if (user.createdAt && Date.now() - user.createdAt.getTime() < 60_000) {
        posthog.capture(ANALYTICS_EVENTS.SIGN_UP_COMPLETED)
      }
    } else if (prevUser && !user) {
      // Transition: signed in → signed out
      posthog.capture(ANALYTICS_EVENTS.SIGN_OUT_COMPLETED)
      posthog.reset()
    }
  }, [user])
}
