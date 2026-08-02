import { describe, test, expect, beforeAll, afterAll, setSystemTime } from 'bun:test'
import {
  inferPreset,
  formatDateForAPI,
  historyDateKey,
  formatHistoryLabel,
  calendarDateRange,
  isParsableDate,
  resolveServerSnapshotTime,
} from './utils'

// Tests ensure TZ-safe history helpers (Bug C regression, Issue #1173).
// historyDateKey and formatHistoryLabel must produce UTC calendar dates regardless
// of local timezone. RFC3339 midnight-UTC dates (e.g., 2026-04-29T00:00:00Z) must
// not shift back one day in negative-offset timezones (US Eastern, etc.).

describe('formatDateForAPI', () => {
  test('formats Date to YYYY-MM-DD', () => {
    const d = new Date(2024, 2, 5) // March 5, 2024 local
    const result = formatDateForAPI(d)
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(result).toBe('2024-03-05')
  })

  test('pads single-digit month and day', () => {
    const d = new Date(2024, 0, 9) // Jan 9
    expect(formatDateForAPI(d)).toBe('2024-01-09')
  })
})

describe('inferPreset', () => {
  test('no dates returns all', () => {
    expect(inferPreset(undefined, undefined)).toBe('all')
    expect(inferPreset('', '')).toBe('all')
  })

  test('this_month: start = first of current month, end = last day of current month', () => {
    // Get this month boundaries
    const now = new Date()
    const firstOfMonth = new Date(now.getFullYear(), now.getMonth(), 1)
    const lastOfMonth = new Date(now.getFullYear(), now.getMonth() + 1, 0)
    const start = formatDateForAPI(firstOfMonth)
    const end = formatDateForAPI(lastOfMonth)
    expect(inferPreset(start, end)).toBe('this_month')
  })

  test('last_month: start = first of prev month, end = last of prev month', () => {
    const now = new Date()
    const firstOfPrev = new Date(now.getFullYear(), now.getMonth() - 1, 1)
    const lastOfPrev = new Date(now.getFullYear(), now.getMonth(), 0)
    const start = formatDateForAPI(firstOfPrev)
    const end = formatDateForAPI(lastOfPrev)
    expect(inferPreset(start, end)).toBe('last_month')
  })

  test('ytd: Jan 1 of current year to today', () => {
    // Frozen to Mar 15 where YTD (Jan 1 → Mar 15) differs from last_6_months
    // (Sep 15 → Mar 15), preventing the later preset from shadowing YTD.
    // try/finally ensures clock is always restored even if the assertion throws.
    setSystemTime(new Date('2026-03-15T12:00:00.000Z'))
    try {
      const start = `${new Date().getFullYear()}-01-01`
      const frozenToday = formatDateForAPI(new Date())
      expect(inferPreset(start, frozenToday)).toBe('ytd')
    } finally {
      setSystemTime() // restore real clock
    }
  })

  test('unrecognized range returns custom', () => {
    expect(inferPreset('2020-01-01', '2020-06-15')).toBe('custom')
  })

  test('end in the future returns custom', () => {
    const future = `${new Date().getFullYear() + 1}-01-01`
    expect(inferPreset('2024-01-01', future)).toBe('custom')
  })
})

// TZ-safe history helpers — Bug C regression tests (Issue #1173).
// historyDateKey and formatHistoryLabel must produce UTC calendar dates
// regardless of the local timezone offset.
describe('historyDateKey', () => {
  test('extracts UTC date from RFC3339 timestamp', () => {
    expect(historyDateKey('2026-04-29T00:00:00Z')).toBe('2026-04-29')
  })

  test('does not shift date for negative UTC offset', () => {
    expect(historyDateKey('2026-04-29T00:00:00Z')).toBe('2026-04-29')
  })

  test('handles date-only strings', () => {
    expect(historyDateKey('2024-01-15')).toBe('2024-01-15')
  })
})

describe('formatHistoryLabel', () => {
  // Freeze to noon UTC to test TZ safety (avoids DST edge cases)
  beforeAll(() => {
    setSystemTime(new Date('2026-04-29T12:00:00.000Z'))
  })
  afterAll(() => {
    setSystemTime()
  })

  test('formats RFC3339 as "MMM d" using UTC calendar date', () => {
    expect(formatHistoryLabel('2026-04-29T00:00:00Z', 'MMM d')).toBe('Apr 29')
  })

  test('formats RFC3339 as "MMMM d, yyyy" for tooltip', () => {
    expect(formatHistoryLabel('2026-04-29T00:00:00Z', 'MMMM d, yyyy')).toBe('April 29, 2026')
  })

  test('formats date-only string correctly', () => {
    expect(formatHistoryLabel('2024-01-15', 'MMM d')).toBe('Jan 15')
  })
})

describe('calendarDateRange', () => {
  test('generates all dates in an inclusive range (RFC3339)', () => {
    const range = calendarDateRange('2024-04-28T00:00:00Z', '2024-04-30T00:00:00Z')
    expect(range).toEqual(['2024-04-28', '2024-04-29', '2024-04-30'])
  })

  test('returns a single date when start equals end', () => {
    const range = calendarDateRange('2024-01-15T00:00:00Z', '2024-01-15T00:00:00Z')
    expect(range).toEqual(['2024-01-15'])
  })

  test('returns empty array when start is after end', () => {
    const range = calendarDateRange('2024-04-30T00:00:00Z', '2024-04-28T00:00:00Z')
    expect(range).toEqual([])
  })

  test('works with date-only strings', () => {
    const range = calendarDateRange('2024-06-01', '2024-06-03')
    expect(range).toEqual(['2024-06-01', '2024-06-02', '2024-06-03'])
  })

  test('works correctly across month boundary', () => {
    const range = calendarDateRange('2024-01-30T00:00:00Z', '2024-02-02T00:00:00Z')
    expect(range).toEqual(['2024-01-30', '2024-01-31', '2024-02-01', '2024-02-02'])
  })

  test('works correctly across year boundary', () => {
    const range = calendarDateRange('2023-12-30T00:00:00Z', '2024-01-02T00:00:00Z')
    expect(range).toEqual(['2023-12-30', '2023-12-31', '2024-01-01', '2024-01-02'])
  })
})

// Defect R3-4: a malformed-but-numeric `as_of` (e.g. '0', '1', '2026') is accepted by a bare
// `new Date(x)` parse check — JS Date happily interprets these as years 2000/2001/2026 rather
// than rejecting them — so isParsableDate previously judged them "parsable" and
// resolveServerSnapshotTime silently anchored ALL override-liveness arithmetic in the admin
// surfaces to that bogus instant, with no "Invalid Date" tell to reveal it. That is strictly
// worse than the browser-clock fallback this helper exists to provide. The wire `as_of` field is
// OpenAPI `format: date-time` — always a full RFC3339 instant — so the fix is ISO-strict: only a
// string with the RFC3339 date-time shape (a literal 'T' separating the date and time parts) that
// also parses to a valid instant counts as parsable; anything else, however numeric-looking,
// fails closed to the browser clock.
describe('isParsableDate', () => {
  test.each([
    ['0', false],
    ['1', false],
    ['2026', false],
    ['08/02/2026', false],
    ['', false],
    ['   ', false],
    ['Invalid Date', false],
    ['not-a-real-date', false],
    [null, false],
    [undefined, false],
    ['2026-08-02T14:30:00Z', true],
  ] as const)('isParsableDate(%p) -> %p', (input, expected) => {
    expect(isParsableDate(input)).toBe(expected)
  })
})

describe('resolveServerSnapshotTime', () => {
  const frozenNow = new Date('2026-08-02T18:00:00.000Z')

  beforeAll(() => {
    setSystemTime(frozenNow)
  })
  afterAll(() => {
    setSystemTime()
  })

  test('anchors to the parsed instant for a valid RFC3339 as_of', () => {
    const result = resolveServerSnapshotTime('2026-01-15T09:00:00Z')
    expect(result.toISOString()).toBe('2026-01-15T09:00:00.000Z')
  })

  // None of these malformed-but-truthy values may anchor liveness arithmetic to a bogus instant
  // (the year-2000 hazard above) — every one must fail closed to the current (frozen) browser
  // clock instead.
  test.each([
    ['0'],
    ['1'],
    ['2026'],
    ['08/02/2026'],
    [''],
    ['   '],
    ['Invalid Date'],
    ['not-a-real-date'],
    [null],
    [undefined],
  ] as const)('falls back to the browser clock, never a bogus anchor, for %p', (input) => {
    const result = resolveServerSnapshotTime(input)
    expect(result.getTime()).toBe(frozenNow.getTime())
  })
})
