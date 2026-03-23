/**
 * useWebhookFilters Hook Tests
 * Issue #747: Paddle Webhook Admin UI
 */

import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useWebhookFilters } from './useWebhookFilters'

describe('useWebhookFilters', () => {
  it('returns default state', () => {
    const { result } = renderHook(() => useWebhookFilters())

    expect(result.current.filters).toEqual({
      eventType: '',
      processed: '',
      startDate: '',
      endDate: '',
      page: 1,
    })
  })

  it('setEventType resets page to 1', () => {
    const { result } = renderHook(() => useWebhookFilters())

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
    const { result } = renderHook(() => useWebhookFilters())

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
    const { result } = renderHook(() => useWebhookFilters())

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
    const { result } = renderHook(() => useWebhookFilters())
    expect(result.current.hasActiveFilters).toBe(false)
  })

  it('hasActiveFilters is true when eventType is set', () => {
    const { result } = renderHook(() => useWebhookFilters())

    act(() => {
      result.current.setEventType('subscription.canceled')
    })

    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('hasActiveFilters is true when processed is set', () => {
    const { result } = renderHook(() => useWebhookFilters())

    act(() => {
      result.current.setProcessed('false')
    })

    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('resetFilters clears all and resets page', () => {
    const { result } = renderHook(() => useWebhookFilters())

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
})
