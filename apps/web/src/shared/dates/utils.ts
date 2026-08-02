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
 * The RFC3339 date-time separator prefix: a 4-digit year, 2-digit month, 2-digit day, and the
 * literal 'T' that introduces the time-of-day component.
 *
 * Required in addition to a `parseISO` validity check because `parseISO` is deliberately lenient
 * about ISO 8601 *precision* — it happily accepts partial forms (a bare year `'2026'`, a
 * year-month `'2026-08'`, a date-only `'2026-08-02'`, even a week-date `'2026-W31'`), defaulting
 * the missing time-of-day to midnight rather than rejecting them. The wire `as_of` field this
 * guards is OpenAPI `format: date-time` — always a full RFC3339 instant — so any of those
 * partial forms reaching here is itself a malformed value, not a legitimately-terser one. Also
 * rejects plain non-ISO garbage a bare `new Date(x)` parse would otherwise silently accept as a
 * real calendar date (e.g. `'0'` -> year 2000, `'2026'` -> Jan 1 2026, `'08/02/2026'` ->
 * locale-dependent) with no "Invalid Date" tell to reveal the mistake.
 */
const RFC3339_DATE_TIME_PREFIX = /^\d{4}-\d{2}-\d{2}T/

/**
 * Reports whether a server-supplied date string is a valid, full-precision RFC3339 date-time.
 *
 * This is the exact parse check resolveServerSnapshotTime (below) uses to decide when to
 * fall back to the browser clock, extracted so callers that need to guard a DIFFERENT
 * computation — not resolve a fallback Date, just decide whether it's safe to format one at
 * all — can reuse the identical rule instead of duplicating the same two checks at each call
 * site. A malformed but truthy `as_of` (present on the wire, not just absent) previously reached
 * `formatRelativeTime`/`toLocaleString` unguarded in SubscriptionOverrideModal.tsx and rendered
 * the literal string "Invalid Date" — a recurrence of the same defect shape
 * resolveServerSnapshotTime itself already guards against.
 *
 * ISO-strict by design (see RFC3339_DATE_TIME_PREFIX above): a bare `new Date(x)` parse — the
 * prior implementation — accepts numeric-but-malformed strings like `'0'`, `'1'`, or `'2026'` as
 * real calendar dates (years 2000, 2001, and 2026 respectively), which silently anchored
 * resolveServerSnapshotTime's callers (UsersPage, UserDetailModal, SubscriptionOverrideModal
 * override-liveness arithmetic) to a bogus instant with no "Invalid Date" tell — strictly worse
 * than the browser-clock fallback this helper exists to provide.
 *
 * @param dateStr - a date string from the server, or null/undefined if the field is absent.
 */
export function isParsableDate(dateStr: string | null | undefined): boolean {
  if (!dateStr) return false
  if (!RFC3339_DATE_TIME_PREFIX.test(dateStr)) return false
  return !Number.isNaN(parseISO(dateStr).getTime())
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
 * @param asOfString - RFC3339 date-time string from the server (e.g., data.as_of, user.as_of)
 * @returns A Date object anchored to the server's snapshot, or the current browser time
 *          if parsing fails or the field is absent/unparseable/not a full-precision RFC3339
 *          date-time (see isParsableDate's ISO-strict rule above).
 */
export function resolveServerSnapshotTime(asOfString: string | null | undefined): Date {
  return isParsableDate(asOfString) ? parseISO(asOfString as string) : new Date()
}
