import { describe, test, expect } from 'bun:test'
import { forwardFillByDay, forwardFillCutoff, todayDateKeyUTC } from './forwardFill'

interface Row {
  date: string
  value: number
}

const carry = (previous: Row, dateKey: string): Row => ({ date: dateKey, value: previous.value })

// "today" is injected everywhere below, so these assertions are exact rather than relative to
// whatever day the suite happens to run on.
const TODAY = '2026-09-09'

describe('todayDateKeyUTC', () => {
  test('reports the UTC calendar date of the given instant', () => {
    // 23:30Z is already the 10th in UTC+13 and still the 9th in UTC-11; the key is neither.
    expect(todayDateKeyUTC(new Date('2026-09-09T23:30:00.000Z'))).toBe('2026-09-09')
    expect(todayDateKeyUTC(new Date('2026-09-09T00:30:00.000Z'))).toBe('2026-09-09')
  })
})

describe('forwardFillCutoff', () => {
  test('is the newest observation when the data stops before today', () => {
    expect(forwardFillCutoff(['2026-09-01', '2026-09-05'], TODAY)).toBe('2026-09-05')
  })

  test('is today when an observation is dated in the future', () => {
    expect(forwardFillCutoff(['2026-09-05', '2026-09-30'], TODAY)).toBe(TODAY)
  })

  test('is today when the newest observation IS today', () => {
    expect(forwardFillCutoff(['2026-09-05', TODAY], TODAY)).toBe(TODAY)
  })

  test('does not assume the keys arrive in order', () => {
    expect(forwardFillCutoff(['2026-09-05', '2026-09-01', '2026-09-03'], TODAY)).toBe('2026-09-05')
  })

  test('is null when there is nothing to fill from', () => {
    expect(forwardFillCutoff([], TODAY)).toBeNull()
  })
})

describe('forwardFillByDay', () => {
  test('fills the gaps between observations', () => {
    const rows: Row[] = [
      { date: '2026-09-01', value: 1 },
      { date: '2026-09-04', value: 4 },
    ]
    expect(forwardFillByDay(rows, '2026-09-01', '2026-09-04', carry, TODAY)).toEqual([
      { date: '2026-09-01', value: 1 },
      { date: '2026-09-02', value: 1 },
      { date: '2026-09-03', value: 1 },
      { date: '2026-09-04', value: 4 },
    ])
  })

  test('stops at the newest observation rather than at a future end date', () => {
    const rows: Row[] = [{ date: '2026-09-01', value: 1 }, { date: '2026-09-02', value: 2 }]
    expect(
      forwardFillByDay(rows, '2026-09-01', '2026-09-30', carry, TODAY).map((r) => r.date)
    ).toEqual(['2026-09-01', '2026-09-02'])
  })

  test('stops at today, and keeps a future observation without ramping up to it', () => {
    const rows: Row[] = [
      { date: '2026-09-07', value: 7 },
      { date: '2026-09-12', value: 12 },
    ]
    expect(forwardFillByDay(rows, '2026-09-07', '2026-09-30', carry, TODAY)).toEqual([
      { date: '2026-09-07', value: 7 },
      { date: '2026-09-08', value: 7 },
      { date: '2026-09-09', value: 7 },
      // No 10th and no 11th: those days are not data, and the 12th is.
      { date: '2026-09-12', value: 12 },
    ])
  })

  test('never fills past end_date when end_date is the earlier bound', () => {
    const rows: Row[] = [{ date: '2026-09-01', value: 1 }, { date: '2026-09-08', value: 8 }]
    expect(
      forwardFillByDay(rows, '2026-09-01', '2026-09-03', carry, TODAY).map((r) => r.date)
    ).toEqual(['2026-09-01', '2026-09-02', '2026-09-03', '2026-09-08'])
  })

  test('skips days before the first observation instead of back-filling them', () => {
    const rows: Row[] = [{ date: '2026-09-05', value: 5 }]
    expect(
      forwardFillByDay(rows, '2026-09-01', '2026-09-06', carry, TODAY).map((r) => r.date)
    ).toEqual(['2026-09-05'])
  })

  test('returns nothing for an empty series', () => {
    expect(forwardFillByDay([], '2026-09-01', '2026-09-30', carry, TODAY)).toEqual([])
  })

  test('accepts RFC3339 observation dates and keys them by UTC calendar day', () => {
    const rows: Row[] = [
      { date: '2026-09-07T06:00:00Z', value: 7 },
      { date: '2026-09-09', value: 9 },
    ]
    expect(forwardFillByDay(rows, '2026-09-07', '2026-09-09', carry, TODAY)).toEqual([
      { date: '2026-09-07T06:00:00Z', value: 7 },
      { date: '2026-09-08', value: 7 },
      { date: '2026-09-09', value: 9 },
    ])
  })
})
