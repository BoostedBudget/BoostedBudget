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
  test('this_month returns same month boundaries', () => {
    const range = getDateRange('this_month')
    expect(range).not.toBeNull()
    const start = parseDate(range!.startDate)
    const end = parseDate(range!.endDate)
    expect(start.day).toBe(1)
    expect(start.month).toBe(end.month)
    expect(start.year).toBe(end.year)
    // End day should be last day of month (28-31)
    expect(end.day).toBeGreaterThanOrEqual(28)
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
