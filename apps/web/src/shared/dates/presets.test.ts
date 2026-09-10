import { describe, test, expect, beforeAll, afterAll, setSystemTime } from 'bun:test'
import {
  getDateRange,
  PRESETS,
  type DatePreset,
} from './presets'

// Helper to parse a YYYY-MM-DD string to Date components
function parseDate(s: string): { year: number; month: number; day: number } {
  const [year, month, day] = s.split('-').map(Number)
  return { year, month, day }
}

describe('PRESETS array', () => {
  test('contains all expected presets', () => {
    const values = PRESETS.map((p) => p.value)
    expect(values).toContain('this_week')
    expect(values).toContain('this_month')
    expect(values).toContain('last_month')
    expect(values).toContain('last_3_months')
    expect(values).toContain('last_6_months')
    expect(values).toContain('ytd')
    expect(values).toContain('all')
    expect(values).toContain('custom')
  })

  test('every preset has a non-empty label', () => {
    for (const preset of PRESETS) {
      expect(preset.label.length).toBeGreaterThan(0)
    }
  })

  test('every preset has a value', () => {
    for (const preset of PRESETS) {
      expect(preset.value).toBeTruthy()
    }
  })
})

describe('getDateRange', () => {
  // #1626 Phase 3: this_month used to end on endOfMonth(today), which sent a future end date
  // to the backend and made both chart generators manufacture points for days that had not
  // happened yet. It now ends on today (UTC), so this asserts the month start and the
  // same-month invariant only — the end-day contract is pinned in the UTC describe below.
  test('this_month starts on the 1st and ends within the same month', () => {
    const range = getDateRange('this_month')
    expect(range).not.toBeNull()
    const start = parseDate(range!.startDate)
    const end = parseDate(range!.endDate)
    expect(start.day).toBe(1)
    expect(start.month).toBe(end.month)
    expect(start.year).toBe(end.year)
    expect(end.day).toBeGreaterThanOrEqual(1)
  })

  test('last_month returns previous month', () => {
    const range = getDateRange('last_month')
    expect(range).not.toBeNull()
    const startDate = new Date(range!.startDate)
    const endDate = new Date(range!.endDate)
    // start day = 1
    expect(startDate.getUTCDate()).toBe(1)
    // start and end in same month
    expect(startDate.getUTCMonth()).toBe(endDate.getUTCMonth())
    expect(startDate.getUTCFullYear()).toBe(endDate.getUTCFullYear())
    // Last month is before current month
    const thisMonth = new Date()
    const lastMonth = startDate
    expect(
      lastMonth.getUTCFullYear() * 12 + lastMonth.getUTCMonth()
    ).toBeLessThan(
      thisMonth.getFullYear() * 12 + thisMonth.getMonth()
    )
  })

  test('last_3_months is a rolling window ending today', () => {
    const range = getDateRange('last_3_months')
    expect(range).not.toBeNull()
    const start = new Date(range!.startDate + 'T12:00:00Z')
    const end = new Date(range!.endDate + 'T12:00:00Z')
    const today = new Date()
    expect(end.getUTCFullYear()).toBe(today.getFullYear())
    expect(end.getUTCMonth()).toBe(today.getMonth())
    expect(start.getTime()).toBeLessThan(end.getTime())
    const monthDiff =
      (end.getUTCFullYear() - start.getUTCFullYear()) * 12 +
      (end.getUTCMonth() - start.getUTCMonth())
    expect(monthDiff).toBe(3)
  })

  test('last_6_months is a rolling window ending today', () => {
    const range = getDateRange('last_6_months')
    expect(range).not.toBeNull()
    const start = new Date(range!.startDate + 'T12:00:00Z')
    const end = new Date(range!.endDate + 'T12:00:00Z')
    const today = new Date()
    expect(end.getUTCFullYear()).toBe(today.getFullYear())
    expect(end.getUTCMonth()).toBe(today.getMonth())
    expect(start.getTime()).toBeLessThan(end.getTime())
    const monthDiff =
      (end.getUTCFullYear() - start.getUTCFullYear()) * 12 +
      (end.getUTCMonth() - start.getUTCMonth())
    expect(monthDiff).toBe(6)
  })

  test('ytd starts on Jan 1 of current year', () => {
    const range = getDateRange('ytd')
    expect(range).not.toBeNull()
    const start = parseDate(range!.startDate)
    expect(start.month).toBe(1) // January
    expect(start.day).toBe(1)
    // End = today
    const today = new Date()
    const end = parseDate(range!.endDate)
    expect(end.year).toBe(today.getFullYear())
  })

  test('this_week starts on Monday', () => {
    const range = getDateRange('this_week')
    expect(range).not.toBeNull()
    const start = new Date(range!.startDate + 'T00:00:00Z')
    // Monday = day 1 in UTC
    expect(start.getUTCDay()).toBe(1)
    // End = today
    const today = new Date()
    const end = parseDate(range!.endDate)
    expect(end.year).toBe(today.getFullYear())
  })

  test('all returns null', () => {
    expect(getDateRange('all')).toBeNull()
  })

  test('custom returns null', () => {
    expect(getDateRange('custom')).toBeNull()
  })

  describe('rolling window end-today semantics (Issue #1173)', () => {
    // Freeze to noon UTC to avoid local-TZ date shifts; last_3/6_months end TODAY
    beforeAll(() => setSystemTime(new Date('2026-05-23T12:00:00.000Z')))
    afterAll(() => setSystemTime())

    test('last_3_months rolls back 3 months and ends today', () => {
      const range = getDateRange('last_3_months')
      expect(range).not.toBeNull()
      expect(range!.startDate).toBe('2026-02-23')
      expect(range!.endDate).toBe('2026-05-23')
    })

    test('last_6_months rolls back 6 months and ends today', () => {
      const range = getDateRange('last_6_months')
      expect(range).not.toBeNull()
      expect(range!.startDate).toBe('2025-11-23')
      expect(range!.endDate).toBe('2026-05-23')
    })
  })

  test('start is always <= end for all non-null presets', () => {
    const nonNullPresets: DatePreset[] = [
      'this_week',
      'this_month',
      'last_month',
      'last_3_months',
      'last_6_months',
      'ytd',
    ]
    for (const preset of nonNullPresets) {
      const range = getDateRange(preset)
      expect(range).not.toBeNull()
      expect(range!.startDate <= range!.endDate).toBe(true)
    }
  })

  test('all dates are in YYYY-MM-DD format', () => {
    const datePattern = /^\d{4}-\d{2}-\d{2}$/
    const nonNullPresets: DatePreset[] = [
      'this_week',
      'this_month',
      'last_month',
      'last_3_months',
      'last_6_months',
      'ytd',
    ]
    for (const preset of nonNullPresets) {
      const range = getDateRange(preset)
      expect(range!.startDate).toMatch(datePattern)
      expect(range!.endDate).toMatch(datePattern)
    }
  })
})

/**
 * A stand-in for `new Date()` whose UTC calendar date and LOCAL calendar date deliberately
 * DISAGREE. Only the getters a UTC-based derivation needs are real; the local-field getters
 * return the skewed date, so a preset that reads local fields instead of UTC ones produces a
 * visibly wrong answer instead of an answer that happens to be right because the test runner
 * is in UTC (`bun test` defaults TZ to UTC, and switching `process.env.TZ` mid-file was
 * verified NOT to take effect once another test in the same file has already resolved a
 * timezone — so an ambient-TZ test here would be vacuous).
 */
function skewedNow(
  utc: [number, number, number],
  local: [number, number, number]
): Date {
  return {
    getUTCFullYear: () => utc[0],
    getUTCMonth: () => utc[1] - 1,
    getUTCDate: () => utc[2],
    getFullYear: () => local[0],
    getMonth: () => local[1] - 1,
    getDate: () => local[2],
    getTime: () => Date.UTC(utc[0], utc[1] - 1, utc[2]),
  } as unknown as Date
}

describe('presets resolve "today" in UTC, and this_month stops there (#1626 Phase 3)', () => {
  // The backend parses YYYY-MM-DD to UTC midnight and buckets on UTC calendar dates, so a
  // browser-local "today" and the backend's UTC "today" disagree by up to a day near the
  // boundary. Every preset therefore derives its "today" from the UTC calendar date.

  test('this_month ends TODAY, not on the last day of the month', () => {
    expect(getDateRange('this_month', new Date('2026-09-09T12:00:00.000Z'))).toEqual({
      startDate: '2026-09-01',
      endDate: '2026-09-09',
    })
  })

  test('this_month takes the UTC date where local time is already tomorrow (UTC+13)', () => {
    expect(
      getDateRange('this_month', skewedNow([2026, 9, 9], [2026, 9, 10]))
    ).toEqual({ startDate: '2026-09-01', endDate: '2026-09-09' })
  })

  test('this_month takes the UTC date where local time is still yesterday (UTC-11)', () => {
    expect(
      getDateRange('this_month', skewedNow([2026, 9, 9], [2026, 9, 8]))
    ).toEqual({ startDate: '2026-09-01', endDate: '2026-09-09' })
  })

  test('a UTC+13 month boundary does not roll this_month into the next month', () => {
    expect(
      getDateRange('this_month', skewedNow([2026, 9, 30], [2026, 10, 1]))
    ).toEqual({ startDate: '2026-09-01', endDate: '2026-09-30' })
  })

  test('a UTC-11 month boundary does not roll this_month into the previous month', () => {
    expect(
      getDateRange('this_month', skewedNow([2026, 9, 1], [2026, 8, 31]))
    ).toEqual({ startDate: '2026-09-01', endDate: '2026-09-01' })
  })

  test('every today-ending preset reports the UTC date, not the local one', () => {
    const now = skewedNow([2026, 9, 9], [2026, 9, 10]) // UTC Wed 9th, local Thu 10th
    expect(getDateRange('this_week', now)).toEqual({
      startDate: '2026-09-07', // the Monday of the UTC date's week
      endDate: '2026-09-09',
    })
    expect(getDateRange('last_3_months', now)).toEqual({
      startDate: '2026-06-09',
      endDate: '2026-09-09',
    })
    expect(getDateRange('last_6_months', now)!.endDate).toBe('2026-09-09')
    expect(getDateRange('ytd', now)).toEqual({
      startDate: '2026-01-01',
      endDate: '2026-09-09',
    })
  })

  // The skew here crosses a MONTH boundary on purpose. last_month only ever reads the month of
  // "today", so a same-month skew (UTC the 9th, local the 10th) returns the same answer either
  // way and could not tell a UTC derivation from a local one.
  test('last_month stays entirely in the past, resolved on the UTC month', () => {
    expect(
      getDateRange('last_month', skewedNow([2026, 10, 1], [2026, 9, 30]))
    ).toEqual({ startDate: '2026-09-01', endDate: '2026-09-30' })
  })

  test('the ambient clock path (no injected now) also stops at today', () => {
    setSystemTime(new Date('2026-09-09T12:00:00.000Z'))
    try {
      expect(getDateRange('this_month')!.endDate).toBe('2026-09-09')
    } finally {
      setSystemTime()
    }
  })
})
