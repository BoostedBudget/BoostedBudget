// Public compatibility module. The schema-backed catalog owns event names and symbols.
export {
  ANALYTICS_EVENTS,
  ANALYTICS_EVENT_CONTRACTS,
  isValidAnalyticsEventPayload,
  normalizeAnalyticsEventPayload,
} from './events.generated'
export type {
  AnalyticsEventArguments,
  AnalyticsEventName,
  AnalyticsEventProperties,
  AnalyticsPropertyContract,
} from './events.generated'
