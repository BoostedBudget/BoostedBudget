import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useUser } from '@clerk/react-router'
import { useGetPrivacySettings } from '@boostedbudget/api-client/endpoints/privacy/privacy'
import { privacySettingsQueryKey } from '../privacy/privacyQueryKey'
import { resolveAnalyticsAuthority } from '../analytics/resolveAnalyticsAuthority'
import type { AnalyticsPurposes } from '../lib/analyticsLifecycle'
import {
  applyProductAnalyticsAuthority,
  getAnalyticsLifecycleSnapshot,
  listenForAnalyticsWithdrawal,
  setAnalyticsLifecyclePending,
  setProductAnalyticsPersonProperties,
  setProductAnalyticsScope,
  subscribeToAnalyticsLifecycle,
} from '../lib/posthog'

export interface AnalyticsLifecycle {
  apply: typeof applyProductAnalyticsAuthority
  getSnapshot: typeof getAnalyticsLifecycleSnapshot
  listenForWithdrawal: typeof listenForAnalyticsWithdrawal
  setPending: typeof setAnalyticsLifecyclePending
  setPersonProperties: typeof setProductAnalyticsPersonProperties
  setScope: typeof setProductAnalyticsScope
  subscribe: typeof subscribeToAnalyticsLifecycle
}

const defaultLifecycle: AnalyticsLifecycle = {
  apply: applyProductAnalyticsAuthority,
  getSnapshot: getAnalyticsLifecycleSnapshot,
  listenForWithdrawal: listenForAnalyticsWithdrawal,
  setPending: setAnalyticsLifecyclePending,
  setPersonProperties: setProductAnalyticsPersonProperties,
  setScope: setProductAnalyticsScope,
  subscribe: subscribeToAnalyticsLifecycle,
}

const NO_ANONYMOUS_PURPOSES: AnalyticsPurposes = {
  analytics: false,
  identifiedDiagnostics: false,
  interactionCapture: false,
}

/**
 * Resolves the only browser analytics authority. Signed-in users are governed
 * exclusively by their server privacy record; signed-out visitors are governed
 * exclusively by the anonymous-device cookie supplied by App.
 */
export function usePostHogIdentify(
  anonymousAnalyticsConsent: boolean | null,
  lifecycle: AnalyticsLifecycle = defaultLifecycle,
): void {
  const { user, isLoaded } = useUser()
  const userId = user?.id
  const appliedAuthority = useRef<string | null>(null)
  const [withdrawn, setWithdrawn] = useState(() => lifecycle.getSnapshot().withdrawn)
  const privacy = useGetPrivacySettings({
    query: {
      enabled: Boolean(userId),
      queryKey: privacySettingsQueryKey(userId),
      retry: false,
    },
  })

  useEffect(() => lifecycle.listenForWithdrawal(), [lifecycle])

  useEffect(
    () => lifecycle.subscribe(snapshot => setWithdrawn(snapshot.withdrawn)),
    [lifecycle],
  )

  useLayoutEffect(() => {
    if (!isLoaded) {
      lifecycle.setPending()
      return
    }

    const scope = userId ?? 'anonymous'
    lifecycle.setScope(scope)

    if (!userId) {
      const purposes: AnalyticsPurposes = {
        ...NO_ANONYMOUS_PURPOSES,
        analytics: anonymousAnalyticsConsent === true,
      }
      const authorityKey = JSON.stringify({ scope, purposes, withdrawn })
      if (appliedAuthority.current === authorityKey) return
      appliedAuthority.current = authorityKey
      void lifecycle.apply(resolveAnalyticsAuthority({ scope, purposes }))
      return
    }

    if (privacy.isLoading || privacy.isFetching) {
      lifecycle.setPending()
      appliedAuthority.current = null
      return
    }

    const granted = privacy.error == null && privacy.data?.gdpr_analytics_consent === true
    const purposes: AnalyticsPurposes = {
      analytics: granted,
      identifiedDiagnostics: privacy.data?.identified_diagnostics_enabled === true,
      interactionCapture: privacy.data?.gdpr_interaction_capture_consent === true,
    }
    const authorityKey = JSON.stringify({ scope, purposes, withdrawn })
    if (appliedAuthority.current === authorityKey) return
    appliedAuthority.current = authorityKey
    void lifecycle.apply(resolveAnalyticsAuthority({
      scope,
      purposes,
      distinctId: userId,
      firstName: user?.firstName,
      fullName: user?.fullName,
    }))
  }, [
    anonymousAnalyticsConsent,
    isLoaded,
    lifecycle,
    privacy.data,
    privacy.error,
    privacy.isFetching,
    privacy.isLoading,
    user,
    userId,
    withdrawn,
  ])
}
