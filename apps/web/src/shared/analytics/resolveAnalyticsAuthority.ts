import type { Properties } from 'posthog-js'
import type { AnalyticsAuthority, AnalyticsPurposes } from '../lib/analyticsLifecycle'

export interface ResolveAnalyticsAuthorityInput {
  scope: string
  purposes: AnalyticsPurposes
  distinctId?: string
  firstName?: string | null
  fullName?: string | null
}

/**
 * Builds the single browser analytics authority. Identified diagnostics ship
 * exactly one person property — the Clerk display name, full name first and
 * first name as the fallback — never an email or server-side tier state.
 */
export function resolveAnalyticsAuthority({
  scope,
  purposes,
  distinctId,
  firstName,
  fullName,
}: ResolveAnalyticsAuthorityInput): AnalyticsAuthority {
  const name = fullName?.trim() || firstName?.trim()
  const personProperties: Properties | undefined = name ? { name } : undefined
  return {
    scope,
    purposes,
    ...(distinctId ? { distinctId } : {}),
    ...(personProperties ? { personProperties } : {}),
  }
}
