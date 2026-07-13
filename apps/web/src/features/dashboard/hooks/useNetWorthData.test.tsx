/**
 * useNetWorthData Hook Tests
 * Issue #1370: Household feature — added to exercise the real hook implementation.
 *
 * Previously only wrapper-level mocked (net-worth-hooks.ts) by NetWorthWidget/NetWorthPage
 * tests, so nothing exercised the real params-building logic. Runs via
 * scripts/test-conflict-files.sh (Group D) from /tmp, bypassing the globally preloaded
 * net-worth-hooks.ts wrapper mock, using API-level mocks (dashboard-api.ts) instead —
 * mirrors useDashboardData.test.tsx's isolation strategy.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useNetWorthData } from './useNetWorthData'
import type { NetWorthSummary, NetWorthHistory, DashboardFilters } from '../types'
import {
  mockUseGetNetWorth,
  mockUseGetNetWorthHistory,
  mockRefetchNetWorth,
  mockRefetchNetWorthHistory,
} from '@/test/mocks/dashboard-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useNetWorthData', () => {
  const mockSummary: NetWorthSummary = {
    as_of_date: '2025-11-15',
    net_worth: 50000,
    total_assets: 75000,
    total_liabilities: 25000,
    by_category: {},
  }

  const mockHistory: NetWorthHistory = {
    start_date: '2025-11-10',
    end_date: '2025-11-15',
    granularity: 'daily',
    snapshots: [
      { date: '2025-11-10', net_worth: 48000, total_assets: 73000, total_liabilities: 25000 },
      { date: '2025-11-15', net_worth: 50000, total_assets: 75000, total_liabilities: 25000 },
    ],
  }

  beforeEach(() => {
    mockUseGetNetWorth.mockClear()
    mockUseGetNetWorthHistory.mockClear()
    mockRefetchNetWorth.mockClear()
    mockRefetchNetWorthHistory.mockClear()
  })

  const configureQueries = (config: {
    summary?: { data?: NetWorthSummary | null; isLoading?: boolean; error?: Error | null }
    history?: { data?: NetWorthHistory | null; isLoading?: boolean; error?: Error | null }
  }) => {
    mockUseGetNetWorth.mockReturnValue({
      data: config.summary?.data ?? undefined,
      isLoading: config.summary?.isLoading ?? false,
      error: config.summary?.error ?? null,
      refetch: mockRefetchNetWorth,
    })
    mockUseGetNetWorthHistory.mockReturnValue({
      data: config.history?.data ?? undefined,
      isLoading: config.history?.isLoading ?? false,
      error: config.history?.error ?? null,
      refetch: mockRefetchNetWorthHistory,
    })
  }

  describe('Basic Functionality', () => {
    it('should return loading state initially', () => {
      configureQueries({ summary: { isLoading: true }, history: { isLoading: true } })

      const { result } = renderHook(() => useNetWorthData(), { wrapper: createWrapper() })

      expect(result.current.loading).toBe(true)
      expect(result.current.summary).toBeNull()
      expect(result.current.history).toBeNull()
    })

    it('should populate data from queries', async () => {
      configureQueries({ summary: { data: mockSummary }, history: { data: mockHistory } })

      const { result } = renderHook(() => useNetWorthData(), { wrapper: createWrapper() })

      await waitFor(() => expect(result.current.loading).toBe(false))
      expect(result.current.summary).toEqual(mockSummary)
    })

    it('should call hooks with account_types/as_of_date/date-range params', () => {
      configureQueries({ summary: { data: mockSummary }, history: { data: mockHistory } })

      const filters: DashboardFilters = {
        accountTypes: ['depository'],
        asOfDate: '2025-11-15',
        startDate: '2025-11-01',
        endDate: '2025-11-15',
      }

      renderHook(() => useNetWorthData(filters), { wrapper: createWrapper() })

      expect(mockUseGetNetWorth).toHaveBeenCalledWith(
        expect.objectContaining({ account_types: 'depository', as_of_date: '2025-11-15' }),
        expect.any(Object)
      )
      expect(mockUseGetNetWorthHistory).toHaveBeenCalledWith(
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
      configureQueries({ summary: { data: mockSummary }, history: { data: mockHistory } })

      const filters: DashboardFilters = {
        startDate: '2025-11-01',
        endDate: '2025-11-15',
        scope: 'household',
      }

      renderHook(() => useNetWorthData(filters), { wrapper: createWrapper() })

      expect(mockUseGetNetWorth).toHaveBeenCalledWith(
        expect.objectContaining({ scope: 'household' }),
        expect.any(Object)
      )
      expect(mockUseGetNetWorthHistory).toHaveBeenCalledWith(
        expect.objectContaining({ scope: 'household' }),
        expect.any(Object)
      )
    })

    it('should default scope to undefined (personal) when not specified', () => {
      configureQueries({ summary: { data: mockSummary }, history: { data: mockHistory } })

      renderHook(() => useNetWorthData({ startDate: '2025-11-01', endDate: '2025-11-15' }), {
        wrapper: createWrapper(),
      })

      expect(mockUseGetNetWorth).toHaveBeenCalledWith(
        expect.objectContaining({ scope: undefined }),
        expect.any(Object)
      )
    })
  })

  describe('Error Handling', () => {
    it('should surface summary query errors', () => {
      const error = new Error('Failed to fetch net worth summary')
      configureQueries({ summary: { error }, history: { data: mockHistory } })

      const { result } = renderHook(() => useNetWorthData(), { wrapper: createWrapper() })

      expect(result.current.error).toEqual(error)
    })

    it('should surface history query errors', () => {
      const error = new Error('Failed to fetch net worth history')
      configureQueries({ summary: { data: mockSummary }, history: { error } })

      const { result } = renderHook(() => useNetWorthData(), { wrapper: createWrapper() })

      expect(result.current.error).toEqual(error)
    })
  })

  describe('Gap-Filling Logic', () => {
    it('should forward-fill missing days in net worth history', async () => {
      configureQueries({ summary: { data: mockSummary }, history: { data: mockHistory } })

      const { result } = renderHook(() => useNetWorthData(), { wrapper: createWrapper() })

      await waitFor(() => expect(result.current.loading).toBe(false))

      const dates = (result.current.history?.snapshots ?? []).map((s) => s.date)
      expect(dates).toEqual([
        '2025-11-10',
        '2025-11-11',
        '2025-11-12',
        '2025-11-13',
        '2025-11-14',
        '2025-11-15',
      ])
    })
  })

  describe('Refetch Function', () => {
    it('should call both refetch functions when refetch is invoked', async () => {
      configureQueries({ summary: { data: mockSummary }, history: { data: mockHistory } })

      const { result } = renderHook(() => useNetWorthData(), { wrapper: createWrapper() })
      await waitFor(() => expect(result.current.loading).toBe(false))

      await act(async () => {
        await result.current.refetch()
      })

      expect(mockRefetchNetWorth).toHaveBeenCalled()
      expect(mockRefetchNetWorthHistory).toHaveBeenCalled()
    })
  })
})
