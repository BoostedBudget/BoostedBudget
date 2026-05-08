import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { createElement } from 'react'

const { useNetWorthBreakdownFilters } = await import('./useNetWorthBreakdownFilters')

function wrapper(initialPath = '/net-worth') {
  return ({ children }: { children: React.ReactNode }) =>
    createElement(MemoryRouter, { initialEntries: [initialPath] }, children)
}

describe('useNetWorthBreakdownFilters', () => {
  it('returns empty filters and null sort by default', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper(),
    })
    expect(result.current.institutionFilter).toEqual([])
    expect(result.current.typeFilter).toEqual([])
    expect(result.current.balanceRange).toBeNull()
    expect(result.current.sortColumn).toBeNull()
    expect(result.current.sortDirection).toBe('asc')
    expect(result.current.hasActiveFilters).toBe(false)
    expect(result.current.activeFilterCount).toBe(0)
  })

  it('parses institution filter from URL', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?institution=Chase,Citibank'),
    })
    expect(result.current.institutionFilter).toEqual(['Chase', 'Citibank'])
  })

  it('parses type filter from URL', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?type=depository,credit'),
    })
    expect(result.current.typeFilter).toEqual(['depository', 'credit'])
  })

  it('parses balance range from URL', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?balance_min=1000&balance_max=50000'),
    })
    expect(result.current.balanceRange).toEqual({ min: 1000, max: 50000 })
  })

  it('parses sort column and direction from URL', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?sort=balance%3Adesc'),
    })
    expect(result.current.sortColumn).toBe('balance')
    expect(result.current.sortDirection).toBe('desc')
  })

  it('setInstitutionFilter updates state', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper(),
    })
    act(() => {
      result.current.setInstitutionFilter(['Chase'])
    })
    expect(result.current.institutionFilter).toEqual(['Chase'])
  })

  it('setSortColumn toggles direction when same column re-clicked', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?sort=balance%3Aasc'),
    })
    expect(result.current.sortDirection).toBe('asc')
    act(() => {
      result.current.setSortColumn('balance')
    })
    expect(result.current.sortColumn).toBe('balance')
    expect(result.current.sortDirection).toBe('desc')
  })

  it('setSortColumn sets asc when switching to a new column', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?sort=balance%3Adesc'),
    })
    act(() => {
      result.current.setSortColumn('institution')
    })
    expect(result.current.sortColumn).toBe('institution')
    expect(result.current.sortDirection).toBe('asc')
  })

  it('clearAllFilters resets everything', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?institution=Chase&type=credit&balance_min=1000&sort=balance%3Adesc'),
    })
    act(() => {
      result.current.clearAllFilters()
    })
    expect(result.current.institutionFilter).toEqual([])
    expect(result.current.typeFilter).toEqual([])
    expect(result.current.balanceRange).toBeNull()
    expect(result.current.sortColumn).toBeNull()
  })

  it('hasActiveFilters is true when any filter is set', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?institution=Chase'),
    })
    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('activeFilterCount counts institution, type, and balance separately', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?institution=Chase&type=credit&balance_min=500'),
    })
    expect(result.current.activeFilterCount).toBe(3)
  })

  it('ignores invalid balance_min values gracefully', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?balance_min=abc&balance_max=xyz'),
    })
    expect(result.current.balanceRange).toBeNull()
  })

  it('ignores invalid sort format gracefully', () => {
    const { result } = renderHook(() => useNetWorthBreakdownFilters(), {
      wrapper: wrapper('/net-worth?sort=badvalue'),
    })
    expect(result.current.sortColumn).toBeNull()
  })
})
