import { useEffect, useRef } from 'react'
import {
  capturePageView,
  getAnalyticsLifecycleState,
  subscribeToAnalyticsLifecycle,
  type AnalyticsLifecycleState,
} from '../lib/posthog'

export interface PageViewDependencies {
  capture: (url: string) => void
  getState: () => AnalyticsLifecycleState
  subscribe: (listener: (state: AnalyticsLifecycleState) => void) => () => void
}

const defaultDependencies: PageViewDependencies = {
  capture: capturePageView,
  getState: getAnalyticsLifecycleState,
  subscribe: subscribeToAnalyticsLifecycle,
}

export function useConsentAwarePageViews(
  url: string,
  dependencies: PageViewDependencies = defaultDependencies,
): void {
  const latestUrl = useRef(url)
  const lastCapturedUrl = useRef<string | null>(null)
  latestUrl.current = url

  useEffect(() => {
    if (dependencies.getState() !== 'granted' || lastCapturedUrl.current === url) return
    dependencies.capture(url)
    lastCapturedUrl.current = url
  }, [dependencies, url])

  useEffect(() => dependencies.subscribe(state => {
    if (state !== 'granted') return
    dependencies.capture(latestUrl.current)
    lastCapturedUrl.current = latestUrl.current
  }), [dependencies])
}
