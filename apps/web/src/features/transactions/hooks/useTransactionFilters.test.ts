/**
 * useTransactionFilters Hook Tests
 *
 * Tests the transaction filter hook with URL state synchronization.
 * Issue #529: Transaction page filtering
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter } from 'react-router'
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
        '/?account_ids=acc-1,acc-2&pfc_primaries=Food,Travel'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.account_ids).toEqual(['acc-1', 'acc-2'])
      expect(result.current.filters.pfc_primaries).toEqual(['Food', 'Travel'])
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
        result.current.setFilter('pfc_primaries', ['Food', 'Shopping'])
      })

      expect(result.current.filters.pfc_primaries).toEqual(['Food', 'Shopping'])
      expect(window.location.search).toContain('pfc_primaries=Food%2CShopping')
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
      window.history.replaceState({}, '', '/?pfc_primaries=Food,Travel')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('pfc_primaries', [])
      })

      expect(result.current.filters.pfc_primaries).toBeUndefined()
      expect(window.location.search).not.toContain('pfc_primaries')
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
        '/?search=coffee&min_amount=10&pfc_primaries=FOOD_AND_DRINK'
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

    it('should preserve unrelated URL params', () => {
      window.history.replaceState(
        {},
        '',
        '/?search=coffee&tab=overview'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(result.current.filters).toEqual({})
      expect(window.location.search).toContain('tab=overview')
      expect(window.location.search).not.toContain('search=')
    })
  })

  describe('activeFilterCount', () => {
    it('should calculate activeFilterCount correctly', () => {
      window.history.replaceState(
        {},
        '',
        '/?search=coffee&pfc_primaries=FOOD_AND_DRINK&min_amount=10'
      )

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      // search=1, pfc_primaries=1, amount range=1 = 3
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

    it('should count merchant_id as active filter', () => {
      window.history.replaceState({}, '', '/?merchant_id=uuid-merchant-abc')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.activeFilterCount).toBe(1)
      expect(result.current.hasActiveFilters).toBe(true)
    })
  })

  describe('Date Presets', () => {
    it('should convert date preset this_week to date range', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('this_week')
      })

      expect(result.current.filters.start_date).toBeDefined()
      expect(result.current.filters.end_date).toBeDefined()
      expect(result.current.datePreset).toBe('this_week')
    })

    it('should convert date preset this_month to date range', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('this_month')
      })

      expect(result.current.filters.start_date).toBeDefined()
      expect(result.current.filters.end_date).toBeDefined()
    })

    it('should convert date preset last_3_months to date range', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('last_3_months')
      })

      expect(result.current.filters.start_date).toBeDefined()
      expect(result.current.filters.end_date).toBeDefined()
    })

    it('should convert date preset ytd to date range', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setDatePreset('ytd')
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
          pfc_primaries: ['FOOD_AND_DRINK'],
        })
      })

      expect(result.current.filters.search).toBe('coffee')
      expect(result.current.filters.min_amount).toBe(10)
      expect(result.current.filters.pfc_primaries).toEqual(['FOOD_AND_DRINK'])
    })
  })

  describe('clearKeysOnWrite — offset cleared on every mutation', () => {
    it('should remove offset when setFilter is called', () => {
      window.history.replaceState({}, '', '/?search=coffee&offset=50')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('search', 'tea')
      })

      expect(new URLSearchParams(window.location.search).get('offset')).toBeNull()
    })

    it('should remove offset when setFilters is called', () => {
      window.history.replaceState({}, '', '/?min_amount=5&offset=100')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilters({ min_amount: 20 })
      })

      expect(new URLSearchParams(window.location.search).get('offset')).toBeNull()
    })

    it('should remove offset when clearFilter is called', () => {
      window.history.replaceState({}, '', '/?search=coffee&offset=25')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearFilter('search')
      })

      expect(new URLSearchParams(window.location.search).get('offset')).toBeNull()
    })

    it('should remove offset when clearAllFilters is called', () => {
      window.history.replaceState({}, '', '/?search=coffee&offset=75')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearAllFilters()
      })

      expect(new URLSearchParams(window.location.search).get('offset')).toBeNull()
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

  describe('merchant_id filter', () => {
    it('should parse merchant_id from URL params', () => {
      window.history.replaceState({}, '', '/?merchant_id=uuid-merchant-abc')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.merchant_id).toBe('uuid-merchant-abc')
    })

    it('should not set merchant_id when param is absent', () => {
      window.history.replaceState({}, '', '/')

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.merchant_id).toBeUndefined()
    })

    it('should set merchant_id via setFilter and update URL', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('merchant_id', 'uuid-merchant-xyz')
      })

      expect(result.current.filters.merchant_id).toBe('uuid-merchant-xyz')
      expect(window.location.search).toContain('merchant_id=uuid-merchant-xyz')
    })
  })

  describe('pfc_detaileds filter', () => {
    it('round-trips pfc_detaileds to/from URL params', () => {
      // Read direction: pre-populate URL and verify parsed on mount
      window.history.replaceState({}, '', '/?pfc_detaileds=FOOD_AND_DRINK_RESTAURANTS%2CTRANSPORTATION_GAS')
      const { result: prePopResult, unmount } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      expect(prePopResult.current.filters.pfc_detaileds).toEqual(['FOOD_AND_DRINK_RESTAURANTS', 'TRANSPORTATION_GAS'])
      unmount()
      window.history.replaceState({}, '', '/') // reset for write-direction assertions below

      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('pfc_detaileds', ['Food_Groceries', 'Food_Restaurants'])
      })

      expect(result.current.filters.pfc_detaileds).toEqual(['Food_Groceries', 'Food_Restaurants'])
      expect(window.location.search).toContain('pfc_detaileds=Food_Groceries%2CFood_Restaurants')
      expect(result.current.activeFilterCount).toBeGreaterThanOrEqual(1)
    })

    it('combines pfc_primaries and pfc_detaileds into separate URL params', () => {
      const { result } = renderHook(() => useTransactionFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setFilter('pfc_primaries', ['Food', 'Travel'])
      })

      act(() => {
        result.current.setFilter('pfc_detaileds', ['Food_Groceries', 'Travel_Air'])
      })

      expect(result.current.filters.pfc_primaries).toEqual(['Food', 'Travel'])
      expect(result.current.filters.pfc_detaileds).toEqual(['Food_Groceries', 'Travel_Air'])

      const urlParams = new URLSearchParams(window.location.search)
      expect(urlParams.get('pfc_primaries')).toBe('Food,Travel')
      expect(urlParams.get('pfc_detaileds')).toBe('Food_Groceries,Travel_Air')
    })
  })

  describe('Sort (URL-persisted)', () => {
    it('returns default sort date:desc when no URL param', () => {
      window.history.replaceState({}, '', '/')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      expect(result.current.sort).toEqual({ columnId: 'date', direction: 'desc' })
      expect(result.current.isDefaultSort).toBe(true)
    })

    it('parses sort from URL', () => {
      window.history.replaceState({}, '', '/?sort=amount%3Adesc')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      expect(result.current.sort).toEqual({ columnId: 'amount', direction: 'desc' })
      expect(result.current.isDefaultSort).toBe(false)
    })

    it('setSort updates sort and deletes offset', () => {
      window.history.replaceState({}, '', '/?offset=50')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      act(() => {
        result.current.setSort({ columnId: 'amount', direction: 'asc' })
      })
      expect(result.current.sort).toEqual({ columnId: 'amount', direction: 'asc' })
      expect(window.location.search).not.toContain('offset')
    })

    it('resetSort restores default date:desc', () => {
      window.history.replaceState({}, '', '/?sort=amount%3Aasc')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      act(() => {
        result.current.resetSort()
      })
      expect(result.current.sort).toEqual({ columnId: 'date', direction: 'desc' })
      expect(result.current.isDefaultSort).toBe(true)
    })

    it('clearAllFilters preserves sort', () => {
      window.history.replaceState({}, '', '/?search=coffee&sort=amount%3Adesc')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      act(() => {
        result.current.clearAllFilters()
      })
      expect(result.current.filters.search).toBeUndefined()
      expect(result.current.sort).toEqual({ columnId: 'amount', direction: 'desc' })
    })

    it('clearAllFilters({ includeSort: true }) clears sort too', () => {
      window.history.replaceState({}, '', '/?sort=amount%3Adesc')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      act(() => {
        result.current.clearAllFilters({ includeSort: true })
      })
      expect(result.current.sort).toEqual({ columnId: 'date', direction: 'desc' })
      expect(result.current.isDefaultSort).toBe(true)
    })

    it('activeFilterCount does not count sort', () => {
      window.history.replaceState({}, '', '/?sort=amount%3Adesc&search=coffee')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      expect(result.current.activeFilterCount).toBe(1)
    })

    it('ignores invalid sort column gracefully', () => {
      window.history.replaceState({}, '', '/?sort=unknown%3Aasc')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      expect(result.current.sort).toEqual({ columnId: 'date', direction: 'desc' })
      expect(result.current.isDefaultSort).toBe(true)
    })
  })

  describe('include_investment_activity filter', () => {
    it('defaults to true (not present in URL) when no URL param', () => {
      window.history.replaceState({}, '', '/')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      // include_investment_activity is true by default — must be absent from URL
      expect(new URLSearchParams(window.location.search).get('include_investment_activity')).toBeNull()
      // The hook exposes undefined (falsy default handled by consumer as true)
      expect(result.current.filters.include_investment_activity).toBeUndefined()
    })

    it('setting to false serializes to URL as "false"', () => {
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      act(() => {
        result.current.setIncludeInvestmentActivity(false)
      })
      expect(result.current.filters.include_investment_activity).toBe(false)
      expect(new URLSearchParams(window.location.search).get('include_investment_activity')).toBe('false')
    })

    it('setting back to true removes the URL param', () => {
      window.history.replaceState({}, '', '/?include_investment_activity=false')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      act(() => {
        result.current.setIncludeInvestmentActivity(true)
      })
      expect(new URLSearchParams(window.location.search).get('include_investment_activity')).toBeNull()
    })

    it('setting activity_types when include=false immediately clears them', () => {
      window.history.replaceState({}, '', '/?include_investment_activity=false')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      act(() => {
        result.current.setActivityTypes(['buy', 'dividend'])
      })
      // activity_types must be absent (cleared because include=false)
      expect(result.current.filters.activity_types).toBeUndefined()
      expect(new URLSearchParams(window.location.search).get('activity_types')).toBeNull()
    })

    it('setting activity_types when include is true persists them', () => {
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      act(() => {
        result.current.setActivityTypes(['sell', 'fee'])
      })
      expect(result.current.filters.activity_types).toEqual(['sell', 'fee'])
      expect(new URLSearchParams(window.location.search).get('activity_types')).toBe('sell,fee')
    })

    it('deep-linked URL with include=false + activity_types → activity_types suppressed in derived state (F2)', () => {
      // A user shares a URL like ?include_investment_activity=false&activity_types=buy — this
      // bypasses both setters. The derived filters object must canonicalize: activity_types is
      // meaningless when investments are excluded, so suppress it and don't count it.
      window.history.replaceState({}, '', '/?include_investment_activity=false&activity_types=buy')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      // Derived filters must suppress activity_types when investments are off
      expect(result.current.filters.activity_types).toBeUndefined()
      // activeFilterCount = 1 (include=false) — activity_types must NOT add to the count
      expect(result.current.activeFilterCount).toBe(1)
    })

    it('deep-linked URL with include absent + activity_types → activity_types counted normally', () => {
      // Investments ON (default) — activity_types should pass through
      window.history.replaceState({}, '', '/?activity_types=buy,dividend')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      expect(result.current.filters.activity_types).toEqual(['buy', 'dividend'])
      // activeFilterCount = 1 (activity_types group)
      expect(result.current.activeFilterCount).toBe(1)
    })
  })
  // ── #1569 T12 gap 1: reversals are opt-in URL state ─────────────────────────────────────
  describe('include_reversed filter', () => {
    it('defaults to off and stays out of the URL', () => {
      window.history.replaceState({}, '', '/')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      expect(result.current.filters.include_reversed).toBeUndefined()
      expect(result.current.includeReversed).toBe(false)
      expect(result.current.activeFilterCount).toBe(0)
    })

    it('opting in serializes to the URL and counts as an active filter', () => {
      window.history.replaceState({}, '', '/')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      act(() => result.current.setIncludeReversed(true))

      expect(new URLSearchParams(window.location.search).get('include_reversed')).toBe('true')
      expect(result.current.filters.include_reversed).toBe(true)
      expect(result.current.includeReversed).toBe(true)
      expect(result.current.activeFilterCount).toBe(1)
    })

    it('opting back out removes the param rather than writing false', () => {
      window.history.replaceState({}, '', '/?include_reversed=true')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      expect(result.current.includeReversed).toBe(true)

      act(() => result.current.setIncludeReversed(false))

      expect(new URLSearchParams(window.location.search).get('include_reversed')).toBeNull()
      expect(result.current.filters.include_reversed).toBeUndefined()
      expect(result.current.activeFilterCount).toBe(0)
    })

    it('is deep-linkable', () => {
      window.history.replaceState({}, '', '/?include_reversed=true')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      expect(result.current.filters.include_reversed).toBe(true)
    })
  })
  // ── #1626 Phase 2: uncategorized is navigable URL state ─────────────────────────────────
  //
  // The reports Sankey renders an "Uncategorized" node, but `UNCATEGORIZED` is a sentinel the
  // analytics layer SYNTHESISES — no row stores it — so `pfc_primaries=UNCATEGORIZED` matched
  // nothing and the node had to be inert. This filter is what makes it clickable, which means the
  // Sankey's click-through depends on every layer below carrying the flag; a gap anywhere leaves
  // the feature looking wired and behaving inertly.
  describe('uncategorized filter', () => {
    it('defaults to off and stays out of the URL', () => {
      window.history.replaceState({}, '', '/')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      expect(result.current.filters.uncategorized).toBeUndefined()
      expect(result.current.uncategorized).toBe(false)
      expect(result.current.activeFilterCount).toBe(0)
    })

    it('opting in serializes to the URL and counts as an active filter', () => {
      window.history.replaceState({}, '', '/')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      act(() => result.current.setUncategorized(true))

      expect(new URLSearchParams(window.location.search).get('uncategorized')).toBe('true')
      expect(result.current.filters.uncategorized).toBe(true)
      expect(result.current.uncategorized).toBe(true)
      expect(result.current.activeFilterCount).toBe(1)
    })

    // ⚠ This is the one the plain-boolean design makes easy to get wrong: the boolean codec
    // serialises `false` as the string "false" and only `undefined`/`null` delete a param, so
    // setFilter('uncategorized', false) would leave `?uncategorized=false` in the URL — a second
    // filter state that requests exactly the same rows, splitting the React Query cache.
    it('opting back out removes the param rather than writing false', () => {
      window.history.replaceState({}, '', '/?uncategorized=true')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })
      expect(result.current.uncategorized).toBe(true)

      act(() => result.current.setUncategorized(false))

      expect(new URLSearchParams(window.location.search).get('uncategorized')).toBeNull()
      expect(result.current.filters.uncategorized).toBeUndefined()
      expect(result.current.activeFilterCount).toBe(0)
    })

    it('is deep-linkable, which is the whole point — the Sankey navigates by URL', () => {
      window.history.replaceState({}, '', '/?uncategorized=true')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      expect(result.current.filters.uncategorized).toBe(true)
      expect(result.current.uncategorized).toBe(true)
    })

    // The four category predicates are OR-joined server-side (#1626 defect C), so combining them
    // is a union and must read as FOUR active filters, not one.
    it('combines with the category filters instead of replacing them', () => {
      window.history.replaceState({}, '', '/?uncategorized=true&pfc_primaries=TRAVEL&pfc_detaileds=TRAVEL_FLIGHTS&custom_category_ids=abc')
      const { result } = renderHook(() => useTransactionFilters(), { wrapper: createWrapper() })

      expect(result.current.filters.uncategorized).toBe(true)
      expect(result.current.filters.pfc_primaries).toEqual(['TRAVEL'])
      expect(result.current.activeFilterCount).toBe(4)
    })
  })
})
