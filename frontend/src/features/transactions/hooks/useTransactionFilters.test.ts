/**
 * useTransactionFilters Hook Tests
 *
 * Tests the transaction filter hook with URL state synchronization.
 * Issue #529: Transaction page filtering
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import React from 'react'
import { useTransactionFilters } from './useTransactionFilters'

// Wrapper component with Router for useSearchParams
function createWrapper() {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(BrowserRouter, null, children)
  }
}

describe('useTransactionFilters', () => {
  beforeEach(() => {
    // Reset URL before each test
    window.history.replaceState({}, '', '/')
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Initialization', () => {
    it('should initialize with empty filters when no URL params', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters).toEqual({})
      expect(result.current.activeFilterCount).toBe(0)
      expect(result.current.hasActiveFilters).toBe(false)
      expect(result.current.datePreset).toBe('all')
      expect(result.current.statusFilter).toBe('all')
    })

    it('should parse filters from URL on mount', () => {
      // Set initial URL with filters
      window.history.replaceState({}, '', '/?search=coffee&min_amount=10&max_amount=100')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.search).toBe('coffee')
      expect(result.current.filters.min_amount).toBe(10)
      expect(result.current.filters.max_amount).toBe(100)
    })

    it('should parse array filters from URL (comma-separated)', () => {
      window.history.replaceState(
        {},
        '',
        '/?account_ids=acc-1,acc-2&categories=Food,Travel'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.account_ids).toEqual(['acc-1', 'acc-2'])
      expect(result.current.filters.categories).toEqual(['Food', 'Travel'])
    })

    it('should parse boolean pending filter from URL', () => {
      window.history.replaceState({}, '', '/?pending=true')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.pending).toBe(true)
      expect(result.current.statusFilter).toBe('pending')
    })
  })

  describe('setFilter', () => {
    it('should update URL when setFilter is called', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('search', 'groceries')
      })

      expect(result.current.filters.search).toBe('groceries')
      expect(window.location.search).toContain('search=groceries')
    })

    it('should handle array filter values', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('categories', ['Food', 'Shopping'])
      })

      expect(result.current.filters.categories).toEqual(['Food', 'Shopping'])
      expect(window.location.search).toContain('categories=Food%2CShopping')
    })

    it('should remove param from URL when value is undefined', () => {
      window.history.replaceState({}, '', '/?search=coffee')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('search', undefined)
      })

      expect(result.current.filters.search).toBeUndefined()
      expect(window.location.search).not.toContain('search')
    })

    it('should remove param from URL when array is empty', () => {
      window.history.replaceState({}, '', '/?categories=Food,Travel')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('categories', [])
      })

      expect(result.current.filters.categories).toBeUndefined()
      expect(window.location.search).not.toContain('categories')
    })
  })

  describe('clearFilter', () => {
    it('should clear individual filter and update URL', () => {
      window.history.replaceState({}, '', '/?search=coffee&min_amount=10')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearFilter('search')
      })

      expect(result.current.filters.search).toBeUndefined()
      expect(result.current.filters.min_amount).toBe(10)
      expect(window.location.search).not.toContain('search')
      expect(window.location.search).toContain('min_amount=10')
    })
  })

  describe('clearAllFilters', () => {
    it('should clear all filters and reset URL', () => {
      window.history.replaceState(
        {},
        '',
        '/?search=coffee&min_amount=10&categories=Food'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(result.current.filters).toEqual({})
      expect(result.current.activeFilterCount).toBe(0)
      expect(window.location.search).toBe('')
    })
  })

  describe('activeFilterCount', () => {
    it('should calculate activeFilterCount correctly', () => {
      window.history.replaceState(
        {},
        '',
        '/?search=coffee&categories=Food&min_amount=10'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      // search=1, categories=1, amount range=1 = 3
      expect(result.current.activeFilterCount).toBe(3)
      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('should count date range as single filter', () => {
      window.history.replaceState(
        {},
        '',
        '/?start_date=2025-01-01&end_date=2025-12-31'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should count amount range as single filter', () => {
      window.history.replaceState({}, '', '/?min_amount=10&max_amount=100')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
    })

    it('should not count amount_type=all as a filter', () => {
      window.history.replaceState({}, '', '/?amount_type=all')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(0)
    })
  })

  describe('Date Presets', () => {
    it('should convert date preset 7d to date range', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('7d')
      })

      expect(result.current.filters.start_date).toBeDefined()
      expect(result.current.filters.end_date).toBeDefined()
      expect(result.current.datePreset).toBe('7d')
    })

    it('should convert date preset 30d to date range', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('30d')
      })

      expect(result.current.filters.start_date).toBeDefined()
      expect(result.current.filters.end_date).toBeDefined()
    })

    it('should convert date preset 90d to date range', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('90d')
      })

      expect(result.current.filters.start_date).toBeDefined()
      expect(result.current.filters.end_date).toBeDefined()
    })

    it('should convert date preset 1y to date range', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('1y')
      })

      expect(result.current.filters.start_date).toBeDefined()
      expect(result.current.filters.end_date).toBeDefined()
    })

    it('should clear date filters when preset is all', () => {
      window.history.replaceState(
        {},
        '',
        '/?start_date=2025-01-01&end_date=2025-12-31'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('all')
      })

      expect(result.current.filters.start_date).toBeUndefined()
      expect(result.current.filters.end_date).toBeUndefined()
      expect(result.current.datePreset).toBe('all')
    })

    it('should not change dates for custom preset', () => {
      window.history.replaceState(
        {},
        '',
        '/?start_date=2025-01-01&end_date=2025-06-30'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      const originalStart = result.current.filters.start_date
      const originalEnd = result.current.filters.end_date

      act(() => {
        result.current.setDatePreset('custom')
      })

      expect(result.current.filters.start_date).toBe(originalStart)
      expect(result.current.filters.end_date).toBe(originalEnd)
    })
  })

  describe('setCustomDateRange', () => {
    it('should set custom date range via setCustomDateRange', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setCustomDateRange('2025-01-01', '2025-06-30')
      })

      expect(result.current.filters.start_date).toBe('2025-01-01')
      expect(result.current.filters.end_date).toBe('2025-06-30')
      expect(result.current.datePreset).toBe('custom')
    })
  })

  describe('Status Filter', () => {
    it('should set pending=true for pending status', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setStatusFilter('pending')
      })

      expect(result.current.filters.pending).toBe(true)
      expect(result.current.statusFilter).toBe('pending')
    })

    it('should set pending=false for posted status', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setStatusFilter('posted')
      })

      expect(result.current.filters.pending).toBe(false)
      expect(result.current.statusFilter).toBe('posted')
    })

    it('should clear pending for all status', () => {
      window.history.replaceState({}, '', '/?pending=true')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setStatusFilter('all')
      })

      expect(result.current.filters.pending).toBeUndefined()
      expect(result.current.statusFilter).toBe('all')
    })
  })

  describe('setFilters (batch update)', () => {
    it('should update multiple filters at once', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilters({
          search: 'coffee',
          min_amount: 10,
          categories: ['Food'],
        })
      })

      expect(result.current.filters.search).toBe('coffee')
      expect(result.current.filters.min_amount).toBe(10)
      expect(result.current.filters.categories).toEqual(['Food'])
    })
  })

  describe('URL State Persistence', () => {
    it('should maintain filter state across re-renders', () => {
      window.history.replaceState({}, '', '/?search=coffee')

      const { result, rerender } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.search).toBe('coffee')

      rerender()

      expect(result.current.filters.search).toBe('coffee')
    })
  })
})
