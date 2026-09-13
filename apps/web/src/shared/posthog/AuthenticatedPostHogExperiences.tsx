import { useEffect, useMemo, useState } from 'react'
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
  useVerifiedIdentity(userId: string | null, enabled: boolean): {
    data: VerifiedPostHogIdentity | undefined
    isLoading: boolean
    isError: boolean
  }
  useAnalyticsSnapshot(): AnalyticsLifecycleSnapshot
}

const defaultDependencies: EngagementDependencies = {
  useAuthState() {
    const { isLoaded, user } = useUser()
    return { isLoaded, userId: user?.id ?? null }
  },
  useVerifiedIdentity(userId, enabled) {
    return useGetPostHogSupportIdentity({
      query: {
        enabled,
        queryKey: ['posthogSupportIdentity', userId],
        retry: false,
        staleTime: 0,
        gcTime: 0,
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
}: {
  controller?: PostHogEngagementController
  dependencies?: EngagementDependencies
}) {
  const ownedController = useMemo(() => createPostHogEngagementController(), [])
  const controller = providedController ?? ownedController
  const { isLoaded, userId } = dependencies.useAuthState()
  const analytics = dependencies.useAnalyticsSnapshot()
  const hasAuthenticatedUser = isLoaded && userId !== null
  const configured = isPostHogConfigured()
  const identity = dependencies.useVerifiedIdentity(userId, hasAuthenticatedUser && configured)
  const [supportReady, setSupportReady] = useState(false)
  const [supportUnavailable, setSupportUnavailable] = useState(false)
  const [handedOff, setHandedOff] = useState(false)
  const verifiedIdentity = hasAuthenticatedUser
    && !identity.isLoading
    && !identity.isError
    && identity.data?.distinct_id === userId
    && identity.data.identity_hash.length > 0
    ? identity.data
    : undefined

  useEffect(() => {
    setSupportReady(false)
    setSupportUnavailable(false)
    setHandedOff(false)

    if (!verifiedIdentity) {
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
  }, [controller, verifiedIdentity])

  useEffect(() => {
    if (verifiedIdentity && analytics.state === 'granted' && analytics.scope === userId) {
      controller.activateSurveys(verifiedIdentity)
    } else {
      controller.deactivateSurveys()
    }
    return () => controller.deactivateSurveys()
  }, [
    analytics.generation,
    analytics.scope,
    analytics.state,
    controller,
    userId,
    verifiedIdentity,
  ])

  useEffect(() => {
    if (!handedOff) return
    const handleVisibility = () => {
      if (document.visibilityState === 'hidden') {
        controller.getSupportClient()?.conversations.hide()
      } else {
        controller.showSupport()
      }
    }
    document.addEventListener('visibilitychange', handleVisibility)
    return () => document.removeEventListener('visibilitychange', handleVisibility)
  }, [controller, handedOff])

  if (!hasAuthenticatedUser) return null

  if (!configured || identity.isError || supportUnavailable) {
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

  if (identity.isLoading) {
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

  if (!verifiedIdentity || !supportReady || handedOff) return null

  return (
    <button
      type="button"
      aria-label="Support"
      className="fixed bottom-6 right-6 z-50 rounded-full bg-primary px-5 py-3 font-medium text-primary-foreground shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      onClick={() => {
        controller.showSupport()
        setHandedOff(true)
      }}
    >
      Support
    </button>
  )
}
