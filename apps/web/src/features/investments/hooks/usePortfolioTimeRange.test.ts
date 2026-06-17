/**
 * usePortfolioTimeRange Hook Tests
 *
 * Issue #1296: Preset-based time range hook for portfolio charts.
 * Tests preset-to-date mapping, the 'All' floor date, and state updates.
 */

import { describe, it, expect, setSystemTime, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { usePortfolioTimeRange, PORTFOLIO_PRESETS, PORTFOLIO_ALL_FLOOR } from './usePortfolioTimeRange'

describe('usePortfolioTimeRange', () => {
  it('defaults to 1Y preset', () => {
    const { result } = renderHook(() => usePortfolioTimeRange())
    expect(result.current.preset).toBe('1Y')
  })

  it('returns non-empty start_date and end_date for all presets', () => {
    for (const preset of PORTFOLIO_PRESETS) {
      const { result } = renderHook(() => usePortfolioTimeRange())
      act(() => {
        result.current.setPreset(preset)
      })
      const { start_date, end_date } = result.current.dateRange
      expect(start_date).toBeTruthy()
      expect(end_date).toBeTruthy()
      expect(start_date.length).toBe(10) // YYYY-MM-DD
      expect(end_date.length).toBe(10)
    }
  })

  it('resolves All preset to concrete floor date (not empty)', () => {
    const { result } = renderHook(() => usePortfolioTimeRange())
    act(() => {
      result.current.setPreset('All')
    })
    expect(result.current.dateRange.start_date).toBe(PORTFOLIO_ALL_FLOOR)
    expect(result.current.dateRange.end_date).not.toBe('')
  })

  it('resolves 1W preset to a date 7 days ago', () => {
    const { result } = renderHook(() => usePortfolioTimeRange())
    act(() => {
      result.current.setPreset('1W')
    })
    const start = new Date(result.current.dateRange.start_date)
    const end = new Date(result.current.dateRange.end_date)
    const diffDays = Math.round((end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))
    // 1W = ~7 days (exact depends on today — allow ±1 for DST boundaries)
    expect(diffDays).toBeGreaterThanOrEqual(6)
    expect(diffDays).toBeLessThanOrEqual(8)
  })

  it('resolves YTD preset to Jan 1 of the current year', () => {
    const { result } = renderHook(() => usePortfolioTimeRange())
    act(() => {
      result.current.setPreset('YTD')
    })
    const start = result.current.dateRange.start_date
    const year = new Date().getFullYear()
    expect(start).toBe(`${year}-01-01`)
  })

  it('setPreset updates the active preset', () => {
    const { result } = renderHook(() => usePortfolioTimeRange())
    act(() => {
      result.current.setPreset('3M')
    })
    expect(result.current.preset).toBe('3M')
  })

  it('date range changes when preset changes', () => {
    const { result } = renderHook(() => usePortfolioTimeRange())

    act(() => { result.current.setPreset('1M') })
    const range1M = result.current.dateRange.start_date

    act(() => { result.current.setPreset('1Y') })
    const range1Y = result.current.dateRange.start_date

    // 1Y start should be earlier than 1M start
    expect(range1Y < range1M).toBe(true)
  })

  describe('month-end date clamping (regression: setMonth() overflow)', () => {
    afterEach(() => {
      // Restore real clock after each frozen-time test
      setSystemTime()
    })

    it('1M on March 31 clamps to Feb 28, not Mar 03', () => {
      // Use explicit UTC time (15:00Z = 11 AM EDT / 10 AM EST) so getDate() returns
      // 31 in every US timezone — date-only '2026-03-31' parses as UTC midnight, which
      // is March 30 local time in UTC-4 (EDT).
      setSystemTime(new Date('2026-03-31T15:00:00Z'))
      const { result } = renderHook(() => usePortfolioTimeRange('1M'))
      // Feb 2026 has 28 days — must NOT overflow to March
      expect(result.current.dateRange.start_date).toBe('2026-02-28')
      expect(result.current.dateRange.end_date).toBe('2026-03-31')
    })

    it('3M on May 31 clamps to Feb 28', () => {
      setSystemTime(new Date('2026-05-31T15:00:00Z'))
      const { result } = renderHook(() => usePortfolioTimeRange('3M'))
      expect(result.current.dateRange.start_date).toBe('2026-02-28')
      expect(result.current.dateRange.end_date).toBe('2026-05-31')
    })

    it('1M on a mid-month date is exact (no clamping needed)', () => {
      setSystemTime(new Date('2026-06-14T15:00:00Z'))
      const { result } = renderHook(() => usePortfolioTimeRange('1M'))
      expect(result.current.dateRange.start_date).toBe('2026-05-14')
    })

    it('1Y on leap day Feb 29 clamps to Feb 28 of prior non-leap year', () => {
      // 2028 is a leap year. Subtracting 1Y from 2028-02-29 via setFullYear produces
      // 2027-03-01 (overflow) — should clamp to 2027-02-28 instead.
      setSystemTime(new Date('2028-02-29T15:00:00Z'))
      const { result } = renderHook(() => usePortfolioTimeRange('1Y'))
      expect(result.current.dateRange.start_date).toBe('2027-02-28')
      expect(result.current.dateRange.end_date).toBe('2028-02-29')
    })
  })
})
