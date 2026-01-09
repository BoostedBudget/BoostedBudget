/**
 * useBudgetFilters Hook Tests
 * Issue #590: Inline Column Filtering
 *
 * Tests the budget filter hook with URL state synchronization.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import React from 'react'
import { useBudgetFilters } from './useBudgetFilters'

// Wrapper component with Router for useSearchParams
function createWrapper() {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(BrowserRouter, null, children)
  }
}

describe('useBudgetFilters', () => {
  beforeEach(() => {
    // Reset URL before each test
    window.history.replaceState({}, '', '/')
  })

  afterEach(() => {
    // mocks restored in afterEach
  })

  describe('Initialization', () => {
    it('should initialize with empty filters when no URL params', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters).toEqual({
        period: [],
      })
      expect(result.current.periodFilter).toEqual([])
      expect(result.current.progressRange).toBeNull()
      expect(result.current.activeFilterCount).toBe(0)
      expect(result.current.hasActiveFilters).toBe(false)
    })

    it('should parse period filter from URL', () => {
      window.history.replaceState({}, '', '/?period=monthly,weekly')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.period).toEqual(['monthly', 'weekly'])
      expect(result.current.periodFilter).toEqual(['monthly', 'weekly'])
    })

    it('should parse progress range from URL', () => {
      window.history.replaceState({}, '', '/?progress_min=0&progress_max=100')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.progressMin).toBe(0)
      expect(result.current.filters.progressMax).toBe(100)
      expect(result.current.progressRange).toEqual({ min: 0, max: 100 })
    })

    it('should parse progress_min only', () => {
      window.history.replaceState({}, '', '/?progress_min=50')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.progressMin).toBe(50)
      expect(result.current.filters.progressMax).toBeUndefined()
      expect(result.current.progressRange).toEqual({ min: 50, max: undefined })
    })

    it('should parse progress_max only', () => {
      window.history.replaceState({}, '', '/?progress_max=80')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.progressMin).toBeUndefined()
      expect(result.current.filters.progressMax).toBe(80)
      expect(result.current.progressRange).toEqual({ min: undefined, max: 80 })
    })

    it('should handle invalid progress values gracefully', () => {
      window.history.replaceState({}, '', '/?progress_min=invalid&progress_max=not-a-number')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.progressMin).toBeUndefined()
      expect(result.current.filters.progressMax).toBeUndefined()
      expect(result.current.progressRange).toBeNull()
    })

    it('should trim whitespace from filter values', () => {
      window.history.replaceState({}, '', '/?period=%20monthly%20,%20weekly%20')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.period).toEqual(['monthly', 'weekly'])
    })

    it('should filter out empty values', () => {
      window.history.replaceState({}, '', '/?period=monthly,,weekly,')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.period).toEqual(['monthly', 'weekly'])
    })

    it('should handle decimal progress values', () => {
      window.history.replaceState({}, '', '/?progress_min=25.5&progress_max=75.5')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.progressMin).toBe(25.5)
      expect(result.current.filters.progressMax).toBe(75.5)
    })
  })

  describe('setPeriodFilter', () => {
    it('should update URL when setting period filter', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPeriodFilter(['monthly', 'quarterly'])
      })

      expect(result.current.periodFilter).toEqual(['monthly', 'quarterly'])
      expect(window.location.search).toContain('period=monthly%2Cquarterly')
    })

    it('should remove param from URL when array is empty', () => {
      window.history.replaceState({}, '', '/?period=monthly')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPeriodFilter([])
      })

      expect(result.current.periodFilter).toEqual([])
      expect(window.location.search).not.toContain('period')
    })
  })

  describe('setProgressRange', () => {
    it('should update URL when setting progress range', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setProgressRange({ min: 0, max: 100 })
      })

      expect(result.current.progressRange).toEqual({ min: 0, max: 100 })
      expect(window.location.search).toContain('progress_min=0')
      expect(window.location.search).toContain('progress_max=100')
    })

    it('should handle min only', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setProgressRange({ min: 50 })
      })

      expect(window.location.search).toContain('progress_min=50')
      expect(window.location.search).not.toContain('progress_max')
    })

    it('should handle max only', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setProgressRange({ max: 80 })
      })

      expect(window.location.search).not.toContain('progress_min')
      expect(window.location.search).toContain('progress_max=80')
    })

    it('should clear progress params when setting null', () => {
      window.history.replaceState({}, '', '/?progress_min=0&progress_max=100')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setProgressRange(null)
      })

      expect(result.current.progressRange).toBeNull()
      expect(window.location.search).not.toContain('progress_min')
      expect(window.location.search).not.toContain('progress_max')
    })

    it('should clear progress params when both min and max are undefined', () => {
      window.history.replaceState({}, '', '/?progress_min=0&progress_max=100')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setProgressRange({ min: undefined, max: undefined })
      })

      expect(window.location.search).not.toContain('progress_min')
      expect(window.location.search).not.toContain('progress_max')
    })
  })

  describe('clearAllFilters', () => {
    it('should clear all filters and reset URL', () => {
      window.history.replaceState(
        {},
        '',
        '/?period=monthly&progress_min=0&progress_max=100'
      )

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(result.current.filters).toEqual({
        period: [],
      })
      expect(result.current.activeFilterCount).toBe(0)
      expect(window.location.search).toBe('')
    })

    it('should preserve other URL params', () => {
      window.history.replaceState({}, '', '/?period=monthly&sort=name&order=asc')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(window.location.search).not.toContain('period')
      expect(window.location.search).toContain('sort=name')
      expect(window.location.search).toContain('order=asc')
    })
  })

  describe('activeFilterCount', () => {
    it('should calculate activeFilterCount correctly', () => {
      window.history.replaceState(
        {},
        '',
        '/?period=monthly&progress_min=0'
      )

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      // period=1, progress range=1 = 2
      expect(result.current.activeFilterCount).toBe(2)
      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('should count period filter as one regardless of array length', () => {
      window.history.replaceState({}, '', '/?period=monthly,weekly,quarterly')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count progress range as single filter', () => {
      window.history.replaceState({}, '', '/?progress_min=0&progress_max=100')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count only progress_min as single filter', () => {
      window.history.replaceState({}, '', '/?progress_min=50')

      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })
  })

  describe('URL State Persistence', () => {
    it('should maintain filter state across re-renders', () => {
      window.history.replaceState({}, '', '/?period=monthly')

      const { result, rerender } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.periodFilter).toEqual(['monthly'])

      rerender()

      expect(result.current.periodFilter).toEqual(['monthly'])
    })
  })
})
