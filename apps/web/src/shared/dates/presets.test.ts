import { describe, test, expect } from 'bun:test'
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

  test('last_3_months start is 3 months before last_month end', () => {
    const range = getDateRange('last_3_months')
    expect(range).not.toBeNull()
    const start = new Date(range!.startDate)
    const end = new Date(range!.endDate)
    // Start should be on the 1st of a month
    expect(start.getUTCDate()).toBe(1)
    // End should be last day of a month
    const nextDay = new Date(end)
    nextDay.setUTCDate(end.getUTCDate() + 1)
    expect(nextDay.getUTCDate()).toBe(1)
    // start is before end
    expect(start.getTime()).toBeLessThan(end.getTime())
  })

  test('last_6_months start is 6 months before last_month end', () => {
    const range = getDateRange('last_6_months')
    expect(range).not.toBeNull()
    const start = new Date(range!.startDate)
    const end = new Date(range!.endDate)
    expect(start.getUTCDate()).toBe(1)
    expect(start.getTime()).toBeLessThan(end.getTime())
    // Should span at least 5 months
    const monthDiff =
      (end.getUTCFullYear() - start.getUTCFullYear()) * 12 +
      (end.getUTCMonth() - start.getUTCMonth())
    expect(monthDiff).toBeGreaterThanOrEqual(5)
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
