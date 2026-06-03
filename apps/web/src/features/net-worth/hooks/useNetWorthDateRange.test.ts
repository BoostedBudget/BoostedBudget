import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useNetWorthDateRange } from './useNetWorthDateRange'

describe('useNetWorthDateRange', () => {
  it('defaults to last_3_months preset', () => {
    const { result } = renderHook(() => useNetWorthDateRange())
    expect(result.current.preset).toBe('last_3_months')
    expect(result.current.startDate).toBeTruthy()
    expect(result.current.endDate).toBeTruthy()
  })

  it('setPreset updates preset and resolves dates', () => {
    const { result } = renderHook(() => useNetWorthDateRange())
    act(() => {
      result.current.setPreset('ytd')
    })
    expect(result.current.preset).toBe('ytd')
    expect(result.current.startDate).toBeTruthy()
    expect(result.current.endDate).toBeTruthy()
  })

  it('setPreset("all") clears start and end dates', () => {
    const { result } = renderHook(() => useNetWorthDateRange())
    act(() => {
      result.current.setPreset('all')
    })
    expect(result.current.preset).toBe('all')
    expect(result.current.startDate).toBe('')
    expect(result.current.endDate).toBe('')
  })

  it('custom preset keeps stale dates until setCustomRange is called', () => {
    const { result } = renderHook(() => useNetWorthDateRange())
    const initialStart = result.current.startDate
    act(() => {
      result.current.setPreset('custom')
    })
    // dates unchanged until explicitly set
    expect(result.current.preset).toBe('custom')
    expect(result.current.startDate).toBe(initialStart)

    act(() => {
      result.current.setCustomRange('2024-01-01', '2024-03-31')
    })
    expect(result.current.startDate).toBe('2024-01-01')
    expect(result.current.endDate).toBe('2024-03-31')
  })

  it('returns DashboardFilters-compatible shape', () => {
    const { result } = renderHook(() => useNetWorthDateRange())
    expect(typeof result.current.startDate).toBe('string')
    expect(typeof result.current.endDate).toBe('string')
    expect(typeof result.current.setPreset).toBe('function')
    expect(typeof result.current.setCustomRange).toBe('function')
  })
})
