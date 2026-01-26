/**
 * useUserFilters Tests
 * Issue #644: Admin User Management UI
 *
 * Tests for URL-synced user filter state hook.
 */

import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useUserFilters } from './useUserFilters'

// Wrapper with router
function createWrapper(initialEntries: string[] = ['/admin/users']) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter initialEntries={initialEntries}>{children}</MemoryRouter>
    )
  }
}

describe('useUserFilters', () => {
  describe('initial state', () => {
    it('returns default filter values when URL has no params', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters).toEqual({
        page: 1,
        pageSize: 20,
        search: '',
        tier: '',
      })
    })

    it('parses search param from URL', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?search=test%40example.com']),
      })

      expect(result.current.filters.search).toBe('test@example.com')
    })

    it('parses tier param from URL', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?tier=premium']),
      })

      expect(result.current.filters.tier).toBe('premium')
    })

    it('parses page param from URL', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?page=3']),
      })

      expect(result.current.filters.page).toBe(3)
    })

    it('parses page_size param from URL', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?page_size=50']),
      })

      expect(result.current.filters.pageSize).toBe(50)
    })

    it('parses multiple params from URL', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?search=john&tier=free&page=2&page_size=10']),
      })

      expect(result.current.filters).toEqual({
        page: 2,
        pageSize: 10,
        search: 'john',
        tier: 'free',
      })
    })
  })

  describe('setSearch', () => {
    it('updates search filter', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setSearch('admin@example.com')
      })

      expect(result.current.filters.search).toBe('admin@example.com')
    })

    it('resets page to 1 when search changes', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?page=5']),
      })

      expect(result.current.filters.page).toBe(5)

      act(() => {
        result.current.setSearch('new search')
      })

      expect(result.current.filters.page).toBe(1)
    })

    it('clears search when empty string passed', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?search=existing']),
      })

      act(() => {
        result.current.setSearch('')
      })

      expect(result.current.filters.search).toBe('')
    })
  })

  describe('setTier', () => {
    it('updates tier filter', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setTier('premium')
      })

      expect(result.current.filters.tier).toBe('premium')
    })

    it('resets page to 1 when tier changes', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?page=3&tier=free']),
      })

      act(() => {
        result.current.setTier('premium')
      })

      expect(result.current.filters.page).toBe(1)
    })
  })

  describe('setPage', () => {
    it('updates page', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPage(5)
      })

      expect(result.current.filters.page).toBe(5)
    })

    it('does not reset other filters when page changes', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?search=test&tier=premium']),
      })

      act(() => {
        result.current.setPage(3)
      })

      expect(result.current.filters.search).toBe('test')
      expect(result.current.filters.tier).toBe('premium')
      expect(result.current.filters.page).toBe(3)
    })
  })

  describe('setPageSize', () => {
    it('updates page size', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPageSize(50)
      })

      expect(result.current.filters.pageSize).toBe(50)
    })

    it('resets page to 1 when page size changes', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?page=5']),
      })

      act(() => {
        result.current.setPageSize(10)
      })

      expect(result.current.filters.page).toBe(1)
    })
  })

  describe('clearFilters', () => {
    it('clears all filters and resets to defaults', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?search=test&tier=premium&page=3&page_size=50']),
      })

      act(() => {
        result.current.clearFilters()
      })

      expect(result.current.filters).toEqual({
        page: 1,
        pageSize: 20,
        search: '',
        tier: '',
      })
    })
  })

  describe('hasActiveFilters', () => {
    it('returns false when no filters are active', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasActiveFilters).toBe(false)
    })

    it('returns true when search is active', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?search=test']),
      })

      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('returns true when tier is active', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?tier=premium']),
      })

      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('does not count page or pageSize as active filters', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?page=3&page_size=50']),
      })

      expect(result.current.hasActiveFilters).toBe(false)
    })
  })

  describe('apiParams', () => {
    it('returns params formatted for API call', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?search=test&tier=premium&page=2&page_size=50']),
      })

      expect(result.current.apiParams).toEqual({
        page: 2,
        page_size: 50,
        search: 'test',
        tier: 'premium',
      })
    })

    it('omits empty string values', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(['/admin/users?page=1']),
      })

      // Should not include search or tier if they're empty
      expect(result.current.apiParams.search).toBeUndefined()
      expect(result.current.apiParams.tier).toBeUndefined()
    })
  })
})
