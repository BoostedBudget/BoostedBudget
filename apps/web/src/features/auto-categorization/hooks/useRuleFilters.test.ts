/**
 * useRuleFilters Hook Tests
 * Issue #590: Inline Column Filtering
 *
 * Tests the auto-categorization rule filter hook with URL state synchronization.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import React from 'react'
import { useRuleFilters } from './useRuleFilters'

// Wrapper component with Router for useSearchParams
function createWrapper() {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(BrowserRouter, null, children)
  }
}

describe('useRuleFilters', () => {
  beforeEach(() => {
    // Reset URL before each test
    window.history.replaceState({}, '', '/')
  })

  afterEach(() => {
    // mocks restored in afterEach
  })

  describe('Initialization', () => {
    it('should initialize with empty filters when no URL params', () => {
      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters).toEqual({
        status: [],
        category: [],
      })
      expect(result.current.statusFilter).toEqual([])
      expect(result.current.categoryFilter).toEqual([])
      expect(result.current.priorityRange).toBeNull()
      expect(result.current.activeFilterCount).toBe(0)
      expect(result.current.hasActiveFilters).toBe(false)
    })

    it('should parse status filter from URL', () => {
      window.history.replaceState({}, '', '/?status=enabled,disabled')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.status).toEqual(['enabled', 'disabled'])
      expect(result.current.statusFilter).toEqual(['enabled', 'disabled'])
    })

    it('should parse category filter from URL', () => {
      window.history.replaceState({}, '', '/?category=Food%20And%20Drink,Shopping')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.category).toEqual(['Food And Drink', 'Shopping'])
      expect(result.current.categoryFilter).toEqual(['Food And Drink', 'Shopping'])
    })

    it('should parse priority range from URL', () => {
      window.history.replaceState({}, '', '/?priority_min=1&priority_max=100')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.priorityMin).toBe(1)
      expect(result.current.filters.priorityMax).toBe(100)
      expect(result.current.priorityRange).toEqual({ min: 1, max: 100 })
    })

    it('should parse priority_min only', () => {
      window.history.replaceState({}, '', '/?priority_min=50')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.priorityMin).toBe(50)
      expect(result.current.filters.priorityMax).toBeUndefined()
      expect(result.current.priorityRange).toEqual({ min: 50, max: undefined })
    })

    it('should parse priority_max only', () => {
      window.history.replaceState({}, '', '/?priority_max=200')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.priorityMin).toBeUndefined()
      expect(result.current.filters.priorityMax).toBe(200)
      expect(result.current.priorityRange).toEqual({ min: undefined, max: 200 })
    })

    it('should handle invalid priority values gracefully', () => {
      window.history.replaceState({}, '', '/?priority_min=invalid&priority_max=not-a-number')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.priorityMin).toBeUndefined()
      expect(result.current.filters.priorityMax).toBeUndefined()
      expect(result.current.priorityRange).toBeNull()
    })

    it('should trim whitespace from filter values', () => {
      window.history.replaceState({}, '', '/?status=%20enabled%20,%20disabled%20')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.status).toEqual(['enabled', 'disabled'])
    })

    it('should filter out empty values', () => {
      window.history.replaceState({}, '', '/?category=Food,,Shopping,')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.category).toEqual(['Food', 'Shopping'])
    })
  })

  describe('setStatusFilter', () => {
    it('should update URL when setting status filter', () => {
      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setStatusFilter(['enabled'])
      })

      expect(result.current.statusFilter).toEqual(['enabled'])
      expect(window.location.search).toContain('status=enabled')
    })

    it('should remove param from URL when array is empty', () => {
      window.history.replaceState({}, '', '/?status=enabled')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setStatusFilter([])
      })

      expect(result.current.statusFilter).toEqual([])
      expect(window.location.search).not.toContain('status')
    })
  })

  describe('setCategoryFilter', () => {
    it('should update URL when setting category filter', () => {
      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setCategoryFilter(['Food And Drink', 'Shopping'])
      })

      expect(result.current.categoryFilter).toEqual(['Food And Drink', 'Shopping'])
      // URL encodes spaces as + or %20 depending on browser
      expect(window.location.search).toMatch(/category=Food(\+|%20)And(\+|%20)Drink%2CShopping/)
    })

    it('should remove param from URL when array is empty', () => {
      window.history.replaceState({}, '', '/?category=Food')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setCategoryFilter([])
      })

      expect(result.current.categoryFilter).toEqual([])
      expect(window.location.search).not.toContain('category')
    })
  })

  describe('setPriorityRange', () => {
    it('should update URL when setting priority range', () => {
      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPriorityRange({ min: 1, max: 100 })
      })

      expect(result.current.priorityRange).toEqual({ min: 1, max: 100 })
      expect(window.location.search).toContain('priority_min=1')
      expect(window.location.search).toContain('priority_max=100')
    })

    it('should handle min only', () => {
      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPriorityRange({ min: 50 })
      })

      expect(window.location.search).toContain('priority_min=50')
      expect(window.location.search).not.toContain('priority_max')
    })

    it('should handle max only', () => {
      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPriorityRange({ max: 200 })
      })

      expect(window.location.search).not.toContain('priority_min')
      expect(window.location.search).toContain('priority_max=200')
    })

    it('should clear priority params when setting null', () => {
      window.history.replaceState({}, '', '/?priority_min=1&priority_max=100')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPriorityRange(null)
      })

      expect(result.current.priorityRange).toBeNull()
      expect(window.location.search).not.toContain('priority_min')
      expect(window.location.search).not.toContain('priority_max')
    })

    it('should clear priority params when both min and max are undefined', () => {
      window.history.replaceState({}, '', '/?priority_min=1&priority_max=100')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPriorityRange({ min: undefined, max: undefined })
      })

      expect(window.location.search).not.toContain('priority_min')
      expect(window.location.search).not.toContain('priority_max')
    })
  })

  describe('clearAllFilters', () => {
    it('should clear all filters and reset URL', () => {
      window.history.replaceState(
        {},
        '',
        '/?status=enabled&category=Food&priority_min=1&priority_max=100'
      )

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(result.current.filters).toEqual({
        status: [],
        category: [],
      })
      expect(result.current.activeFilterCount).toBe(0)
      expect(window.location.search).toBe('')
    })

    it('should preserve other URL params', () => {
      window.history.replaceState({}, '', '/?status=enabled&sort=name&order=asc')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(window.location.search).not.toContain('status')
      expect(window.location.search).toContain('sort=name')
      expect(window.location.search).toContain('order=asc')
    })
  })

  describe('activeFilterCount', () => {
    it('should calculate activeFilterCount correctly', () => {
      window.history.replaceState(
        {},
        '',
        '/?status=enabled&category=Food&priority_min=1'
      )

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      // status=1, category=1, priority range=1 = 3
      expect(result.current.activeFilterCount).toBe(3)
      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('should count status filter as one regardless of array length', () => {
      window.history.replaceState({}, '', '/?status=enabled,disabled')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count priority range as single filter', () => {
      window.history.replaceState({}, '', '/?priority_min=1&priority_max=100')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count only priority_min as single filter', () => {
      window.history.replaceState({}, '', '/?priority_min=50')

      const { result } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })
  })

  describe('URL State Persistence', () => {
    it('should maintain filter state across re-renders', () => {
      window.history.replaceState({}, '', '/?status=enabled')

      const { result, rerender } = renderHook(() => useRuleFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.statusFilter).toEqual(['enabled'])

      rerender()

      expect(result.current.statusFilter).toEqual(['enabled'])
    })
  })
})
