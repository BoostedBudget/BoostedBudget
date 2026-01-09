/**
 * useAccountFilters Hook Tests
 * Issue #590: Inline Column Filtering
 *
 * Tests the account filter hook with URL state synchronization.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import React from 'react'
import { useAccountFilters } from './useAccountFilters'

// Wrapper component with Router for useSearchParams
function createWrapper() {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(BrowserRouter, null, children)
  }
}

describe('useAccountFilters', () => {
  beforeEach(() => {
    // Reset URL before each test
    window.history.replaceState({}, '', '/')
  })

  afterEach(() => {
    // mocks restored in afterEach
  })

  describe('Initialization', () => {
    it('should initialize with empty filters when no URL params', () => {
      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters).toEqual({
        institution: [],
        type: [],
      })
      expect(result.current.institutionFilter).toEqual([])
      expect(result.current.typeFilter).toEqual([])
      expect(result.current.balanceRange).toBeNull()
      expect(result.current.activeFilterCount).toBe(0)
      expect(result.current.hasActiveFilters).toBe(false)
    })

    it('should parse institution filter from URL', () => {
      window.history.replaceState({}, '', '/?institution=Chase,Bank%20of%20America')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.institution).toEqual(['Chase', 'Bank of America'])
      expect(result.current.institutionFilter).toEqual(['Chase', 'Bank of America'])
    })

    it('should parse type filter from URL', () => {
      window.history.replaceState({}, '', '/?type=checking,savings')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.type).toEqual(['checking', 'savings'])
      expect(result.current.typeFilter).toEqual(['checking', 'savings'])
    })

    it('should parse balance range from URL', () => {
      window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.balanceMin).toBe(100)
      expect(result.current.filters.balanceMax).toBe(5000)
      expect(result.current.balanceRange).toEqual({ min: 100, max: 5000 })
    })

    it('should parse balance_min only', () => {
      window.history.replaceState({}, '', '/?balance_min=500')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.balanceMin).toBe(500)
      expect(result.current.filters.balanceMax).toBeUndefined()
      expect(result.current.balanceRange).toEqual({ min: 500, max: undefined })
    })

    it('should parse balance_max only', () => {
      window.history.replaceState({}, '', '/?balance_max=10000')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.balanceMin).toBeUndefined()
      expect(result.current.filters.balanceMax).toBe(10000)
      expect(result.current.balanceRange).toEqual({ min: undefined, max: 10000 })
    })

    it('should handle invalid balance values gracefully', () => {
      window.history.replaceState({}, '', '/?balance_min=invalid&balance_max=not-a-number')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.balanceMin).toBeUndefined()
      expect(result.current.filters.balanceMax).toBeUndefined()
      expect(result.current.balanceRange).toBeNull()
    })

    it('should trim whitespace from filter values', () => {
      window.history.replaceState({}, '', '/?institution=%20Chase%20,%20Wells%20Fargo%20')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.institution).toEqual(['Chase', 'Wells Fargo'])
    })

    it('should filter out empty values', () => {
      window.history.replaceState({}, '', '/?type=checking,,savings,')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.type).toEqual(['checking', 'savings'])
    })
  })

  describe('setInstitutionFilter', () => {
    it('should update URL when setting institution filter', () => {
      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setInstitutionFilter(['Chase', 'Wells Fargo'])
      })

      expect(result.current.institutionFilter).toEqual(['Chase', 'Wells Fargo'])
      // URL encodes spaces as + or %20 depending on browser
      expect(window.location.search).toMatch(/institution=Chase%2CWells(\+|%20)Fargo/)
    })

    it('should remove param from URL when array is empty', () => {
      window.history.replaceState({}, '', '/?institution=Chase')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setInstitutionFilter([])
      })

      expect(result.current.institutionFilter).toEqual([])
      expect(window.location.search).not.toContain('institution')
    })
  })

  describe('setTypeFilter', () => {
    it('should update URL when setting type filter', () => {
      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setTypeFilter(['checking', 'savings'])
      })

      expect(result.current.typeFilter).toEqual(['checking', 'savings'])
      expect(window.location.search).toContain('type=checking%2Csavings')
    })

    it('should remove param from URL when array is empty', () => {
      window.history.replaceState({}, '', '/?type=checking')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setTypeFilter([])
      })

      expect(result.current.typeFilter).toEqual([])
      expect(window.location.search).not.toContain('type')
    })
  })

  describe('setBalanceRange', () => {
    it('should update URL when setting balance range', () => {
      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setBalanceRange({ min: 100, max: 5000 })
      })

      expect(result.current.balanceRange).toEqual({ min: 100, max: 5000 })
      expect(window.location.search).toContain('balance_min=100')
      expect(window.location.search).toContain('balance_max=5000')
    })

    it('should handle min only', () => {
      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setBalanceRange({ min: 500 })
      })

      expect(window.location.search).toContain('balance_min=500')
      expect(window.location.search).not.toContain('balance_max')
    })

    it('should handle max only', () => {
      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setBalanceRange({ max: 10000 })
      })

      expect(window.location.search).not.toContain('balance_min')
      expect(window.location.search).toContain('balance_max=10000')
    })

    it('should clear balance params when setting null', () => {
      window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setBalanceRange(null)
      })

      expect(result.current.balanceRange).toBeNull()
      expect(window.location.search).not.toContain('balance_min')
      expect(window.location.search).not.toContain('balance_max')
    })

    it('should clear balance params when both min and max are undefined', () => {
      window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setBalanceRange({ min: undefined, max: undefined })
      })

      expect(window.location.search).not.toContain('balance_min')
      expect(window.location.search).not.toContain('balance_max')
    })
  })

  describe('clearAllFilters', () => {
    it('should clear all filters and reset URL', () => {
      window.history.replaceState(
        {},
        '',
        '/?institution=Chase&type=checking&balance_min=100&balance_max=5000'
      )

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(result.current.filters).toEqual({
        institution: [],
        type: [],
      })
      expect(result.current.activeFilterCount).toBe(0)
      expect(window.location.search).toBe('')
    })

    it('should preserve other URL params', () => {
      window.history.replaceState({}, '', '/?institution=Chase&sort=name&order=asc')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(window.location.search).not.toContain('institution')
      expect(window.location.search).toContain('sort=name')
      expect(window.location.search).toContain('order=asc')
    })
  })

  describe('activeFilterCount', () => {
    it('should calculate activeFilterCount correctly', () => {
      window.history.replaceState(
        {},
        '',
        '/?institution=Chase&type=checking&balance_min=100'
      )

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      // institution=1, type=1, balance range=1 = 3
      expect(result.current.activeFilterCount).toBe(3)
      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('should count institution filter as one regardless of array length', () => {
      window.history.replaceState({}, '', '/?institution=Chase,Wells%20Fargo,Bank%20of%20America')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count balance range as single filter', () => {
      window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count only balance_min as single filter', () => {
      window.history.replaceState({}, '', '/?balance_min=100')

      const { result } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })
  })

  describe('URL State Persistence', () => {
    it('should maintain filter state across re-renders', () => {
      window.history.replaceState({}, '', '/?institution=Chase')

      const { result, rerender } = renderHook(() => useAccountFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.institutionFilter).toEqual(['Chase'])

      rerender()

      expect(result.current.institutionFilter).toEqual(['Chase'])
    })
  })
})
