import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import {
  mockUseGetSpendingByMerchant,
} from '@/test/mocks/analytics-api'

import { useSpendingByMerchant } from './useSpendingByMerchant'

describe('useSpendingByMerchant', () => {
  beforeEach(() => {
    mockUseGetSpendingByMerchant.mockReset()
    mockUseGetSpendingByMerchant.mockImplementation(() => ({
      data: null,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    }))
  })

  it('returns null data when no date filters', () => {
    const { result } = renderHook(() =>
      useSpendingByMerchant({ filters: { startDate: '', endDate: '' } })
    )
    expect(result.current.data).toBeNull()
    expect(result.current.loading).toBe(false)
  })

  it('returns data when API call succeeds', () => {
    const mockData = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      top_n: 10,
      merchants: [
        { merchant_name: 'Starbucks', total: 150, transaction_count: 15, percentage: 30 },
      ],
    }
    mockUseGetSpendingByMerchant.mockImplementation(() => ({
      data: mockData,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    }))

    const { result } = renderHook(() =>
      useSpendingByMerchant({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
        limit: 10,
      })
    )
    expect(result.current.data).toEqual(mockData)
    expect(result.current.error).toBeNull()
  })
})
