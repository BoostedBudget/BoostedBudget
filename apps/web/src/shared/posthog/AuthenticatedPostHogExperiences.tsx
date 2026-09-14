import { useEffect, useLayoutEffect, useMemo, useState } from 'react'
import { useUser } from '@clerk/react-router'
import { useGetPostHogSupportIdentity } from '@boostedbudget/api-client/endpoints/support/support'
import {
  getAnalyticsLifecycleSnapshot,
  isPostHogConfigured,
  subscribeToAnalyticsLifecycle,
  type AnalyticsLifecycleSnapshot,
} from '../lib/posthog'
import {
  createPostHogEngagementController,
  type PostHogEngagementController,
  type VerifiedPostHogIdentity,
} from './engagement'

export interface EngagementDependencies {
  useAuthState(): { isLoaded: boolean; userId: string | null }
  useVerifiedIdentity(userId: string | null, enabled: boolean, pollContinuously: boolean): {
    data: VerifiedPostHogIdentity | undefined
    isLoading: boolean
    isError: boolean
    refetch?: () => Promise<unknown>
  }
  useAnalyticsSnapshot(): AnalyticsLifecycleSnapshot
}

const defaultDependencies: EngagementDependencies = {
  useAuthState() {
    const { isLoaded, user } = useUser()
    return { isLoaded, userId: user?.id ?? null }
  },
  useVerifiedIdentity(userId, enabled, pollContinuously) {
    return useGetPostHogSupportIdentity({
      query: {
        enabled,
        queryKey: ['posthogSupportIdentity', userId],
        retry: false,
        staleTime: 0,
        gcTime: 0,
        refetchInterval: pollContinuously ? 15_000 : false,
        refetchIntervalInBackground: true,
      },
    })
  },
  useAnalyticsSnapshot() {
    const [snapshot, setSnapshot] = useState(getAnalyticsLifecycleSnapshot)
    useEffect(() => subscribeToAnalyticsLifecycle(setSnapshot), [])
    return snapshot
  },
}

export function AuthenticatedPostHogExperiences({
  controller: providedController,
  dependencies = defaultDependencies,
  routeEligible = true,
}: {
  controller?: PostHogEngagementController
  dependencies?: EngagementDependencies
  routeEligible?: boolean
}) {
  const ownedController = useMemo(() => createPostHogEngagementController(), [])
  const controller = providedController ?? ownedController
  const { isLoaded, userId } = dependencies.useAuthState()
  const analytics = dependencies.useAnalyticsSnapshot()
  const hasAuthenticatedUser = isLoaded && userId !== null
  const hasRouteAuthority = hasAuthenticatedUser && routeEligible
  const configured = isPostHogConfigured()
  const surveyConsentAuthorized = hasAuthenticatedUser
    && analytics.state === 'granted'
    && analytics.scope === userId
  const surveyAuthorized = routeEligible && surveyConsentAuthorized
  const [supportRequestedFor, setSupportRequestedFor] = useState<string | null>(null)
  const supportRequested = hasRouteAuthority && supportRequestedFor === userId
  const identity = dependencies.useVerifiedIdentity(
    userId,
    configured && (supportRequested || surveyAuthorized),
    configured && supportRequested,
  )
  const refetchIdentity = identity.refetch
  const [supportReady, setSupportReady] = useState(false)
  const [supportUnavailable, setSupportUnavailable] = useState(false)
  const [handedOff, setHandedOff] = useState(false)
  const [resumeValidationPending, setResumeValidationPending] = useState(false)
  const verifiedIdentity = hasRouteAuthority
    && !identity.isLoading
    && !identity.isError
    && !resumeValidationPending
    && identity.data?.distinct_id === userId
    && identity.data.identity_hash.length > 0
    ? identity.data
    : undefined

  useLayoutEffect(() => {
    setSupportReady(false)
    setSupportUnavailable(false)
    setHandedOff(false)

    if (!supportRequested || !verifiedIdentity) {
      controller.deactivateSupport()
      return
    }

    controller.activateSupport(verifiedIdentity)

    let attempts = 0
    let interval: number | undefined
    const checkAvailability = () => {
      if (document.visibilityState === 'hidden') return
      attempts += 1
      if (controller.isSupportAvailable()) {
        setSupportReady(true)
        controller.showSupport()
        setHandedOff(true)
        if (interval !== undefined) window.clearInterval(interval)
      } else if (attempts >= 40) {
        setSupportUnavailable(true)
        if (interval !== undefined) window.clearInterval(interval)
      }
    }
    checkAvailability()
    if (!controller.isSupportAvailable()) interval = window.setInterval(checkAvailability, 250)

    return () => {
      if (interval !== undefined) window.clearInterval(interval)
      controller.deactivateSupport()
    }
  }, [controller, supportRequested, verifiedIdentity])

  useLayoutEffect(() => {
    if (!surveyConsentAuthorized) {
      controller.deactivateSurveys()
    } else if (!routeEligible) {
      controller.suspendSurveys()
    } else if (verifiedIdentity && surveyAuthorized) {
      controller.activateSurveys(verifiedIdentity)
    } else {
      controller.deactivateSurveys()
    }
  }, [
    analytics.generation,
    analytics.scope,
    analytics.state,
    controller,
    routeEligible,
    surveyConsentAuthorized,
    surveyAuthorized,
    userId,
    verifiedIdentity,
  ])

  useLayoutEffect(() => () => {
    controller.deactivateSupport()
    controller.deactivateSurveys()
  }, [controller])

  useEffect(() => {
    if (!hasRouteAuthority || supportRequestedFor !== userId) setSupportRequestedFor(null)
  }, [hasRouteAuthority, supportRequestedFor, userId])

  useEffect(() => {
    if (!handedOff) return
    const handleVisibility = () => {
      if (document.visibilityState === 'hidden') {
        controller.getSupportClient()?.conversations.hide()
      } else {
        setResumeValidationPending(true)
        controller.deactivateSupport()
        const revalidation = refetchIdentity?.()
        if (revalidation) {
          void revalidation.finally(() => setResumeValidationPending(false))
        }
      }
    }
    document.addEventListener('visibilitychange', handleVisibility)
    return () => document.removeEventListener('visibilitychange', handleVisibility)
  }, [controller, handedOff, refetchIdentity])

  if (!hasRouteAuthority) return null

  if (!configured || (supportRequested && (identity.isError || supportUnavailable))) {
    return (
      <button
        type="button"
        aria-label="Support unavailable"
        disabled
        className="fixed bottom-6 right-6 z-50 rounded-full bg-muted px-4 py-3 text-sm text-muted-foreground shadow-lg"
      >
        Support unavailable
      </button>
    )
  }

  if (supportRequested && (identity.isLoading || !supportReady)) {
    return (
      <button
        type="button"
        aria-label="Support loading"
        disabled
        className="fixed bottom-6 right-6 z-50 rounded-full bg-muted px-4 py-3 text-sm text-muted-foreground shadow-lg"
      >
        Support loading…
      </button>
    )
  }

  if (handedOff) return null

  return (
    <button
      type="button"
      aria-label="Support"
      className="fixed bottom-6 right-6 z-50 rounded-full bg-primary px-5 py-3 font-medium text-primary-foreground shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      onClick={() => {
        setSupportRequestedFor(userId)
      }}
    >
      Support
    </button>
  )
}
