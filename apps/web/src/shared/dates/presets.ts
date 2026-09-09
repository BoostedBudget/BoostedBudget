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

export const PRESETS: PresetDefinition[] = [
  {
    value: 'this_week',
    label: 'This Week',
    getRange: (now) => {
      const today = utcCalendarDay(now)
      return {
        startDate: fmt(startOfWeek(today, { weekStartsOn: 1 })),
        endDate: fmt(today),
      }
    },
  },
  {
    value: 'this_month',
    label: 'This Month',
    // Ends TODAY, not on endOfMonth(today): an end date in the future made the backend
    // zero-fill and the frontend forward-fill manufacture chart points for days that had not
    // happened yet (#1626 Phase 3, Defect 1). The manual-entry path was already clamped by
    // DateRangePicker's `max={today}`; this preset was the hole.
    getRange: (now) => {
      const today = utcCalendarDay(now)
      return {
        startDate: fmt(startOfMonth(today)),
        endDate: fmt(today),
      }
    },
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
    getRange: (now) => {
      const today = utcCalendarDay(now)
      return {
        startDate: fmt(subMonths(today, 3)),
        endDate: fmt(today),
      }
    },
  },
  {
    value: 'last_6_months',
    label: 'Last 6 Months',
    getRange: (now) => {
      const today = utcCalendarDay(now)
      return {
        startDate: fmt(subMonths(today, 6)),
        endDate: fmt(today),
      }
    },
  },
  {
    value: 'ytd',
    label: 'Year to Date',
    getRange: (now) => {
      const today = utcCalendarDay(now)
      return {
        startDate: fmt(startOfYear(today)),
        endDate: fmt(today),
      }
    },
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
