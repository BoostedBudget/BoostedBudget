import { format, parseISO } from 'date-fns'
import { type DatePreset, PRESETS } from './presets'

const API_DATE_FORMAT = 'yyyy-MM-dd'

/** Format a Date to YYYY-MM-DD for API params. */
export function formatDateForAPI(d: Date): string {
  return format(d, API_DATE_FORMAT)
}

/**
 * Extract the UTC calendar date key from an RFC3339 timestamp or date-only string.
 * Uses string slicing to avoid local-TZ shift: "2026-04-29T00:00:00Z" → "2026-04-29".
 */
export function historyDateKey(rfc3339: string): string {
  return rfc3339.slice(0, 10)
}

/**
 * Format a history snapshot date for display using the UTC calendar date.
 * Anchors to noon UTC before formatting so the result is TZ-safe in any
 * negative-offset timezone (parseISO of a date-only string defaults to midnight local,
 * which would shift back one day for users west of UTC).
 */
export function formatHistoryLabel(rfc3339: string, pattern: string): string {
  return format(parseISO(historyDateKey(rfc3339) + 'T12:00:00Z'), pattern)
}

/**
 * Generate an array of YYYY-MM-DD calendar date strings (UTC) covering
 * [startRfc3339, endRfc3339] inclusive. Uses noon-UTC anchor to avoid DST
 * and local-TZ edge cases.
 */
export function calendarDateRange(startRfc3339: string, endRfc3339: string): string[] {
  const keys: string[] = [];
  const msPerDay = 24 * 60 * 60 * 1000;
  let d = new Date(historyDateKey(startRfc3339) + 'T12:00:00Z');
  const last = new Date(historyDateKey(endRfc3339) + 'T12:00:00Z');

  while (d <= last) {
    keys.push(d.toISOString().slice(0, 10));
    d = new Date(d.getTime() + msPerDay);
  }
  return keys;
}

/**
 * Reverse-map a start/end date pair back to a DatePreset value.
 * Returns 'all' if both are empty, 'custom' if no preset matches.
 */
export function inferPreset(
  startDate?: string,
  endDate?: string
): DatePreset {
  if (!startDate && !endDate) return 'all'

  // Try each non-null, non-custom preset
  const candidates: DatePreset[] = [
    'this_week',
    'this_month',
    'last_month',
    'last_3_months',
    'last_6_months',
    'ytd',
  ]

  for (const preset of candidates) {
    const def = PRESETS.find((p) => p.value === preset)
    const range = def?.getRange()
    if (range && range.startDate === startDate && range.endDate === endDate) {
      return preset
    }
  }

  return 'custom'
}

/**
 * Resolves a server snapshot time from an `as_of` field.
 *
 * Used by admin components (UsersPage, UserDetailModal, SubscriptionOverrideModal)
 * to anchor override-liveness arithmetic to the server's snapshot instant
 * (Issue #1462 follow-up G2) instead of the browser's clock. This prevents
 * browser/server clock skew from causing disagreement about whether an override
 * is live near expiry boundaries.
 *
 * @param asOfString - ISO date string from the server (e.g., data.as_of, user.as_of)
 * @returns A Date object anchored to the server's snapshot, or the current browser time
 *          if parsing fails or the field is absent/unparseable.
 */
export function resolveServerSnapshotTime(asOfString: string | null | undefined): Date {
  if (!asOfString) {
    return new Date()
  }
  const asOfMs = new Date(asOfString).getTime()
  return Number.isNaN(asOfMs) ? new Date() : new Date(asOfMs)
}
