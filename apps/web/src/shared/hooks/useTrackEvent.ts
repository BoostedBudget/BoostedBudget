import { useCallback } from 'react'
import { captureProductEvent } from '../lib/posthog'
import type { AnalyticsEventName } from '../analytics/events'

type TrackEvent = (event: AnalyticsEventName, properties?: Record<string, unknown>) => void

export function useTrackEvent(): TrackEvent {
  return useCallback((event: AnalyticsEventName, properties?: Record<string, unknown>) => {
    captureProductEvent(event, properties)
  }, [])
}
