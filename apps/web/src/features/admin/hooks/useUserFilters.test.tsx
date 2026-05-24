/**
 * useUserFilters Tests
 * Issue #644: Admin User Management UI
 *
 * Tests for URL-synced user filter state hook.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter, MemoryRouter } from 'react-router-dom'
import { createElement } from 'react'
import type { ReactNode } from 'react'
import { useUserFilters } from './useUserFilters'

// Wrapper with router (pre-existing tests use BrowserRouter + window.history.replaceState)
function createWrapper() {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <BrowserRouter>{children}</BrowserRouter>
  }
}

// MemoryRouter wrapper for sort tests (project pattern: MemoryRouter + initialEntries)
function createMemoryWrapper(initialUrl = '/') {
  return ({ children }: { children: ReactNode }) =>
    createElement(MemoryRouter, { initialEntries: [initialUrl] }, children)
}

describe('useUserFilters', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

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
      window.history.replaceState({}, '', '/admin/users?search=test%40example.com')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.search).toBe('test@example.com')
    })

    it('parses tier param from URL', () => {
      window.history.replaceState({}, '', '/admin/users?tier=premium')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.tier).toBe('premium')
    })

    it('parses page param from URL', () => {
      window.history.replaceState({}, '', '/admin/users?page=3')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.page).toBe(3)
    })

    it('parses page_size param from URL', () => {
      window.history.replaceState({}, '', '/admin/users?page_size=50')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.pageSize).toBe(50)
    })

    it('parses multiple params from URL', () => {
      window.history.replaceState({}, '', '/admin/users?search=john&tier=free&page=2&page_size=10')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
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
      window.history.replaceState({}, '', '/admin/users?page=5')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.page).toBe(5)

      act(() => {
        result.current.setSearch('new search')
      })

      expect(result.current.filters.page).toBe(1)
    })

    it('clears search when empty string passed', () => {
      window.history.replaceState({}, '', '/admin/users?search=existing')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
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
      window.history.replaceState({}, '', '/admin/users?page=3&tier=free')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
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
      window.history.replaceState({}, '', '/admin/users?search=test&tier=premium')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
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
      window.history.replaceState({}, '', '/admin/users?page=5')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setPageSize(10)
      })

      expect(result.current.filters.page).toBe(1)
    })
  })

  describe('clearFilters', () => {
    it('clears all filters and resets to defaults', () => {
      window.history.replaceState({}, '', '/admin/users?search=test&tier=premium&page=3&page_size=50')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
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
      window.history.replaceState({}, '', '/admin/users?search=test')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('returns true when tier is active', () => {
      window.history.replaceState({}, '', '/admin/users?tier=premium')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasActiveFilters).toBe(true)
    })

    it('does not count page or pageSize as active filters', () => {
      window.history.replaceState({}, '', '/admin/users?page=3&page_size=50')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasActiveFilters).toBe(false)
    })
  })

  describe('apiParams', () => {
    it('returns params formatted for API call', () => {
      window.history.replaceState({}, '', '/admin/users?search=test&tier=premium&page=2&page_size=50')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.apiParams).toEqual({
        page: 2,
        page_size: 50,
        search: 'test',
        tier: 'premium',
      })
    })

    it('omits empty string values', () => {
      window.history.replaceState({}, '', '/admin/users?page=1')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      // Should not include search or tier if they're empty
      expect(result.current.apiParams.search).toBeUndefined()
      expect(result.current.apiParams.tier).toBeUndefined()
    })
  })

  describe('sort state', () => {
    it('parses sort from URL', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createMemoryWrapper('/admin/users?sort=last_login_at:asc'),
      })

      expect(result.current.sort).toEqual({ columnId: 'last_login_at', direction: 'asc' })
    })

    it('ignores invalid sort column', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createMemoryWrapper('/admin/users?sort=invalid_col:asc'),
      })

      expect(result.current.sort).toBeNull()
    })

    it('setSort updates URL and resets page atomically', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createMemoryWrapper('/admin/users?page=3'),
      })

      expect(result.current.filters.page).toBe(3)

      act(() => {
        result.current.setSort({ columnId: 'subscription_tier', direction: 'desc' })
      })

      expect(result.current.sort).toEqual({ columnId: 'subscription_tier', direction: 'desc' })
      expect(result.current.filters.page).toBe(1)
    })

    it('setSort(null) clears sort from URL', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createMemoryWrapper('/admin/users?sort=created_at:asc'),
      })

      act(() => {
        result.current.setSort(null)
      })

      expect(result.current.sort).toBeNull()
    })

    it('apiParams includes sort_by and sort_dir when sort is set', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createMemoryWrapper('/admin/users?sort=subscription_tier:asc'),
      })

      expect(result.current.apiParams.sort_by).toBe('subscription_tier')
      expect(result.current.apiParams.sort_dir).toBe('asc')
    })

    it('apiParams omits sort fields when sort is null', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createMemoryWrapper('/admin/users'),
      })

      expect(result.current.apiParams.sort_by).toBeUndefined()
      expect(result.current.apiParams.sort_dir).toBeUndefined()
    })

    it('clearFilters preserves active sort', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createMemoryWrapper('/admin/users?search=test&tier=premium&sort=created_at:desc'),
      })

      expect(result.current.hasActiveFilters).toBe(true)
      expect(result.current.sort).toEqual({ columnId: 'created_at', direction: 'desc' })

      act(() => {
        result.current.clearFilters()
      })

      expect(result.current.hasActiveFilters).toBe(false)
      expect(result.current.sort).toEqual({ columnId: 'created_at', direction: 'desc' })
    })
  })
})
