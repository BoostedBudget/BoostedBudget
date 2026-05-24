/**
 * useWebhookFilters Hook Tests
 * Issue #747: Paddle Webhook Admin UI
 * Issue #1148: Server-side sort for admin tables
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter, MemoryRouter } from 'react-router-dom'
import { createElement } from 'react'
import type { ReactNode } from 'react'
import { useWebhookFilters } from './useWebhookFilters'

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

describe('useWebhookFilters', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  it('returns default state', () => {
    const { result } = renderHook(() => useWebhookFilters(), {
      wrapper: createWrapper(),
    })

    expect(result.current.filters).toEqual({
      eventType: '',
      processed: '',
      startDate: '',
      endDate: '',
      page: 1,
    })
  })

  it('setEventType resets page to 1', () => {
    const { result } = renderHook(() => useWebhookFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setPage(3)
    })
    expect(result.current.filters.page).toBe(3)

    act(() => {
      result.current.setEventType('subscription.created')
    })
    expect(result.current.filters.eventType).toBe('subscription.created')
    expect(result.current.filters.page).toBe(1)
  })

  it('setProcessed resets page to 1', () => {
    const { result } = renderHook(() => useWebhookFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setPage(2)
    })

    act(() => {
      result.current.setProcessed('true')
    })
    expect(result.current.filters.processed).toBe('true')
    expect(result.current.filters.page).toBe(1)
  })

  it('setPage does not reset other filters', () => {
    const { result } = renderHook(() => useWebhookFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setEventType('transaction.completed')
      result.current.setProcessed('false')
    })

    act(() => {
      result.current.setPage(4)
    })

    expect(result.current.filters.page).toBe(4)
    expect(result.current.filters.eventType).toBe('transaction.completed')
    expect(result.current.filters.processed).toBe('false')
  })

  it('hasActiveFilters is false by default', () => {
    const { result } = renderHook(() => useWebhookFilters(), {
      wrapper: createWrapper(),
    })
    expect(result.current.hasActiveFilters).toBe(false)
  })

  it('hasActiveFilters is true when eventType is set', () => {
    const { result } = renderHook(() => useWebhookFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setEventType('subscription.canceled')
    })

    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('hasActiveFilters is true when processed is set', () => {
    const { result } = renderHook(() => useWebhookFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setProcessed('false')
    })

    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('setStartDate resets page to 1', () => {
    const { result } = renderHook(() => useWebhookFilters(), {
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
    const { result } = renderHook(() => useWebhookFilters(), {
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
    const { result } = renderHook(() => useWebhookFilters(), {
      wrapper: createWrapper(),
    })

    act(() => {
      result.current.setEventType('subscription.created')
      result.current.setProcessed('true')
      result.current.setStartDate('2026-01-01')
      result.current.setEndDate('2026-03-22')
      result.current.setPage(5)
    })

    act(() => {
      result.current.resetFilters()
    })

    expect(result.current.filters).toEqual({
      eventType: '',
      processed: '',
      startDate: '',
      endDate: '',
      page: 1,
    })
    expect(result.current.hasActiveFilters).toBe(false)
  })

  describe('sort state', () => {
    it('returns null sort by default', () => {
      const { result } = renderHook(() => useWebhookFilters(), {
        wrapper: createMemoryWrapper('/admin/webhooks'),
      })

      expect(result.current.sort).toBeNull()
    })

    it('setSort updates sort and resets page to 1', () => {
      const { result } = renderHook(() => useWebhookFilters(), {
        wrapper: createMemoryWrapper('/admin/webhooks'),
      })

      act(() => {
        result.current.setPage(3)
      })
      expect(result.current.filters.page).toBe(3)

      act(() => {
        result.current.setSort({ columnId: 'attempts', direction: 'asc' })
      })

      expect(result.current.sort).toEqual({ columnId: 'attempts', direction: 'asc' })
      expect(result.current.filters.page).toBe(1)
    })

    it('setSort(null) clears sort', () => {
      const { result } = renderHook(() => useWebhookFilters(), {
        wrapper: createMemoryWrapper('/admin/webhooks?sort=created_at:desc'),
      })

      expect(result.current.sort).toEqual({ columnId: 'created_at', direction: 'desc' })

      act(() => {
        result.current.setSort(null)
      })

      expect(result.current.sort).toBeNull()
    })

    it('apiParams includes sort_by and sort_dir when sort is set', () => {
      const { result } = renderHook(() => useWebhookFilters(), {
        wrapper: createMemoryWrapper('/admin/webhooks'),
      })

      act(() => {
        result.current.setSort({ columnId: 'attempts', direction: 'asc' })
      })

      expect(result.current.apiParams.sort_by).toBe('attempts')
      expect(result.current.apiParams.sort_dir).toBe('asc')
    })

    it('apiParams omits sort_by and sort_dir when sort is null', () => {
      const { result } = renderHook(() => useWebhookFilters(), {
        wrapper: createMemoryWrapper('/admin/webhooks'),
      })

      expect(result.current.apiParams.sort_by).toBeUndefined()
      expect(result.current.apiParams.sort_dir).toBeUndefined()
    })

    it('resetFilters preserves active sort', () => {
      const { result } = renderHook(() => useWebhookFilters(), {
        wrapper: createMemoryWrapper('/admin/webhooks?sort=created_at:desc'),
      })

      act(() => {
        result.current.setEventType('subscription.created')
        result.current.setPage(3)
      })

      act(() => {
        result.current.resetFilters()
      })

      expect(result.current.hasActiveFilters).toBe(false)
      expect(result.current.filters.page).toBe(1)
      expect(result.current.sort).toEqual({ columnId: 'created_at', direction: 'desc' })
    })
  })
})
