/**
 * useCashFlowData Hook Tests
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 * Issue #575: Migrated to bun:test with centralized API mocks
 *
 * Tests for useCashFlowSummary and useCashFlowHistory hooks.
 * NOTE: Runs with API-level mocks (analytics-api.ts), NOT wrapper mocks (dashboard-hooks.ts)
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useCashFlowSummary, useCashFlowHistory } from './useCashFlowData'
import type { CashFlowSummary, CashFlowHistory, CashFlowFilters } from '../types'
import {
  mockUseGetCashFlowSummary,
  mockUseGetCashFlowHistory,
  mockRefetchCashFlowSummary,
  mockRefetchCashFlowHistory,
} from '@/test/mocks/analytics-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

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
    ],
  }

  beforeEach(() => {
    mockUseGetCashFlowSummary.mockClear()
    mockUseGetCashFlowHistory.mockClear()
    mockRefetchCashFlowSummary.mockClear()
    mockRefetchCashFlowHistory.mockClear()
  })

  function configureSummaryQuery(config: {
    data?: CashFlowSummary | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetCashFlowSummary.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchCashFlowSummary,
    })
  }

  const configureHistoryQuery = (config: {
    data?: CashFlowHistory | null
    isLoading?: boolean
    error?: Error | null
  }) =>
    mockUseGetCashFlowHistory.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchCashFlowHistory,
    })

  describe('useCashFlowSummary', () => {
    describe('Initial State', () => {
      it('should return loading=true initially when enabled', () => {
        configureSummaryQuery({ isLoading: true })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(true)
      })

      it('should return data=null initially', () => {
        configureSummaryQuery({ isLoading: true })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.data).toBeNull()
      })

      it('should return error=null initially', () => {
        configureSummaryQuery({ isLoading: true })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.error).toBeNull()
      })
    })

    describe('Fetch on Mount', () => {
      it('should call the query hook with correct params', async () => {
        configureSummaryQuery({ data: mockSummary })

        renderHook(() => useCashFlowSummary({ filters: mockFilters }), {
          wrapper: createWrapper(),
        })

        expect(mockUseGetCashFlowSummary).toHaveBeenCalledWith(
          expect.objectContaining({
            start_date: '2024-01-01',
            end_date: '2024-01-31',
          }),
          expect.any(Object)
        )
      })

      it('should set data from query response', async () => {
        configureSummaryQuery({ data: mockSummary })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        await waitFor(() => {
          expect(result.current.data).toEqual(mockSummary)
        })
      })

      it('should set loading=false after fetch', async () => {
        configureSummaryQuery({ data: mockSummary })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })
      })
    })

    describe('Error Handling', () => {
      it('should handle errors with Error message', async () => {
        configureSummaryQuery({ error: new Error('Network error') })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.error).toBe('Network error')
      })

      it('should set data=null on error', async () => {
        configureSummaryQuery({ error: new Error('Error') })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.data).toBeNull()
      })

      it('should set loading=false on error', async () => {
        configureSummaryQuery({ error: new Error('Error') })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(false)
      })
    })

    describe('Enabled Flag', () => {
      it('should not set loading when disabled', async () => {
        configureSummaryQuery({ isLoading: false })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters, enabled: false }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(false)
      })
    })

    describe('Missing Dates', () => {
      it('should not be loading when startDate is missing', () => {
        configureSummaryQuery({ isLoading: false })

        const incompleteFilters: CashFlowFilters = {
          startDate: '',
          endDate: '2024-01-31',
        }

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: incompleteFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(false)
      })

      it('should not be loading when endDate is missing', () => {
        configureSummaryQuery({ isLoading: false })

        const incompleteFilters: CashFlowFilters = {
          startDate: '2024-01-01',
          endDate: '',
        }

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: incompleteFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(false)
      })
    })

    describe('Refetch', () => {
      it('should call refetch when refetch is called', async () => {
        configureSummaryQuery({ data: mockSummary })

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })

        await act(async () => {
          await result.current.refetch()
        })

        expect(mockRefetchCashFlowSummary).toHaveBeenCalled()
      })
    })
  })

  describe('useCashFlowHistory', () => {
    describe('Initial State', () => {
      it('should return loading=true initially when enabled', () => {
        configureHistoryQuery({ isLoading: true })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(true)
      })

      it('should return data=null initially', () => {
        configureHistoryQuery({ isLoading: true })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.data).toBeNull()
      })

      it('should return error=null initially', () => {
        configureHistoryQuery({ isLoading: true })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.error).toBeNull()
      })
    })

    describe('Fetch on Mount', () => {
      it('should call the query hook with correct params', async () => {
        configureHistoryQuery({ data: mockHistory })

        renderHook(() => useCashFlowHistory({ filters: mockFilters }), {
          wrapper: createWrapper(),
        })

        expect(mockUseGetCashFlowHistory).toHaveBeenCalledWith(
          expect.objectContaining({
            start_date: '2024-01-01',
            end_date: '2024-01-31',
          }),
          expect.any(Object)
        )
      })

      it('should set data from query response', async () => {
        configureHistoryQuery({ data: mockHistory })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        await waitFor(() => {
          expect(result.current.data).toEqual(mockHistory)
        })
      })

      it('should set loading=false after fetch', async () => {
        configureHistoryQuery({ data: mockHistory })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })
      })
    })

    describe('Error Handling', () => {
      it('should handle errors with Error message', async () => {
        configureHistoryQuery({ error: new Error('Server error') })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.error).toBe('Server error')
      })

      it('should set data=null on error', async () => {
        configureHistoryQuery({ error: new Error('Error') })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.data).toBeNull()
      })

      it('should set loading=false on error', async () => {
        configureHistoryQuery({ error: new Error('Error') })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(false)
      })
    })

    describe('Enabled Flag', () => {
      it('should not set loading when disabled', async () => {
        configureHistoryQuery({ isLoading: false })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters, enabled: false }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(false)
      })
    })

    describe('Missing Dates', () => {
      it('should not be loading when startDate is missing', () => {
        configureHistoryQuery({ isLoading: false })

        const incompleteFilters: CashFlowFilters = {
          startDate: '',
          endDate: '2024-01-31',
        }

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: incompleteFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(false)
      })

      it('should not be loading when endDate is missing', () => {
        configureHistoryQuery({ isLoading: false })

        const incompleteFilters: CashFlowFilters = {
          startDate: '2024-01-01',
          endDate: '',
        }

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: incompleteFilters }),
          { wrapper: createWrapper() }
        )

        expect(result.current.loading).toBe(false)
      })
    })

    describe('Refetch', () => {
      it('should call refetch when refetch is called', async () => {
        configureHistoryQuery({ data: mockHistory })

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        )

        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })

        await act(async () => {
          await result.current.refetch()
        })

        expect(mockRefetchCashFlowHistory).toHaveBeenCalled()
      })
    })
  })
})
