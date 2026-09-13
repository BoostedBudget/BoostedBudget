import { useEffect, useRef } from 'react'
import { useUser } from '@clerk/react-router'
import { useGetPrivacySettings } from '@boostedbudget/api-client/endpoints/privacy/privacy'
import { useProfile } from '@/features/profile/hooks/useProfile'
import {
  denyProductAnalytics,
  grantProductAnalytics,
  listenForAnalyticsWithdrawal,
  setProductAnalyticsPersonProperties,
  setAnalyticsLifecyclePending,
} from '../lib/posthog'

export interface AnalyticsLifecycle {
  deny: typeof denyProductAnalytics
  grant: typeof grantProductAnalytics
  listenForWithdrawal: typeof listenForAnalyticsWithdrawal
  setPending: typeof setAnalyticsLifecyclePending
  setPersonProperties: typeof setProductAnalyticsPersonProperties
}

const defaultLifecycle: AnalyticsLifecycle = {
  deny: denyProductAnalytics,
  grant: grantProductAnalytics,
  listenForWithdrawal: listenForAnalyticsWithdrawal,
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
  const previousUserId = useRef<string | null | undefined>(undefined)
  const appliedAuthority = useRef<string | null>(null)
  const privacy = useGetPrivacySettings({
    query: {
      enabled: Boolean(userId),
      queryKey: ['/api/v1/user/privacy', userId ?? 'signed-out'],
      retry: false,
    },
  })
  const { user: profile, loading: profileLoading } = useProfile({ enabled: Boolean(userId) })

  useEffect(() => lifecycle.listenForWithdrawal(), [lifecycle])

  useEffect(() => {
    if (!isLoaded) {
      lifecycle.setPending()
      return
    }

    const previous = previousUserId.current
    const identityChanged = previous !== undefined && previous !== (userId ?? null)
    if (identityChanged) {
      lifecycle.deny()
      appliedAuthority.current = null
    }
    previousUserId.current = userId ?? null

    if (!userId) {
      const authority = `anonymous:${anonymousAnalyticsConsent === true ? 'granted' : 'denied'}`
      if (appliedAuthority.current === authority) return
      appliedAuthority.current = authority
      if (anonymousAnalyticsConsent === true) lifecycle.grant()
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

    lifecycle.grant(userId, {
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
