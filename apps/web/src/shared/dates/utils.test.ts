import { describe, test, expect, beforeAll, afterAll, setSystemTime } from 'bun:test'
import {
  inferPreset,
  formatDateForAPI,
  historyDateKey,
  formatHistoryLabel,
  calendarDateRange,
  isParsableDate,
  parseServerDateTime,
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

  // this_month now ends on TODAY (UTC), not on the last day of the month (#1626 Phase 3).
  // Both bounds are derived from the UTC calendar date because that is what the preset emits.
  test('this_month: start = first of current UTC month, end = today (UTC)', () => {
    const todayUTC = new Date().toISOString().slice(0, 10)
    const start = todayUTC.slice(0, 8) + '01'
    expect(inferPreset(start, todayUTC)).toBe('this_month')
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

// Defect R3-4 (Issue #1432 follow-up S4): a malformed-but-numeric `as_of` (e.g. '0', '1', '2026')
// is accepted by a bare `new Date(x)` parse check — JS Date happily interprets these as years
// 2000/2001/2026 rather than rejecting them — so isParsableDate previously judged them
// "parsable" and resolveServerSnapshotTime silently anchored ALL override-liveness arithmetic in
// the admin surfaces to that bogus instant, with no "Invalid Date" tell to reveal it. That is
// strictly worse than the browser-clock fallback this helper exists to provide.
//
// The wire `as_of` field is a Go `time.Time`, JSON-marshaled via `encoding/json` — NOT full
// RFC3339 (see GO_TIME_JSON_PATTERN in utils.ts for exactly what that wire format does and does
// not permit, and why). isParsableDate enforces that exact shape (4-digit year, zero-padded
// month/day, literal 'T', zero-padded HH:MM:SS with seconds never omitted, optional 1-9-digit
// fraction, and a mandatory 'Z' or numeric '+HH:MM'/'-HH:MM' offset), each numeric field
// bounds-checked (hour 00-23, minute/second 00-59, offset hour 00-23, offset minute 00-59),
// followed by a semantic calendar check (month 1-12, day-of-month including leap years) that no
// regex alone can express. A partial value (no seconds, no offset) is rejected rather than
// silently defaulting to browser-local time — the exact "narrower form of the same bug" this
// round closes: a partial timestamp with no offset would otherwise be interpreted in the
// BROWSER's zone, not the server's.
describe('isParsableDate', () => {
  test.each([
    // Already-covered non-ISO / empty / nullish inputs (Defect R3-4).
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
    // Partial date-times: parseISO alone accepts these, defaulting missing fields to
    // browser-local midnight/zone — exactly the hazard resolveServerSnapshotTime exists to
    // remove, reintroduced in narrower form if a partial value slips through.
    ['2026-08-02T', false],
    ['2026-08-02T14', false],
    ['2026-08-02T14:30', false],
    ['2026-08-02T14:30:00', false], // no offset -> would be interpreted in the browser's zone
    // Out-of-range numeric fields a shape-only regex would miss without explicit bounds.
    ['2026-08-02T14:30:00+24:00', false], // offset hour out of range
    ['2026-08-02T24:00:00Z', false], // hour out of range
    // Trailing garbage after a syntactically valid instant.
    ['2026-08-02T14:30:00Zjunk', false],
    // Comma fractional-seconds separator (RFC3339 §5.6 permits it; Go's encoding/json never
    // emits it) — rejected because it's not a shape this wire produces.
    ['2026-08-02T14:30:00,123Z', false],
    // Calendar-invalid dates a regex alone cannot catch (shape-valid, semantically invalid).
    ['2026-02-30T00:00:00Z', false], // February never has 30 days
    ['2026-13-01T00:00:00Z', false], // month 13 does not exist
    ['2026-02-29T00:00:00Z', false], // 2026 is not a leap year
    // Valid full instants.
    ['2026-08-02T14:30:00Z', true],
    ['2026-08-02T14:30:00+05:30', true],
    ['2026-08-02T14:30:00.123Z', true],
    ['2028-02-29T00:00:00Z', true], // 2028 IS a leap year
  ] as const)('isParsableDate(%p) -> %p', (input, expected) => {
    expect(isParsableDate(input)).toBe(expected)
  })
})

describe('parseServerDateTime', () => {
  test('returns the parsed Date for a valid Go time.Time wire string', () => {
    const result = parseServerDateTime('2026-01-15T09:00:00Z')
    expect(result?.toISOString()).toBe('2026-01-15T09:00:00.000Z')
  })

  test('returns null for a value isParsableDate rejects', () => {
    expect(parseServerDateTime('2026-08-02T14:30:00')).toBeNull()
    expect(parseServerDateTime('2026-02-30T00:00:00Z')).toBeNull()
    expect(parseServerDateTime(null)).toBeNull()
    expect(parseServerDateTime(undefined)).toBeNull()
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

  test('anchors to the parsed instant for a valid Go time.Time wire as_of', () => {
    const result = resolveServerSnapshotTime('2026-01-15T09:00:00Z')
    expect(result.toISOString()).toBe('2026-01-15T09:00:00.000Z')
  })

  // None of these malformed-but-truthy values may anchor liveness arithmetic to a bogus instant
  // (the year-2000 hazard above, or the browser-local-time hazard of a partial value) — every
  // one must fail closed to the current (frozen) browser clock instead.
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
    ['2026-08-02T'],
    ['2026-08-02T14:30:00'],
    ['2026-08-02T14:30:00+24:00'],
    ['2026-08-02T24:00:00Z'],
    ['2026-02-30T00:00:00Z'],
  ] as const)('falls back to the browser clock, never a bogus anchor, for %p', (input) => {
    const result = resolveServerSnapshotTime(input)
    expect(result.getTime()).toBe(frozenNow.getTime())
  })
})
