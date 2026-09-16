import { useCallback } from 'react'
import { captureProductEvent } from '../lib/posthog'
import type { AnalyticsEventArguments, AnalyticsEventName } from '../analytics/events'

type TrackEvent = <Event extends AnalyticsEventName>(
  event: Event,
  ...args: AnalyticsEventArguments<Event>
) => void

export function useTrackEvent(): TrackEvent {
  return useCallback(<Event extends AnalyticsEventName>(
    event: Event,
    ...args: AnalyticsEventArguments<Event>
  ) => {
    captureProductEvent(event, ...args)
  }, [])
}
