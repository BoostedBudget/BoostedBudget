import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { mockUseGetIncomeSourceBreakdown } from '@/test/mocks/analytics-api'
import { useIncomeSourceBreakdown } from './useIncomeSourceBreakdown'

describe('useIncomeSourceBreakdown', () => {
  beforeEach(() => {
    mockUseGetIncomeSourceBreakdown.mockReset()
    mockUseGetIncomeSourceBreakdown.mockImplementation(() => ({
      data: null,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    }))
  })

  it('returns null data and loading false when no date filters', () => {
    const { result } = renderHook(() =>
      useIncomeSourceBreakdown({ filters: { startDate: '', endDate: '' } }),
    )
    expect(result.current.data).toBeNull()
    expect(result.current.loading).toBe(false)
    expect(mockUseGetIncomeSourceBreakdown).not.toHaveBeenCalledWith(
      expect.objectContaining({ start_date: expect.anything() }),
      expect.anything(),
    )
  })

  it('does not fetch when enabled=false', () => {
    renderHook(() =>
      useIncomeSourceBreakdown({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
        enabled: false,
      }),
    )
    const lastCall = mockUseGetIncomeSourceBreakdown.mock.calls.at(-1)
    const opts = lastCall?.[1] as { query?: { enabled?: boolean } } | undefined
    expect(opts?.query?.enabled).toBe(false)
  })

  it('returns loading true while fetching', () => {
    mockUseGetIncomeSourceBreakdown.mockImplementation(() => ({
      data: null,
      isLoading: true,
      error: null,
      refetch: () => Promise.resolve({}),
    }))
    const { result } = renderHook(() =>
      useIncomeSourceBreakdown({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
      }),
    )
    expect(result.current.loading).toBe(true)
  })

  it('returns data when API call succeeds', () => {
    const mockData = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      total_income: 5000,
      sources: [
        { category: 'INCOME_WAGES', pfc_primary_display: 'Wages', total: 5000, percentage: 100, transaction_count: 2 },
      ],
    }
    mockUseGetIncomeSourceBreakdown.mockImplementation(() => ({
      data: mockData,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    }))
    const { result } = renderHook(() =>
      useIncomeSourceBreakdown({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
      }),
    )
    expect(result.current.data).toEqual(mockData)
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('returns error string when API call fails', () => {
    mockUseGetIncomeSourceBreakdown.mockImplementation(() => ({
      data: null,
      isLoading: false,
      error: new Error('Network error'),
      refetch: () => Promise.resolve({}),
    }))
    const { result } = renderHook(() =>
      useIncomeSourceBreakdown({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
      }),
    )
    expect(result.current.error).toBe('Network error')
  })

  it('passes start_date and end_date params to the query', () => {
    renderHook(() =>
      useIncomeSourceBreakdown({
        filters: { startDate: '2024-03-01', endDate: '2024-03-31' },
      }),
    )
    const params = mockUseGetIncomeSourceBreakdown.mock.calls.at(-1)?.[0] as
      | Record<string, unknown>
      | undefined
    expect(params?.start_date).toBe('2024-03-01')
    expect(params?.end_date).toBe('2024-03-31')
  })
})
