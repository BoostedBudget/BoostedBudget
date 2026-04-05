import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import {
  mockUseGetSpendingTrends,
} from '@/test/mocks/analytics-api'

import { useSpendingTrends } from './useSpendingTrends'

describe('useSpendingTrends', () => {
  beforeEach(() => {
    mockUseGetSpendingTrends.mockReset()
    mockUseGetSpendingTrends.mockImplementation(() => ({
      data: null,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    }))
  })

  it('returns null data when no date filters', () => {
    const { result } = renderHook(() =>
      useSpendingTrends({ filters: { startDate: '', endDate: '' } })
    )
    expect(result.current.data).toBeNull()
    expect(result.current.loading).toBe(false)
  })

  it('returns data when API call succeeds', () => {
    const mockData = {
      start_date: '2024-01-01',
      end_date: '2024-03-31',
      top_categories: ['FOOD_AND_DRINK'],
      periods: [
        {
          period_start: '2024-01-01',
          period_end: '2024-01-31',
          total: 280,
          categories: [{ category: 'FOOD_AND_DRINK', display_name: 'Food & Drink', total: 280 }],
        },
      ],
    }
    mockUseGetSpendingTrends.mockImplementation(() => ({
      data: mockData,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    }))

    const { result } = renderHook(() =>
      useSpendingTrends({
        filters: { startDate: '2024-01-01', endDate: '2024-03-31' },
      })
    )
    expect(result.current.data).toEqual(mockData)
    expect(result.current.error).toBeNull()
  })
})
