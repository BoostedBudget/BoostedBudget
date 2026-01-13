/**
 * useBudgetFilters Hook Tests
 * Issue #590: Inline Column Filtering
 * Issue #575: Fixed for bun:test with MemoryRouter
 *
 * Tests the budget filter hook with URL state synchronization.
 *
 * NOTE: This test file passes when run alone (bun test useBudgetFilters.test.ts)
 * but may fail when run in the full suite due to mock pollution from
 * BudgetsPage.test.tsx which runs first alphabetically and mocks useBudgetFilters.
 * This is a known limitation of Bun's mock.module() persistence.
 */

import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import React from 'react'
import { useBudgetFilters } from './useBudgetFilters'

// Wrapper component with MemoryRouter for useSearchParams
// Uses initialEntries to set the initial URL state
function createWrapper(initialEntry = '/') {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(MemoryRouter, { initialEntries: [initialEntry] }, children)
  }
}

describe('useBudgetFilters', () => {

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
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?period=monthly,weekly'),
      })

      expect(result.current.filters.period).toEqual(['monthly', 'weekly'])
      expect(result.current.periodFilter).toEqual(['monthly', 'weekly'])
    })

    it('should parse progress range from URL', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_min=0&progress_max=100'),
      })

      expect(result.current.filters.progressMin).toBe(0)
      expect(result.current.filters.progressMax).toBe(100)
      expect(result.current.progressRange).toEqual({ min: 0, max: 100 })
    })

    it('should parse progress_min only', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_min=50'),
      })

      expect(result.current.filters.progressMin).toBe(50)
      expect(result.current.filters.progressMax).toBeUndefined()
      expect(result.current.progressRange).toEqual({ min: 50, max: undefined })
    })

    it('should parse progress_max only', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_max=80'),
      })

      expect(result.current.filters.progressMin).toBeUndefined()
      expect(result.current.filters.progressMax).toBe(80)
      expect(result.current.progressRange).toEqual({ min: undefined, max: 80 })
    })

    it('should handle invalid progress values gracefully', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_min=invalid&progress_max=not-a-number'),
      })

      expect(result.current.filters.progressMin).toBeUndefined()
      expect(result.current.filters.progressMax).toBeUndefined()
      expect(result.current.progressRange).toBeNull()
    })

    it('should trim whitespace from filter values', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?period=%20monthly%20,%20weekly%20'),
      })

      expect(result.current.filters.period).toEqual(['monthly', 'weekly'])
    })

    it('should filter out empty values', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?period=monthly,,weekly,'),
      })

      expect(result.current.filters.period).toEqual(['monthly', 'weekly'])
    })

    it('should handle decimal progress values', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_min=25.5&progress_max=75.5'),
      })

      expect(result.current.filters.progressMin).toBe(25.5)
      expect(result.current.filters.progressMax).toBe(75.5)
    })
  })

  describe('setPeriodFilter', () => {
    it('should update filter when setting period filter', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPeriodFilter(['monthly', 'quarterly'])
      })

      expect(result.current.periodFilter).toEqual(['monthly', 'quarterly'])
    })

    it('should clear filter when array is empty', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?period=monthly'),
      })

      act(() => {
        result.current.setPeriodFilter([])
      })

      expect(result.current.periodFilter).toEqual([])
    })
  })

  describe('setProgressRange', () => {
    it('should update filter when setting progress range', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setProgressRange({ min: 0, max: 100 })
      })

      expect(result.current.progressRange).toEqual({ min: 0, max: 100 })
    })

    it('should handle min only', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setProgressRange({ min: 50 })
      })

      expect(result.current.filters.progressMin).toBe(50)
      expect(result.current.filters.progressMax).toBeUndefined()
    })

    it('should handle max only', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setProgressRange({ max: 80 })
      })

      expect(result.current.filters.progressMin).toBeUndefined()
      expect(result.current.filters.progressMax).toBe(80)
    })

    it('should clear progress params when setting null', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_min=0&progress_max=100'),
      })

      act(() => {
        result.current.setProgressRange(null)
      })

      expect(result.current.progressRange).toBeNull()
    })

    it('should clear progress params when both min and max are undefined', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_min=0&progress_max=100'),
      })

      act(() => {
        result.current.setProgressRange({ min: undefined, max: undefined })
      })

      expect(result.current.progressRange).toBeNull()
    })
  })

  describe('clearAllFilters', () => {
    it('should clear all filters', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?period=monthly&progress_min=0&progress_max=100'),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(result.current.filters).toEqual({
        period: [],
      })
      expect(result.current.activeFilterCount).toBe(0)
    })
  })

  describe('activeFilterCount', () => {
    it('should calculate activeFilterCount correctly', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?period=monthly&progress_min=0'),
      })

      // period=1, progress range=1 = 2
      expect(result.current.activeFilterCount).toBe(2)
      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('should count period filter as one regardless of array length', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?period=monthly,weekly,quarterly'),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count progress range as single filter', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_min=0&progress_max=100'),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count only progress_min as single filter', () => {
      const { result } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?progress_min=50'),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })
  })

  describe('URL State Persistence', () => {
    it('should maintain filter state across re-renders', () => {
      const { result, rerender } = renderHook(() => useBudgetFilters(), {
        wrapper: createWrapper('/?period=monthly'),
      })

      expect(result.current.periodFilter).toEqual(['monthly'])

      rerender()

      expect(result.current.periodFilter).toEqual(['monthly'])
    })
  })
})
