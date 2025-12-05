/**
 * useCashFlowData Hook Tests
 *
 * Tests for useCashFlowSummary and useCashFlowHistory hooks
 * including loading, error handling, and data fetching.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useCashFlowSummary, useCashFlowHistory } from './useCashFlowData'
import * as analyticsApi from '../api/analyticsApi'
import type { CashFlowSummary, CashFlowHistory, CashFlowFilters } from '../types'

// Mock the API module
vi.mock('../api/analyticsApi')

describe('useCashFlowData', () => {
  const mockFilters: CashFlowFilters = {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
  }

  const mockSummary: CashFlowSummary = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    income: 5000,
    expenses: 3000,
    net_cash_flow: 2000,
    income_count: 10,
    expense_count: 25,
    period_comparison: {
      previous_income: 4500,
      previous_expenses: 2800,
      previous_net_cash_flow: 1700,
      income_change_amount: 500,
      income_change_percent: 11.11,
      expense_change_amount: 200,
      expense_change_percent: 7.14,
      net_change_amount: 300,
      net_change_percent: 17.65,
    },
  }

  const mockHistory: CashFlowHistory = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    grouping: 'weekly',
    periods: [
      {
        period_start: '2024-01-01',
        period_end: '2024-01-07',
        income: 1200,
        expenses: 800,
        net_cash_flow: 400,
        transaction_count: 10,
      },
      {
        period_start: '2024-01-08',
        period_end: '2024-01-14',
        income: 1100,
        expenses: 750,
        net_cash_flow: 350,
        transaction_count: 8,
      },
      {
        period_start: '2024-01-15',
        period_end: '2024-01-21',
        income: 1300,
        expenses: 900,
        net_cash_flow: 400,
        transaction_count: 12,
      },
      {
        period_start: '2024-01-22',
        period_end: '2024-01-31',
        income: 1400,
        expenses: 550,
        net_cash_flow: 850,
        transaction_count: 5,
      },
    ],
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('useCashFlowSummary', () => {
    describe('Initial State', () => {
      it('should return loading=true initially when enabled', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockImplementation(
          () => new Promise(() => {})
        )

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        expect(result.current.loading).toBe(true)
      })

      it('should return data=null initially', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockImplementation(
          () => new Promise(() => {})
        )

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        expect(result.current.data).toBeNull()
      })

      it('should return error=null initially', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockImplementation(
          () => new Promise(() => {})
        )

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        expect(result.current.error).toBeNull()
      })
    })

    describe('Fetch on Mount', () => {
      it('should fetch data on mount', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        renderHook(() => useCashFlowSummary({ filters: mockFilters }))

        await waitFor(() => {
          expect(analyticsApi.getCashFlowSummary).toHaveBeenCalledTimes(1)
        })
      })

      it('should pass filters to API', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        renderHook(() => useCashFlowSummary({ filters: mockFilters }))

        await waitFor(() => {
          expect(analyticsApi.getCashFlowSummary).toHaveBeenCalledWith(mockFilters)
        })
      })

      it('should set data from API response', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.data).toEqual(mockSummary)
        })
      })

      it('should set loading=false after fetch', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })
      })
    })

    describe('Error Handling', () => {
      it('should handle errors with Error message', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockRejectedValue(
          new Error('Network error')
        )

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.error).toBe('Network error')
        })
      })

      it('should handle non-Error errors with default message', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockRejectedValue(
          'Unknown error'
        )

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.error).toBe('Failed to fetch cash flow summary')
        })
      })

      it('should set data=null on error', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockRejectedValue(
          new Error('Error')
        )

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.data).toBeNull()
        })
      })

      it('should set loading=false on error', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockRejectedValue(
          new Error('Error')
        )

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })
      })
    })

    describe('Enabled Flag', () => {
      it('should not fetch when enabled=false', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        renderHook(() =>
          useCashFlowSummary({ filters: mockFilters, enabled: false })
        )

        // Wait a bit to ensure no calls are made
        await new Promise((resolve) => setTimeout(resolve, 50))

        expect(analyticsApi.getCashFlowSummary).not.toHaveBeenCalled()
      })

      it('should not set loading when disabled', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters, enabled: false })
        )

        // Wait a bit
        await new Promise((resolve) => setTimeout(resolve, 50))

        // Initial loading is false (not set to true since fetch doesn't start)
        expect(result.current.loading).toBe(false)
      })
    })

    describe('Missing Dates', () => {
      it('should not fetch when startDate is missing', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        const incompleteFilters: CashFlowFilters = {
          startDate: '',
          endDate: '2024-01-31',
        }

        renderHook(() =>
          useCashFlowSummary({ filters: incompleteFilters })
        )

        await new Promise((resolve) => setTimeout(resolve, 50))

        expect(analyticsApi.getCashFlowSummary).not.toHaveBeenCalled()
      })

      it('should not fetch when endDate is missing', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        const incompleteFilters: CashFlowFilters = {
          startDate: '2024-01-01',
          endDate: '',
        }

        renderHook(() =>
          useCashFlowSummary({ filters: incompleteFilters })
        )

        await new Promise((resolve) => setTimeout(resolve, 50))

        expect(analyticsApi.getCashFlowSummary).not.toHaveBeenCalled()
      })
    })

    describe('Refetch', () => {
      it('should refetch when refetch is called', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })

        expect(analyticsApi.getCashFlowSummary).toHaveBeenCalledTimes(1)

        await act(async () => {
          await result.current.refetch()
        })

        expect(analyticsApi.getCashFlowSummary).toHaveBeenCalledTimes(2)
      })

      it('should update data on refetch', async () => {
        const newSummary = { ...mockSummary, income: 6000 }
        vi.mocked(analyticsApi.getCashFlowSummary)
          .mockResolvedValueOnce(mockSummary)
          .mockResolvedValueOnce(newSummary)

        const { result } = renderHook(() =>
          useCashFlowSummary({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.data?.income).toBe(5000)
        })

        await act(async () => {
          await result.current.refetch()
        })

        expect(result.current.data?.income).toBe(6000)
      })
    })

    describe('Filter Changes', () => {
      it('should refetch when filters change', async () => {
        vi.mocked(analyticsApi.getCashFlowSummary).mockResolvedValue(mockSummary)

        const { rerender } = renderHook(
          ({ filters }) => useCashFlowSummary({ filters }),
          { initialProps: { filters: mockFilters } }
        )

        await waitFor(() => {
          expect(analyticsApi.getCashFlowSummary).toHaveBeenCalledTimes(1)
        })

        const newFilters: CashFlowFilters = {
          startDate: '2024-02-01',
          endDate: '2024-02-29',
        }

        rerender({ filters: newFilters })

        await waitFor(() => {
          expect(analyticsApi.getCashFlowSummary).toHaveBeenCalledTimes(2)
        })

        expect(analyticsApi.getCashFlowSummary).toHaveBeenLastCalledWith(
          newFilters
        )
      })
    })
  })

  describe('useCashFlowHistory', () => {
    describe('Initial State', () => {
      it('should return loading=true initially when enabled', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockImplementation(
          () => new Promise(() => {})
        )

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        expect(result.current.loading).toBe(true)
      })

      it('should return data=null initially', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockImplementation(
          () => new Promise(() => {})
        )

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        expect(result.current.data).toBeNull()
      })

      it('should return error=null initially', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockImplementation(
          () => new Promise(() => {})
        )

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        expect(result.current.error).toBeNull()
      })
    })

    describe('Fetch on Mount', () => {
      it('should fetch data on mount', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        renderHook(() => useCashFlowHistory({ filters: mockFilters }))

        await waitFor(() => {
          expect(analyticsApi.getCashFlowHistory).toHaveBeenCalledTimes(1)
        })
      })

      it('should pass filters to API', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        renderHook(() => useCashFlowHistory({ filters: mockFilters }))

        await waitFor(() => {
          expect(analyticsApi.getCashFlowHistory).toHaveBeenCalledWith(mockFilters)
        })
      })

      it('should set data from API response', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.data).toEqual(mockHistory)
        })
      })

      it('should set loading=false after fetch', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })
      })
    })

    describe('Error Handling', () => {
      it('should handle errors with Error message', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockRejectedValue(
          new Error('Server error')
        )

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.error).toBe('Server error')
        })
      })

      it('should handle non-Error errors with default message', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockRejectedValue('oops')

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.error).toBe('Failed to fetch cash flow history')
        })
      })

      it('should set data=null on error', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockRejectedValue(
          new Error('Error')
        )

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.data).toBeNull()
        })
      })

      it('should set loading=false on error', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockRejectedValue(
          new Error('Error')
        )

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })
      })
    })

    describe('Enabled Flag', () => {
      it('should not fetch when enabled=false', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        renderHook(() =>
          useCashFlowHistory({ filters: mockFilters, enabled: false })
        )

        await new Promise((resolve) => setTimeout(resolve, 50))

        expect(analyticsApi.getCashFlowHistory).not.toHaveBeenCalled()
      })

      it('should not set loading when disabled', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters, enabled: false })
        )

        await new Promise((resolve) => setTimeout(resolve, 50))

        expect(result.current.loading).toBe(false)
      })
    })

    describe('Missing Dates', () => {
      it('should not fetch when startDate is missing', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        const incompleteFilters: CashFlowFilters = {
          startDate: '',
          endDate: '2024-01-31',
        }

        renderHook(() =>
          useCashFlowHistory({ filters: incompleteFilters })
        )

        await new Promise((resolve) => setTimeout(resolve, 50))

        expect(analyticsApi.getCashFlowHistory).not.toHaveBeenCalled()
      })

      it('should not fetch when endDate is missing', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        const incompleteFilters: CashFlowFilters = {
          startDate: '2024-01-01',
          endDate: '',
        }

        renderHook(() =>
          useCashFlowHistory({ filters: incompleteFilters })
        )

        await new Promise((resolve) => setTimeout(resolve, 50))

        expect(analyticsApi.getCashFlowHistory).not.toHaveBeenCalled()
      })
    })

    describe('Refetch', () => {
      it('should refetch when refetch is called', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })

        expect(analyticsApi.getCashFlowHistory).toHaveBeenCalledTimes(1)

        await act(async () => {
          await result.current.refetch()
        })

        expect(analyticsApi.getCashFlowHistory).toHaveBeenCalledTimes(2)
      })

      it('should update data on refetch', async () => {
        const newHistory = {
          ...mockHistory,
          periods: [...mockHistory.periods, {
            period_start: '2024-02-01',
            period_end: '2024-02-07',
            income: 1500,
            expenses: 600,
            net_cash_flow: 900,
            transaction_count: 7,
          }],
        }
        vi.mocked(analyticsApi.getCashFlowHistory)
          .mockResolvedValueOnce(mockHistory)
          .mockResolvedValueOnce(newHistory)

        const { result } = renderHook(() =>
          useCashFlowHistory({ filters: mockFilters })
        )

        await waitFor(() => {
          expect(result.current.data?.periods.length).toBe(4)
        })

        await act(async () => {
          await result.current.refetch()
        })

        expect(result.current.data?.periods.length).toBe(5)
      })
    })

    describe('Filter Changes', () => {
      it('should refetch when filters change', async () => {
        vi.mocked(analyticsApi.getCashFlowHistory).mockResolvedValue(mockHistory)

        const { rerender } = renderHook(
          ({ filters }) => useCashFlowHistory({ filters }),
          { initialProps: { filters: mockFilters } }
        )

        await waitFor(() => {
          expect(analyticsApi.getCashFlowHistory).toHaveBeenCalledTimes(1)
        })

        const newFilters: CashFlowFilters = {
          startDate: '2024-02-01',
          endDate: '2024-02-29',
          grouping: 'monthly',
        }

        rerender({ filters: newFilters })

        await waitFor(() => {
          expect(analyticsApi.getCashFlowHistory).toHaveBeenCalledTimes(2)
        })

        expect(analyticsApi.getCashFlowHistory).toHaveBeenLastCalledWith(
          newFilters
        )
      })
    })
  })
})
