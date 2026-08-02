import { useEffect, useRef } from 'react'
import { useUser } from '@clerk/react-router'
import { posthog } from '../lib/posthog'
import { ANALYTICS_EVENTS } from '../analytics'
import { useProfile } from '@/features/profile/hooks/useProfile'

/**
 * Identifies the current Clerk user in PostHog.
 * Fires sign_in_completed / sign_up_completed on sign-in and
 * sign_out_completed on sign-out, using ref-based transition detection.
 *
 * Issue #1413: Also syncs server-authoritative subscription fields (effective tier,
 * effective status, comp flag) as PostHog person properties once the profile query
 * resolves. This is a *second*, independent effect — the identify() call above must
 * keep firing immediately on the Clerk sign-in transition (it anchors sign_in_completed
 * timing), so it is left untouched. Clerk's publicMetadata.subscription_tier lags the
 * database and carries no comp signal at all, so every tier-segmented funnel was
 * misreporting comped users before this sync existed.
 */
export function usePostHogIdentify(): void {
  const { user } = useUser()
  const prevUserRef = useRef<typeof user>(undefined)
  // Gated on a signed-in Clerk user: this hook is mounted at the App root, so it also runs on
  // public routes (landing, pricing). Ungated, it would fire GET /api/v1/user/profile on every
  // anonymous page view and take a 401 — the profile was previously only fetched behind
  // PremiumRoute. The person-property sync below has nothing to report when signed out anyway.
  const { user: profile, loading: profileLoading } = useProfile({ enabled: !!user })

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

  // Issue #1413: Sync server-authoritative tier/status/comp fields whenever the
  // profile resolves or changes. Independent of the sign-in transition effect above
  // by design — it must not gate or delay the identify() call's timing, and it should
  // stay in sync (e.g. an admin comp applied mid-session) rather than firing once.
  useEffect(() => {
    if (!user) return // signed out — nothing to sync
    if (profileLoading || !profile) return // profile query hasn't resolved yet

    posthog.setPersonProperties({
      subscription_tier: profile.effective_tier,
      subscription_status_effective: profile.effective_status,
      is_comped: profile.is_comped,
    })
  }, [user, profile, profileLoading])
}
