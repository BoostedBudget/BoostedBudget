/**
 * useDashboardData Hook Tests
 * Issue #353: Balance Tracking Dashboard
 * Issue #575: Migrated to bun:test with centralized API mocks
 *
 * Tests the dashboard data fetching hook with gap-filling logic,
 * parallel API calls, filtering, and error handling.
 * NOTE: Runs with API-level mocks (dashboard-api.ts), NOT wrapper mocks (dashboard-hooks.ts)
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useDashboardData } from './useDashboardData'
import type {
  BalanceSummary,
  BalanceHistory,
  AccountTypesResponse,
  DashboardFilters,
} from '../types'
import {
  mockUseGetBalanceSummary,
  mockUseGetBalanceHistory,
  mockUseGetAccountTypes,
  mockRefetchBalanceSummary,
  mockRefetchBalanceHistory,
  mockRefetchAccountTypes,
} from '@/test/mocks/dashboard-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useDashboardData', () => {
  const mockBalanceSummary: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 15000.5,
    by_type: {
      depository: {
        balance: 5000.0,
        account_count: 2,
        accounts: [
          { id: 'acc-1', name: 'Checking', balance: 3000.0 },
          { id: 'acc-2', name: 'Savings', balance: 2000.0 },
        ],
      },
    },
    period_comparison: {
      previous_balance: 14000.0,
      change_amount: 1000.5,
      change_percent: 7.15,
    },
  }

  const mockBalanceHistory: BalanceHistory = {
    start_date: '2025-11-10',
    end_date: '2025-11-15',
    granularity: 'daily',
    snapshots: [
      {
        date: '2025-11-10',
        balance: 14000.0,
        by_type: { depository: 4500.0, credit: -1500.0 },
      },
      {
        date: '2025-11-12',
        balance: 14500.0,
        by_type: { depository: 5000.0, credit: -1500.0 },
      },
      {
        date: '2025-11-15',
        balance: 15000.5,
        by_type: { depository: 5000.0, credit: -2000.0 },
      },
    ],
  }

  const mockAccountTypes: AccountTypesResponse = {
    types: [
      {
        key: 'depository',
        label: 'Checking & Savings',
        count: 2,
        total_balance: 5000.0,
      },
      { key: 'credit', label: 'Credit Cards', count: 1, total_balance: -2000.0 },
    ],
  }

  const mockEmptyHistory: BalanceHistory = {
    start_date: '2025-11-10',
    end_date: '2025-11-15',
    granularity: 'daily',
    snapshots: [],
  }

  beforeEach(() => {
    mockUseGetBalanceSummary.mockClear()
    mockUseGetBalanceHistory.mockClear()
    mockUseGetAccountTypes.mockClear()
    mockRefetchBalanceSummary.mockClear()
    mockRefetchBalanceHistory.mockClear()
    mockRefetchAccountTypes.mockClear()
  })

  const configureQuery = (
    mock: typeof mockUseGetBalanceSummary,
    refetch: typeof mockRefetchBalanceSummary,
    config?: { data?: unknown | null; isLoading?: boolean; error?: Error | null }
  ) =>
    mock.mockReturnValue({
      data: config?.data ?? undefined,
      isLoading: config?.isLoading ?? false,
      error: config?.error ?? null,
      refetch,
    })

  function configureQueries(config: {
    summary?: {
      data?: BalanceSummary | null
      isLoading?: boolean
      error?: Error | null
    }
    history?: {
      data?: BalanceHistory | null
      isLoading?: boolean
      error?: Error | null
    }
    types?: {
      data?: AccountTypesResponse | null
      isLoading?: boolean
      error?: Error | null
    }
  }) {
    configureQuery(mockUseGetBalanceSummary, mockRefetchBalanceSummary, config.summary)
    configureQuery(mockUseGetBalanceHistory, mockRefetchBalanceHistory, config.history)
    configureQuery(mockUseGetAccountTypes, mockRefetchAccountTypes, config.types)
  }

  describe('Basic Functionality', () => {
    it('should return loading state initially', () => {
      configureQueries({
        summary: { isLoading: true },
        history: { isLoading: true },
        types: { isLoading: true },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.summary).toBeNull()
      expect(result.current.history).toBeNull()
      expect(result.current.accountTypes).toBeNull()
      expect(result.current.error).toBeNull()
    })

    it('should populate data from queries', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.summary).toEqual(mockBalanceSummary)
      expect(result.current.accountTypes).toEqual(mockAccountTypes)
      expect(result.current.error).toBeNull()
    })

    it('should call hooks with correct params', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const filters: DashboardFilters = {
        accountTypes: ['depository'],
        asOfDate: '2025-11-15',
        startDate: '2025-11-01',
        endDate: '2025-11-15',
      }

      renderHook(() => useDashboardData(filters), {
        wrapper: createWrapper(),
      })

      expect(mockUseGetBalanceSummary).toHaveBeenCalledWith(
        expect.objectContaining({
          account_types: 'depository',
          as_of_date: '2025-11-15',
        }),
        expect.any(Object)
      )

      expect(mockUseGetBalanceHistory).toHaveBeenCalledWith(
        expect.objectContaining({
          account_types: 'depository',
          start_date: '2025-11-01',
          end_date: '2025-11-15',
        }),
        expect.any(Object)
      )
    })

    // Issue #1370: Personal/Household dashboard scope toggle — the key regression
    // this task must guard: `scope` must reach the actual generated-hook params.
    it('should forward scope: "household" to both summary and history query params', () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const filters: DashboardFilters = {
        startDate: '2025-11-01',
        endDate: '2025-11-15',
        scope: 'household',
      }

      renderHook(() => useDashboardData(filters), {
        wrapper: createWrapper(),
      })

      expect(mockUseGetBalanceSummary).toHaveBeenCalledWith(
        expect.objectContaining({ scope: 'household' }),
        expect.any(Object)
      )
      expect(mockUseGetBalanceHistory).toHaveBeenCalledWith(
        expect.objectContaining({ scope: 'household' }),
        expect.any(Object)
      )
    })

    it('should default scope to undefined (personal) when not specified', () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      renderHook(() => useDashboardData({ startDate: '2025-11-01', endDate: '2025-11-15' }), {
        wrapper: createWrapper(),
      })

      expect(mockUseGetBalanceSummary).toHaveBeenCalledWith(
        expect.objectContaining({ scope: undefined }),
        expect.any(Object)
      )
    })
  })

  describe('Error Handling', () => {
    it('should handle summary query errors', async () => {
      const error = new Error('Failed to fetch balance summary')
      configureQueries({
        summary: { error },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual(error)
    })

    it('should handle history query errors', async () => {
      const error = new Error('Failed to fetch balance history')
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { error },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual(error)
    })

    it('should handle account types query errors', async () => {
      const error = new Error('Failed to fetch account types')
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { error },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual(error)
    })
  })

  describe('Gap-Filling Logic', () => {
    it('should fill missing days in balance history', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Original has 3 snapshots (11/10, 11/12, 11/15)
      // Should fill gaps: 11/11, 11/13, 11/14
      expect(result.current.history?.snapshots).toHaveLength(6)

      const snapshots = result.current.history?.snapshots || []
      const dates = snapshots.map((s) => s.date)

      expect(dates).toEqual([
        '2025-11-10',
        '2025-11-11', // Filled
        '2025-11-12',
        '2025-11-13', // Filled
        '2025-11-14', // Filled
        '2025-11-15',
      ])
    })

    it('should use forward-fill strategy for missing days', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const snapshots = result.current.history?.snapshots || []

      // 11/11 should inherit from 11/10
      const nov11 = snapshots.find((s) => s.date === '2025-11-11')
      expect(nov11?.balance).toBe(14000.0)
      expect(nov11?.by_type).toEqual({ depository: 4500.0, credit: -1500.0 })

      // 11/13 should inherit from 11/12
      const nov13 = snapshots.find((s) => s.date === '2025-11-13')
      expect(nov13?.balance).toBe(14500.0)
    })

    // ─── #1626 Phase 3, Defect 1 ─────────────────────────────────────────────────────────
    // The fill used to run to end_date, so a range whose end lies in the future had the last
    // real snapshot copied forward into every day that had not happened yet — the Balance
    // History chart plotted a flat line out to Sep 29 while the newest snapshot was Sep 8.
    // The fill now stops at min(last real snapshot, today in UTC).
    describe('Future-Date Clamping', () => {
      const dayKey = (offsetDays: number) => {
        const d = new Date()
        d.setUTCDate(d.getUTCDate() + offsetDays)
        return d.toISOString().slice(0, 10)
      }

      it('does not fill past the last real snapshot when end_date is in the future', async () => {
        const history: BalanceHistory = {
          start_date: dayKey(-4),
          end_date: dayKey(20),
          granularity: 'daily',
          snapshots: [
            { date: dayKey(-4), balance: 100, by_type: { depository: 100 } },
            { date: dayKey(-1), balance: 250, by_type: { depository: 250 } },
          ],
        }
        configureQueries({
          summary: { data: mockBalanceSummary },
          history: { data: history },
          types: { data: mockAccountTypes },
        })

        const { result } = renderHook(() => useDashboardData(), {
          wrapper: createWrapper(),
        })
        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })

        const dates = (result.current.history?.snapshots ?? []).map((s) => s.date)
        expect(dates).toEqual([dayKey(-4), dayKey(-3), dayKey(-2), dayKey(-1)])
        // The two intermediate days are still forward-filled — the fix removes the future,
        // not the gap-filling.
        const filled = result.current.history?.snapshots.find((s) => s.date === dayKey(-3))
        expect(filled?.balance).toBe(100)
      })

      it('stops at today when the snapshots themselves run into the future', async () => {
        // A future-dated snapshot row would survive a naive "stop at the array's last row"
        // cutoff and take the fill with it, so the cutoff is the EARLIER of the two.
        const history: BalanceHistory = {
          start_date: dayKey(-2),
          end_date: dayKey(20),
          granularity: 'daily',
          snapshots: [
            { date: dayKey(-2), balance: 100, by_type: { depository: 100 } },
            { date: dayKey(3), balance: 900, by_type: { depository: 900 } },
          ],
        }
        configureQueries({
          summary: { data: mockBalanceSummary },
          history: { data: history },
          types: { data: mockAccountTypes },
        })

        const { result } = renderHook(() => useDashboardData(), {
          wrapper: createWrapper(),
        })
        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })

        const snapshots = result.current.history?.snapshots ?? []
        const dates = snapshots.map((s) => s.date)
        expect(dates.slice(0, 3)).toEqual([dayKey(-2), dayKey(-1), dayKey(0)])
        // Nothing between today and the real future row is invented...
        expect(dates).not.toContain(dayKey(1))
        expect(dates).not.toContain(dayKey(2))
        // ...and the real row itself is not deleted, only left standing alone.
        expect(snapshots.find((s) => s.date === dayKey(3))?.balance).toBe(900)
        expect(dates).toEqual([dayKey(-2), dayKey(-1), dayKey(0), dayKey(3)])
      })

      it('still skips days before the first snapshot', async () => {
        const history: BalanceHistory = {
          start_date: dayKey(-6),
          end_date: dayKey(10),
          granularity: 'daily',
          snapshots: [{ date: dayKey(-2), balance: 100, by_type: { depository: 100 } }],
        }
        configureQueries({
          summary: { data: mockBalanceSummary },
          history: { data: history },
          types: { data: mockAccountTypes },
        })

        const { result } = renderHook(() => useDashboardData(), {
          wrapper: createWrapper(),
        })
        await waitFor(() => {
          expect(result.current.loading).toBe(false)
        })

        expect((result.current.history?.snapshots ?? []).map((s) => s.date)).toEqual([dayKey(-2)])
      })
    })

    it('should handle empty snapshots array', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockEmptyHistory },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.history).toEqual(mockEmptyHistory)
      expect(result.current.history?.snapshots).toHaveLength(0)
    })
  })

  describe('Refetch Function', () => {
    it('should provide refetch function', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.refetch).toBeInstanceOf(Function)
    })

    it('should call all refetch functions when refetch is called', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.refetch()
      })

      expect(mockRefetchBalanceSummary).toHaveBeenCalled()
      expect(mockRefetchBalanceHistory).toHaveBeenCalled()
      expect(mockRefetchAccountTypes).toHaveBeenCalled()
    })
  })

  describe('Edge Cases', () => {
    it('should handle null history gracefully', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: null },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.history).toBeNull()
      expect(result.current.summary).toEqual(mockBalanceSummary)
      expect(result.current.accountTypes).toEqual(mockAccountTypes)
    })

    it('should handle loading state from any query', () => {
      // Only summary is loading
      configureQueries({
        summary: { isLoading: true },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      })

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
    })
  })
})
