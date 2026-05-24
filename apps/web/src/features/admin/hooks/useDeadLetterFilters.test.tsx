/**
 * useDeadLetterFilters Hook Tests
 * Issue #633: Dead Letter Queue Admin UI
 * Issue #1148: Server-side sort for admin tables
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter, MemoryRouter } from 'react-router-dom'
import { createElement } from 'react'
import type { ReactNode } from 'react'
import { useDeadLetterFilters } from './useDeadLetterFilters'

// BrowserRouter wrapper for existing filter tests
function createWrapper() {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <BrowserRouter>{children}</BrowserRouter>
  }
}

// MemoryRouter wrapper for sort tests (initialEntries for URL-synced sort)
function createMemoryWrapper(initialUrl = '/') {
  return ({ children }: { children: ReactNode }) =>
    createElement(MemoryRouter, { initialEntries: [initialUrl] }, children)
}

describe('useDeadLetterFilters', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  it('returns default state', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    expect(result.current.filters).toEqual({
      kind: '',
      userId: '',
      retried: '',
      startDate: '',
      endDate: '',
      page: 1,
    })
  })

  it('setKind resets page to 1', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setPage(3)
    })
    expect(result.current.filters.page).toBe(3)

    act(() => {
      result.current.setKind('send_email')
    })
    expect(result.current.filters.kind).toBe('send_email')
    expect(result.current.filters.page).toBe(1)
  })

  it('setUserId resets page to 1', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setPage(2)
    })

    act(() => {
      result.current.setUserId('user_abc')
    })
    expect(result.current.filters.userId).toBe('user_abc')
    expect(result.current.filters.page).toBe(1)
  })

  it('setRetried resets page to 1', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setPage(2)
    })

    act(() => {
      result.current.setRetried('true')
    })
    expect(result.current.filters.retried).toBe('true')
    expect(result.current.filters.page).toBe(1)
  })

  it('setPage does not reset other filters', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setKind('send_email')
      result.current.setRetried('false')
    })

    act(() => {
      result.current.setPage(4)
    })

    expect(result.current.filters.page).toBe(4)
    expect(result.current.filters.kind).toBe('send_email')
    expect(result.current.filters.retried).toBe('false')
  })

  it('hasActiveFilters is false by default', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })
    expect(result.current.hasActiveFilters).toBe(false)
  })

  it('hasActiveFilters is true when kind is set', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setKind('send_email')
    })

    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('hasActiveFilters is true when userId is set', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setUserId('user_abc')
    })

    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('setStartDate resets page to 1', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setPage(3)
    })
    expect(result.current.filters.page).toBe(3)

    act(() => {
      result.current.setStartDate('2026-01-01')
    })
    expect(result.current.filters.startDate).toBe('2026-01-01')
    expect(result.current.filters.page).toBe(1)
  })

  it('setEndDate resets page to 1', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setPage(2)
    })
    expect(result.current.filters.page).toBe(2)

    act(() => {
      result.current.setEndDate('2026-03-22')
    })
    expect(result.current.filters.endDate).toBe('2026-03-22')
    expect(result.current.filters.page).toBe(1)
  })

  it('resetFilters clears all and resets page', () => {
    const { result } = renderHook(() => useDeadLetterFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setKind('send_email')
      result.current.setUserId('user_abc')
      result.current.setRetried('true')
      result.current.setStartDate('2026-01-01')
      result.current.setEndDate('2026-03-22')
      result.current.setPage(5)
    })

    act(() => {
      result.current.resetFilters()
    })

    expect(result.current.filters).toEqual({
      kind: '',
      userId: '',
      retried: '',
      startDate: '',
      endDate: '',
      page: 1,
    })
    expect(result.current.hasActiveFilters).toBe(false)
  })

  describe('sort state', () => {
    it('returns null sort by default', () => {
      const { result } = renderHook(() => useDeadLetterFilters(), {
        wrapper: createMemoryWrapper('/admin/dead-letter'),
      })

      expect(result.current.sort).toBeNull()
    })

    it('setSort updates sort and resets page to 1', () => {
      const { result } = renderHook(() => useDeadLetterFilters(), {
        wrapper: createMemoryWrapper('/admin/dead-letter'),
      })

      act(() => {
        result.current.setPage(3)
      })
      expect(result.current.filters.page).toBe(3)

      act(() => {
        result.current.setSort({ columnId: 'failed_at', direction: 'asc' })
      })

      expect(result.current.sort).toEqual({ columnId: 'failed_at', direction: 'asc' })
      expect(result.current.filters.page).toBe(1)
    })

    it('setSort(null) clears sort', () => {
      const { result } = renderHook(() => useDeadLetterFilters(), {
        wrapper: createMemoryWrapper('/admin/dead-letter?sort=failed_at:desc'),
      })

      expect(result.current.sort).toEqual({ columnId: 'failed_at', direction: 'desc' })

      act(() => {
        result.current.setSort(null)
      })

      expect(result.current.sort).toBeNull()
    })

    it('apiParams includes sort_by and sort_dir when sort is set', () => {
      const { result } = renderHook(() => useDeadLetterFilters(), {
        wrapper: createMemoryWrapper('/admin/dead-letter'),
      })

      act(() => {
        result.current.setSort({ columnId: 'kind', direction: 'asc' })
      })

      expect(result.current.apiParams.sort_by).toBe('kind')
      expect(result.current.apiParams.sort_dir).toBe('asc')
    })

    it('apiParams omits sort_by and sort_dir when sort is null', () => {
      const { result } = renderHook(() => useDeadLetterFilters(), {
        wrapper: createMemoryWrapper('/admin/dead-letter'),
      })

      expect(result.current.apiParams.sort_by).toBeUndefined()
      expect(result.current.apiParams.sort_dir).toBeUndefined()
    })

    it('resetFilters preserves active sort', () => {
      const { result } = renderHook(() => useDeadLetterFilters(), {
        wrapper: createMemoryWrapper('/admin/dead-letter?sort=retried_at:desc'),
      })

      act(() => {
        result.current.setKind('send_email')
        result.current.setPage(3)
      })

      act(() => {
        result.current.resetFilters()
      })

      expect(result.current.hasActiveFilters).toBe(false)
      expect(result.current.filters.page).toBe(1)
      expect(result.current.sort).toEqual({ columnId: 'retried_at', direction: 'desc' })
    })
  })
})
