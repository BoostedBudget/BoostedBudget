import {
  format,
  startOfWeek,
  startOfMonth,
  endOfMonth,
  startOfYear,
  subMonths,
} from 'date-fns'

const API_DATE_FORMAT = 'yyyy-MM-dd'

export type DatePreset =
  | 'this_week'
  | 'this_month'
  | 'last_month'
  | 'last_3_months'
  | 'last_6_months'
  | 'ytd'
  | 'all'
  | 'custom'

export interface DateRange {
  /** YYYY-MM-DD */
  startDate: string
  /** YYYY-MM-DD */
  endDate: string
}

export interface PresetDefinition {
  value: DatePreset
  label: string
  /**
   * @param now - the instant to resolve the range against; defaults to the current time.
   *              Injectable so tests can pin it without touching the ambient clock.
   */
  getRange: (now?: Date) => DateRange | null
}

function fmt(d: Date): string {
  return format(d, API_DATE_FORMAT)
}

/**
 * The UTC calendar date of `now`, as a Date whose LOCAL fields carry that date.
 *
 * Two things have to agree here. The backend parses `YYYY-MM-DD` to UTC midnight and buckets
 * transactions and snapshots on UTC calendar dates, so "today" must be the UTC calendar date;
 * but `fmt` and the date-fns helpers below all read LOCAL fields, so the value they read has to
 * already be the UTC date. Rebuilding the date from `getUTC*` parts into a local-midnight Date
 * satisfies both: everything downstream keeps using local-field helpers, and what those helpers
 * see is the backend's day. A browser-local `new Date()` would instead disagree with the backend
 * by up to a day near the date boundary (#1626 Phase 3, controller ruling: UTC end-to-end).
 *
 * A per-user IANA timezone does exist (`users.timezone`, default "UTC") and moving both the
 * personal and household paths to user-local is a coherent follow-up; it is deliberately not
 * done here.
 */
function utcCalendarDay(now: Date = new Date()): Date {
  return new Date(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
}

/**
 * Builds a `getRange` for a preset whose window ENDS TODAY and only its start varies.
 *
 * Five of the presets below are of that shape and share this one construction — `this_week`,
 * `this_month`, `last_3_months`, `last_6_months`, and `ytd`. (`last_month` deliberately does not
 * use this helper: it ends at the *previous* month's end, not today — see its own comment below.)
 * Sharing this construction is what keeps the #1626 Phase 3 Defect 1 fix structural rather than
 * repeated: `this_month` used to end on `endOfMonth(today)`, and an end date in the future made
 * the backend zero-fill and the frontend forward-fill manufacture chart points for days that had
 * not happened yet. (The manual-entry path was already clamped by DateRangePicker's
 * `max={today}`; the presets were the hole.) A new preset written through this helper cannot
 * reopen it.
 *
 * `now` is resolved through utcCalendarDay in one place for the same reason — see there for why a
 * browser-local `new Date()` disagrees with the backend by up to a day.
 */
function rangeEndingToday(
  startFrom: (today: Date) => Date
): (now?: Date) => DateRange {
  return (now) => {
    const today = utcCalendarDay(now)
    return { startDate: fmt(startFrom(today)), endDate: fmt(today) }
  }
}

export const PRESETS: PresetDefinition[] = [
  {
    value: 'this_week',
    label: 'This Week',
    getRange: rangeEndingToday((today) => startOfWeek(today, { weekStartsOn: 1 })),
  },
  {
    value: 'this_month',
    label: 'This Month',
    getRange: rangeEndingToday((today) => startOfMonth(today)),
  },
  {
    value: 'last_month',
    label: 'Last Month',
    // Unchanged in scope, UTC-resolved for consistency: both bounds are already in the PAST,
    // so this preset never needed a clamp.
    getRange: (now) => {
      const prevMonth = subMonths(utcCalendarDay(now), 1)
      return {
        startDate: fmt(startOfMonth(prevMonth)),
        endDate: fmt(endOfMonth(prevMonth)),
      }
    },
  },
  {
    value: 'last_3_months',
    label: 'Last 3 Months',
    getRange: rangeEndingToday((today) => subMonths(today, 3)),
  },
  {
    value: 'last_6_months',
    label: 'Last 6 Months',
    getRange: rangeEndingToday((today) => subMonths(today, 6)),
  },
  {
    value: 'ytd',
    label: 'Year to Date',
    getRange: rangeEndingToday((today) => startOfYear(today)),
  },
  {
    value: 'all',
    label: 'All Time',
    getRange: () => null,
  },
  {
    value: 'custom',
    label: 'Custom',
    getRange: () => null,
  },
]

/**
 * Returns the date range for a preset, or null for 'all' and 'custom'.
 *
 * @param now - the instant to resolve the range against; defaults to the current time.
 */
export function getDateRange(preset: DatePreset, now?: Date): DateRange | null {
  const def = PRESETS.find((p) => p.value === preset)
  return def?.getRange(now) ?? null
}
