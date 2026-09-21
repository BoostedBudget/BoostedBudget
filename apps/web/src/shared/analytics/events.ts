/**
 * Public compatibility module. The schema-backed catalog owns event names and symbols.
 *
 * #1685: `BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED` is a FRONTEND-ONLY contract, declared here
 * instead of in `config/posthog_events.yaml`. The catalog and its generated artifacts are pinned
 * by exact string (`bun run validate:posthog` regenerates and byte-compares them), so the
 * frontend contract is added additively at this boundary rather than by hand-editing generated
 * output. Every catalogued event still takes the catalog as its source of truth: the catalog
 * wins on any name both layers define, and both validators below delegate to the generated ones
 * for every event the catalog knows.
 */
import {
  ANALYTICS_EVENTS as CATALOG_ANALYTICS_EVENTS,
  ANALYTICS_EVENT_CONTRACTS as CATALOG_ANALYTICS_EVENT_CONTRACTS,
  normalizeAnalyticsEventPayload as normalizeCatalogAnalyticsEventPayload,
  type AnalyticsEventProperties as CatalogAnalyticsEventProperties,
  type AnalyticsPropertyContract,
} from './events.generated'

export type { AnalyticsPropertyContract }

const FRONTEND_ONLY_ANALYTICS_EVENTS = {
  /**
   * Fires when the budget page displays the persisted `actuals_incomplete_reasons` notice.
   * `reason_count` is the number of DISTINCT reasons displayed — never a reason string, an
   * identifier or an amount; the notice itself renders no financial amount.
   */
  BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED: 'budget_actuals_incomplete_reasons_viewed',
} as const

export const ANALYTICS_EVENTS = {
  ...FRONTEND_ONLY_ANALYTICS_EVENTS,
  ...CATALOG_ANALYTICS_EVENTS,
} as const

/**
 * The frontend-only contracts are integer-count only. A future event needing another property
 * type must widen this union — otherwise the validator below would drop it silently.
 */
type FrontendOnlyPropertyContract = { type: 'integer'; required: boolean; minimum?: number }

const FRONTEND_ONLY_ANALYTICS_EVENT_CONTRACTS: Readonly<
  Record<string, Readonly<Record<string, FrontendOnlyPropertyContract>>>
> = {
  'budget_actuals_incomplete_reasons_viewed': {
    reason_count: { type: 'integer', required: true, minimum: 1 },
  },
}

/**
 * Property contracts for every event this module exposes, keyed by event name. The key type is
 * `string` rather than `AnalyticsEventName` because the catalog's contracts arrive as a spread
 * whose keys TypeScript cannot enumerate; the generated module still exports the precise
 * `Record<AnalyticsEventName, …>` for any consumer that needs it.
 */
export const ANALYTICS_EVENT_CONTRACTS: Readonly<
  Record<string, Readonly<Record<string, AnalyticsPropertyContract>>>
> = {
  ...FRONTEND_ONLY_ANALYTICS_EVENT_CONTRACTS,
  ...CATALOG_ANALYTICS_EVENT_CONTRACTS,
}

export type AnalyticsEventProperties = CatalogAnalyticsEventProperties & {
  'budget_actuals_incomplete_reasons_viewed': { reason_count: number }
}

export type AnalyticsEventName = keyof AnalyticsEventProperties

export type AnalyticsEventArguments<Event extends AnalyticsEventName> =
  keyof AnalyticsEventProperties[Event] extends never
    ? []
    : Record<never, never> extends AnalyticsEventProperties[Event]
      ? [properties?: AnalyticsEventProperties[Event]]
      : [properties: AnalyticsEventProperties[Event]]

function normalizeFrontendOnlyPayload(
  event: string,
  properties?: unknown,
): Record<string, unknown> | null {
  if (properties === null || typeof properties !== 'object' || Array.isArray(properties)) return null
  const input = properties as Record<string, unknown>
  const contract = FRONTEND_ONLY_ANALYTICS_EVENT_CONTRACTS[event]
  for (const key of Object.keys(input)) {
    if (!Object.prototype.hasOwnProperty.call(contract, key)) return null
  }
  for (const [key, propertyContract] of Object.entries(contract)) {
    const value = input[key]
    if (value === undefined) {
      if (propertyContract.required) return null
      continue
    }
    if (typeof value !== 'number' || !Number.isInteger(value)) return null
    if (propertyContract.minimum !== undefined && value < propertyContract.minimum) return null
  }
  return input
}

export function normalizeAnalyticsEventPayload(
  event: string,
  properties?: unknown,
): Record<string, unknown> | null {
  if (Object.prototype.hasOwnProperty.call(CATALOG_ANALYTICS_EVENT_CONTRACTS, event)) {
    return normalizeCatalogAnalyticsEventPayload(event, properties)
  }
  if (!Object.prototype.hasOwnProperty.call(FRONTEND_ONLY_ANALYTICS_EVENT_CONTRACTS, event)) {
    return null
  }
  return normalizeFrontendOnlyPayload(event, properties)
}

export function isValidAnalyticsEventPayload(event: string, properties?: unknown): boolean {
  return normalizeAnalyticsEventPayload(event, properties) !== null
}
