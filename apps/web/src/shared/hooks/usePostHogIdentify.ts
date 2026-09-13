import { useEffect, useLayoutEffect, useRef } from 'react'
import { useUser } from '@clerk/react-router'
import { useGetPrivacySettings } from '@boostedbudget/api-client/endpoints/privacy/privacy'
import { useProfile } from '@/features/profile/hooks/useProfile'
import {
  applyProductAnalyticsAuthority,
  denyProductAnalytics,
  listenForAnalyticsWithdrawal,
  setProductAnalyticsScope,
  setProductAnalyticsPersonProperties,
  setAnalyticsLifecyclePending,
} from '../lib/posthog'

export interface AnalyticsLifecycle {
  applyAuthority: typeof applyProductAnalyticsAuthority
  deny: typeof denyProductAnalytics
  listenForWithdrawal: typeof listenForAnalyticsWithdrawal
  setScope: typeof setProductAnalyticsScope
  setPending: typeof setAnalyticsLifecyclePending
  setPersonProperties: typeof setProductAnalyticsPersonProperties
}

const defaultLifecycle: AnalyticsLifecycle = {
  applyAuthority: applyProductAnalyticsAuthority,
  deny: denyProductAnalytics,
  listenForWithdrawal: listenForAnalyticsWithdrawal,
  setScope: setProductAnalyticsScope,
  setPending: setAnalyticsLifecyclePending,
  setPersonProperties: setProductAnalyticsPersonProperties,
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
  const privacy = useGetPrivacySettings({
    query: {
      enabled: Boolean(userId),
      queryKey: ['/api/v1/user/privacy', userId ?? 'signed-out'],
      retry: false,
    },
  })
  const { user: profile, loading: profileLoading } = useProfile({
    enabled: Boolean(userId),
    userId,
  })

  useEffect(() => lifecycle.listenForWithdrawal(), [lifecycle])

  useLayoutEffect(() => {
    if (!isLoaded) {
      lifecycle.setPending()
      return
    }

    const scope = userId ?? 'anonymous'
    lifecycle.setScope(scope)

    if (!userId) {
      const authority = `anonymous:${anonymousAnalyticsConsent === true ? 'granted' : 'denied'}`
      if (appliedAuthority.current === authority) return
      appliedAuthority.current = authority
      if (anonymousAnalyticsConsent === true) lifecycle.applyAuthority(scope)
      else lifecycle.deny()
      return
    }

    if (privacy.isLoading || privacy.isFetching) {
      lifecycle.setPending()
      appliedAuthority.current = null
      return
    }

    const granted = privacy.error == null && privacy.data?.gdpr_analytics_consent === true
    const authority = `${userId}:${granted ? 'granted' : 'denied'}`
    if (appliedAuthority.current === authority) return
    appliedAuthority.current = authority

    if (!granted) {
      lifecycle.deny()
      return
    }

    lifecycle.applyAuthority(scope, userId, {
      subscription_tier: user.publicMetadata?.subscription_tier,
      primary_region: user.publicMetadata?.primary_region,
    })
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
  ])

  useEffect(() => {
    if (!userId || profileLoading || !profile) return
    lifecycle.setPersonProperties({
      subscription_tier: profile.effective_tier,
      subscription_status_effective: profile.effective_status,
      is_comped: profile.is_comped,
    })
  }, [
    lifecycle,
    privacy.data,
    privacy.error,
    privacy.isFetching,
    privacy.isLoading,
    profile,
    profileLoading,
    userId,
  ])
}
