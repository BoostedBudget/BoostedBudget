import { useCallback } from 'react'
import { posthog } from '../lib/posthog'
import type { AnalyticsEventName } from '../analytics/events'

export function useTrackEvent() {
  return useCallback((event: AnalyticsEventName, properties?: Record<string, unknown>) => {
    posthog.capture(event, properties)
  }, [])
}
