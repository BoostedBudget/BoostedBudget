import { useCallback, useEffect, useRef } from 'react'
import {
  capturePageView,
  getAnalyticsLifecycleSnapshot,
  isAnonymousAnalyticsPath,
  isSensitiveAnalyticsPath,
  sanitizeAnalyticsUrl,
  subscribeToAnalyticsLifecycle,
  type AnalyticsLifecycleSnapshot,
} from '../lib/posthog'

export interface PageViewDependencies {
  capture: (url: string) => void
  getSnapshot: () => AnalyticsLifecycleSnapshot
  subscribe: (listener: (snapshot: AnalyticsLifecycleSnapshot) => void) => () => void
}

const defaultDependencies: PageViewDependencies = {
  capture: capturePageView,
  getSnapshot: getAnalyticsLifecycleSnapshot,
  subscribe: subscribeToAnalyticsLifecycle,
}

function pageViewKey(url: string, snapshot: AnalyticsLifecycleSnapshot): string | null {
  if (snapshot.scope === null) return null
  if (isSensitiveAnalyticsPath(url)) return null
  if (snapshot.scope === 'anonymous' && !isAnonymousAnalyticsPath(url)) return null
  return `${snapshot.generation}:${snapshot.scope}:${sanitizeAnalyticsUrl(url)}`
}

export function useConsentAwarePageViews(
  url: string,
  dependencies: PageViewDependencies = defaultDependencies,
): void {
  const latestUrl = useRef(url)
  const lastCapturedKey = useRef<string | null>(null)
  latestUrl.current = url

  const captureOnce = useCallback((
    candidateUrl: string,
    snapshot: AnalyticsLifecycleSnapshot,
  ): void => {
    if (snapshot.state !== 'granted') return
    const key = pageViewKey(candidateUrl, snapshot)
    if (key === null) {
      lastCapturedKey.current = null
      return
    }
    if (lastCapturedKey.current === key) return
    dependencies.capture(candidateUrl)
    lastCapturedKey.current = key
  }, [dependencies])

  useEffect(() => {
    captureOnce(url, dependencies.getSnapshot())
  }, [captureOnce, dependencies, url])

  useEffect(() => dependencies.subscribe(snapshot => {
    captureOnce(latestUrl.current, snapshot)
  }), [captureOnce, dependencies])
}
