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
 * Matches the Go `time.Time` JSON wire format — NOT full RFC3339. Every backend `as_of` field
 * this module's date-time helpers guard is declared `format: date-time` in `api/openapi.yaml`
 * (e.g. lines 489, 594, 731) and backed by a Go `time.Time` struct field (e.g.
 * `AsOf time.Time \`json:"as_of"\`` in backend/internal/domain/admin_user.go). Go's
 * `encoding/json` always marshals `time.Time` the same way: 4-digit year, 2-digit month/day,
 * uppercase literal `T`, zero-padded `HH:MM:SS` (seconds are never omitted), an optional
 * fractional-second component, and a mandatory uppercase `Z` or numeric `±HH:MM` offset (also
 * never omitted). This pattern requires exactly that shape — a strict SUBSET of RFC3339 §5.6,
 * chosen deliberately narrower because the full grammar permits things this wire never produces
 * and that JS `Date` cannot faithfully represent anyway:
 *   - Lowercase `t`/`z` separators (RFC3339 permits them; Go never emits them) — rejected. This
 *     is the one intentional divergence from calling this "RFC3339": accepting only uppercase is
 *     correct for this wire, but it means this function is not a general RFC3339 validator.
 *   - The `-00:00` "unknown local offset" marker — Go never emits it, so it's not special-cased;
 *     it would need to match the same `[+-]HH:MM` shape as any other offset (it does, structurally
 *     "-00:00" parses as a zero offset, which is an acceptable simplification for a wire that
 *     never emits it as a semantic "unknown" marker in the first place).
 *   - A contextually valid leap second (`:60`) — rejected by the `[0-5]\d` seconds bound below,
 *     because JS `Date` cannot represent a leap second either.
 *   - Arbitrary fractional-second precision — bounded to 1-9 digits here (JS `Date` only has
 *     millisecond resolution, but `parseISO` rounds sub-millisecond digits rather than needing
 *     them rejected outright, so there's no reason to cap tighter than what Go could plausibly
 *     emit).
 *
 * Field-level numeric bounds are enforced for hour (00-23), minute/second (00-59), and offset
 * hour/minute (00-23/00-59) directly in the pattern. This matters because `parseISO` does NOT
 * reject all of these on its own: empirically, `parseISO('...T24:00:00Z')` and
 * `parseISO('...+24:00')` both parse successfully by rolling the overflow into the adjacent day
 * (verified — see utils.test.ts) rather than failing, so an unbounded pattern would let
 * `'2026-08-02T24:00:00Z'` and `'...+24:00'` (both explicitly called out as must-reject) through.
 * `parseISO` DOES already reject minute/second overflow (`:60`) and offset-minute overflow on its
 * own, so those bounds here are belt-and-suspenders, not load-bearing — but stated explicitly
 * rather than relied on implicitly, since silently depending on an undocumented library-internal
 * validation boundary is fragile across `date-fns` versions.
 *
 * Month and day are intentionally left as bare `\d{2}` here — NOT bounded to 01-12 / 01-31 in the
 * pattern — because "is day 30 valid" depends on which month (28/29/30/31-day) AND, for Feb 29,
 * which year (leap or not), which isn't expressible as a fixed-width regex range. That check is
 * NOT skipped: `parseISO`'s own `getTime()` empirically already performs exact Gregorian
 * calendar validation — confirmed by direct probing to reject month `00`/`13`, day `00`, day
 * overflow for every short month (Apr/Jun/Sep/Nov 31, Feb 30), and non-leap Feb 29 (2026, and the
 * 1900 century-rule case), while correctly accepting leap Feb 29 (2028, and the 2000 century-rule
 * case). So the `Number.isNaN(parseISO(dateStr).getTime())` check below IS the semantic calendar
 * validator for month/day — not just a defensive NaN guard — and no separate hand-rolled
 * month-length/leap-year function is needed on top of it.
 */
const GO_TIME_JSON_PATTERN =
  /^(\d{4})-(\d{2})-(\d{2})T([01]\d|2[0-3]):([0-5]\d):([0-5]\d)(\.\d{1,9})?(Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/

/**
 * Parses a server-supplied date-time string against the Go `time.Time` JSON wire format (see
 * GO_TIME_JSON_PATTERN above), returning the parsed instant or `null` if the string is absent,
 * malformed, or calendar-invalid (bad month/day/leap-day).
 *
 * This is the single parse point for that format. `isParsableDate` and `resolveServerSnapshotTime`
 * below are both thin wrappers over this function, and callers that need the parsed `Date` itself
 * (e.g. `SnapshotAgeLabel`, which uses the SAME parsed instant for its visible label, its `title`
 * attribute, and any liveness arithmetic) should call this directly rather than checking
 * `isParsableDate` and then re-parsing the same string with a different parser — the two can
 * disagree on a malformed instant (Issue #1432 follow-up S4).
 *
 * @param dateStr - a date string from the server, or null/undefined if the field is absent.
 */
export function parseServerDateTime(dateStr: string | null | undefined): Date | null {
  if (!dateStr) return null
  if (!GO_TIME_JSON_PATTERN.test(dateStr)) return null
  const parsed = parseISO(dateStr)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}

/**
 * Reports whether a server-supplied date string matches the Go `time.Time` JSON wire format —
 * NOT full RFC3339 (see GO_TIME_JSON_PATTERN above for exactly what that means and why).
 *
 * This is the exact parse check resolveServerSnapshotTime (below) uses to decide when to
 * fall back to the browser clock, extracted so callers that need to guard a DIFFERENT
 * computation — not resolve a fallback Date, just decide whether it's safe to format one at
 * all — can reuse the identical rule instead of duplicating it at each call site. Prefer
 * `parseServerDateTime` directly if the parsed `Date` is also needed — calling both re-parses
 * the same string twice for no reason.
 *
 * A malformed but truthy `as_of` (present on the wire, not just absent) previously reached
 * `formatRelativeTime`/`toLocaleString` unguarded in SubscriptionOverrideModal.tsx and rendered
 * the literal string "Invalid Date" — a recurrence of the same defect shape
 * resolveServerSnapshotTime itself already guards against.
 *
 * @param dateStr - a date string from the server, or null/undefined if the field is absent.
 */
export function isParsableDate(dateStr: string | null | undefined): boolean {
  return parseServerDateTime(dateStr) !== null
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
 * @param asOfString - a date-time string from the server (e.g., data.as_of, user.as_of),
 *        expected to be the Go `time.Time` JSON wire format (see GO_TIME_JSON_PATTERN above).
 * @returns A Date object anchored to the server's snapshot, or the current browser time
 *          if parsing fails or the field is absent/unparseable/malformed (see
 *          parseServerDateTime's rule above).
 */
export function resolveServerSnapshotTime(asOfString: string | null | undefined): Date {
  return parseServerDateTime(asOfString) ?? new Date()
}
