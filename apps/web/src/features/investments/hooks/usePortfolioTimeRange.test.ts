/**
 * usePortfolioTimeRange Hook Tests
 *
 * Issue #1296: Preset-based time range hook for portfolio charts.
 * Tests preset-to-date mapping, the 'All' floor date, and state updates.
 */

import { describe, it, expect } from 'bun:test'
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
})
