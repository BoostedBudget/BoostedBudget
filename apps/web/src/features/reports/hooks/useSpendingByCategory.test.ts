import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import {
  mockUseGetSpendingByCategory,
} from '@/test/mocks/analytics-api'

import { useSpendingByCategory } from './useSpendingByCategory'

describe('useSpendingByCategory', () => {
  beforeEach(() => {
    mockUseGetSpendingByCategory.mockReset()
    mockUseGetSpendingByCategory.mockImplementation(() => ({
      data: null,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    }))
  })

  it('returns null data and loading false when no date filters', () => {
    const { result } = renderHook(() =>
      useSpendingByCategory({ filters: { startDate: '', endDate: '' } })
    )
    expect(result.current.data).toBeNull()
    expect(result.current.loading).toBe(false)
  })

  it('returns loading true while fetching', () => {
    mockUseGetSpendingByCategory.mockImplementation(() => ({
      data: null,
      isLoading: true,
      error: null,
      refetch: () => Promise.resolve({}),
    }))

    const { result } = renderHook(() =>
      useSpendingByCategory({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
      })
    )
    expect(result.current.loading).toBe(true)
  })

  it('returns data when API call succeeds', () => {
    const mockData = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      total_spent: 500,
      categories: [
        { category: 'FOOD_AND_DRINK', display_name: 'Food & Drink', total: 300, percentage: 60, transaction_count: 20 },
      ],
    }
    mockUseGetSpendingByCategory.mockImplementation(() => ({
      data: mockData,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    }))

    const { result } = renderHook(() =>
      useSpendingByCategory({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
      })
    )
    expect(result.current.data).toEqual(mockData)
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('returns error string when API call fails', () => {
    mockUseGetSpendingByCategory.mockImplementation(() => ({
      data: null,
      isLoading: false,
      error: new Error('Network error'),
      refetch: () => Promise.resolve({}),
    }))

    const { result } = renderHook(() =>
      useSpendingByCategory({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
      })
    )
    expect(result.current.error).toBe('Network error')
  })
})
