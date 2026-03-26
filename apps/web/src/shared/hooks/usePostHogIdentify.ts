import { useEffect } from 'react'
import { useUser } from '@clerk/react-router'
import { posthog } from '../lib/posthog'

/**
 * Identifies the current Clerk user in PostHog.
 * Calls posthog.identify() on sign-in and posthog.reset() on sign-out.
 */
export function usePostHogIdentify(): void {
  const { user } = useUser()

  useEffect(() => {
    if (user) {
      posthog.identify(user.id, {
        email: user.primaryEmailAddress?.emailAddress,
        name: user.fullName,
        subscription_tier: user.publicMetadata?.subscription_tier,
        primary_region: user.publicMetadata?.primary_region,
      })
    } else {
      posthog.reset()
    }
  }, [user])
}
