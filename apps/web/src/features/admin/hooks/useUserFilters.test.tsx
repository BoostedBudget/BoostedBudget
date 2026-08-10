/**
 * useUserFilters Tests
 * Issue #644: Admin User Management UI
 *
 * Tests for URL-synced user filter state hook.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter, MemoryRouter } from 'react-router'
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

    // Batch EC / C4: `search` holds an email (or email prefix) of a data subject, and PostHog
    // session replay records the full URL verbatim — the anti-pattern named in
    // docs/security/posthog-pii-masking.md. It is deliberately NOT read from or written to the
    // URL any more, matching the sibling useDeadLetterFilters. These two tests pin both halves.
    it('IGNORES a search param in the URL (PII must not be URL state)', () => {
      window.history.replaceState({}, '', '/admin/users?search=test%40example.com')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters.search).toBe('')
    })

    it('never writes the search term into the URL', () => {
      window.history.replaceState({}, '', '/admin/users')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setSearch('victim@example.com')
      })

      expect(result.current.filters.search).toBe('victim@example.com')
      expect(window.location.search).not.toContain('victim')
      expect(window.location.search).not.toContain('search=')
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
      // `search` is intentionally absent from the URL — see the two tests above.
      window.history.replaceState({}, '', '/admin/users?tier=free&page=2&page_size=10')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      expect(result.current.filters).toEqual({
        page: 2,
        pageSize: 10,
        search: '',
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
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setSearch('existing')
      })
      expect(result.current.filters.search).toBe('existing')

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
      window.history.replaceState({}, '', '/admin/users?tier=premium')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setSearch('test')
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
      window.history.replaceState({}, '', '/admin/users?tier=premium&page=3&page_size=50')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      // search lives in component state now, so seed it through the setter — otherwise this
      // test would pass without clearFilters ever having to clear it.
      act(() => {
        result.current.setSearch('test')
      })
      expect(result.current.filters.search).toBe('test')

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
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.setSearch('test')
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
      window.history.replaceState({}, '', '/admin/users?tier=premium&page_size=50')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })

      // setSearch resets the page, so set it afterwards to reach the state under test.
      act(() => {
        result.current.setSearch('test')
      })
      act(() => {
        result.current.setPage(2)
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

  describe('apiParams search gating', () => {
    it('includes search in apiParams when search.length >= 3', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })
      act(() => result.current.setSearch('abc'))
      expect(result.current.apiParams.search).toBe('abc')
    })

    it('excludes search from apiParams when search.length === 2', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })
      act(() => result.current.setSearch('ab'))
      expect(result.current.apiParams.search).toBeUndefined()
    })

    it('excludes search from apiParams when search.length === 1', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })
      act(() => result.current.setSearch('a'))
      expect(result.current.apiParams.search).toBeUndefined()
    })

    it('excludes search from apiParams when search is empty', () => {
      window.history.replaceState({}, '', '/admin/users')
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })
      expect(result.current.apiParams.search).toBeUndefined()
    })

    it('includes search in apiParams when search.length === 3 exactly', () => {
      const { result } = renderHook(() => useUserFilters(), {
        wrapper: createWrapper(),
      })
      act(() => result.current.setSearch('xyz'))
      expect(result.current.apiParams.search).toBe('xyz')
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
        wrapper: createMemoryWrapper('/admin/users?tier=premium&sort=created_at:desc'),
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
