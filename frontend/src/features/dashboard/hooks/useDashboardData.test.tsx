/**
 * useDashboardData Hook Tests
 * Issue #353: Balance Tracking Dashboard
 * Issue #564: Migrated to mock generated React Query hooks
 *
 * Tests the dashboard data fetching hook with gap-filling logic,
 * parallel API calls, filtering, and error handling.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useDashboardData } from './useDashboardData';
import type { BalanceSummary, BalanceHistory, AccountTypesResponse, DashboardFilters } from '../types';

// Use vi.hoisted for mock functions
const { mockSummaryRefetch, mockHistoryRefetch, mockTypesRefetch } = vi.hoisted(() => ({
  mockSummaryRefetch: vi.fn().mockResolvedValue({}),
  mockHistoryRefetch: vi.fn().mockResolvedValue({}),
  mockTypesRefetch: vi.fn().mockResolvedValue({}),
}));

// Mock the generated hooks
vi.mock('@/api/generated/endpoints/dashboard/dashboard', () => ({
  useGetBalanceSummary: vi.fn(),
  useGetBalanceHistory: vi.fn(),
  useGetAccountTypes: vi.fn(),
  getGetBalanceSummaryQueryKey: vi.fn((params) => ['balance-summary', params]),
  getGetBalanceHistoryQueryKey: vi.fn((params) => ['balance-history', params]),
  getGetAccountTypesQueryKey: vi.fn(() => ['account-types']),
}));

// Import the mocked hooks
import {
  useGetBalanceSummary,
  useGetBalanceHistory,
  useGetAccountTypes,
} from '@/api/generated/endpoints/dashboard/dashboard';

// Helper to create wrapper with QueryClient
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
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
  };

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
  };

  const mockAccountTypes: AccountTypesResponse = {
    types: [
      { key: 'depository', label: 'Checking & Savings', count: 2, total_balance: 5000.0 },
      { key: 'credit', label: 'Credit Cards', count: 1, total_balance: -2000.0 },
    ],
  };

  const mockEmptyHistory: BalanceHistory = {
    start_date: '2025-11-10',
    end_date: '2025-11-15',
    granularity: 'daily',
    snapshots: [],
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockSummaryRefetch.mockClear();
    mockHistoryRefetch.mockClear();
    mockTypesRefetch.mockClear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Helper to configure all query states
  function configureQueries(config: {
    summary?: { data?: BalanceSummary | null; isLoading?: boolean; error?: Error | null };
    history?: { data?: BalanceHistory | null; isLoading?: boolean; error?: Error | null };
    types?: { data?: AccountTypesResponse | null; isLoading?: boolean; error?: Error | null };
  }) {
    vi.mocked(useGetBalanceSummary).mockReturnValue({
      data: config.summary?.data ?? undefined,
      isLoading: config.summary?.isLoading ?? false,
      error: config.summary?.error ?? null,
      refetch: mockSummaryRefetch,
    } as unknown as ReturnType<typeof useGetBalanceSummary>);

    vi.mocked(useGetBalanceHistory).mockReturnValue({
      data: config.history?.data ?? undefined,
      isLoading: config.history?.isLoading ?? false,
      error: config.history?.error ?? null,
      refetch: mockHistoryRefetch,
    } as unknown as ReturnType<typeof useGetBalanceHistory>);

    vi.mocked(useGetAccountTypes).mockReturnValue({
      data: config.types?.data ?? undefined,
      isLoading: config.types?.isLoading ?? false,
      error: config.types?.error ?? null,
      refetch: mockTypesRefetch,
    } as unknown as ReturnType<typeof useGetAccountTypes>);
  }

  describe('Basic Functionality', () => {
    it('should return loading state initially', () => {
      configureQueries({
        summary: { isLoading: true },
        history: { isLoading: true },
        types: { isLoading: true },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(true);
      expect(result.current.summary).toBeNull();
      expect(result.current.history).toBeNull();
      expect(result.current.accountTypes).toBeNull();
      expect(result.current.error).toBeNull();
    });

    it('should populate data from queries', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.summary).toEqual(mockBalanceSummary);
      expect(result.current.accountTypes).toEqual(mockAccountTypes);
      expect(result.current.error).toBeNull();
    });

    it('should call hooks with correct params', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      });

      const filters: DashboardFilters = {
        accountTypes: ['depository'],
        asOfDate: '2025-11-15',
        startDate: '2025-11-01',
        endDate: '2025-11-15',
      };

      renderHook(() => useDashboardData(filters), {
        wrapper: createWrapper(),
      });

      expect(useGetBalanceSummary).toHaveBeenCalledWith(
        expect.objectContaining({
          account_types: 'depository',
          as_of_date: '2025-11-15',
        }),
        expect.any(Object)
      );

      expect(useGetBalanceHistory).toHaveBeenCalledWith(
        expect.objectContaining({
          account_types: 'depository',
          start_date: '2025-11-01',
          end_date: '2025-11-15',
        }),
        expect.any(Object)
      );
    });
  });

  describe('Error Handling', () => {
    it('should handle summary query errors', async () => {
      const error = new Error('Failed to fetch balance summary');
      configureQueries({
        summary: { error },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      expect(result.current.error).toEqual(error);
    });

    it('should handle history query errors', async () => {
      const error = new Error('Failed to fetch balance history');
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { error },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      expect(result.current.error).toEqual(error);
    });

    it('should handle account types query errors', async () => {
      const error = new Error('Failed to fetch account types');
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { error },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      expect(result.current.error).toEqual(error);
    });
  });

  describe('Gap-Filling Logic', () => {
    it('should fill missing days in balance history', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Original has 3 snapshots (11/10, 11/12, 11/15)
      // Should fill gaps: 11/11, 11/13, 11/14
      expect(result.current.history?.snapshots).toHaveLength(6);

      const snapshots = result.current.history?.snapshots || [];
      const dates = snapshots.map((s) => s.date);

      expect(dates).toEqual([
        '2025-11-10',
        '2025-11-11', // Filled
        '2025-11-12',
        '2025-11-13', // Filled
        '2025-11-14', // Filled
        '2025-11-15',
      ]);
    });

    it('should use forward-fill strategy for missing days', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      const snapshots = result.current.history?.snapshots || [];

      // 11/11 should inherit from 11/10
      const nov11 = snapshots.find((s) => s.date === '2025-11-11');
      expect(nov11?.balance).toBe(14000.0);
      expect(nov11?.by_type).toEqual({ depository: 4500.0, credit: -1500.0 });

      // 11/13 should inherit from 11/12
      const nov13 = snapshots.find((s) => s.date === '2025-11-13');
      expect(nov13?.balance).toBe(14500.0);
    });

    it('should handle empty snapshots array', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockEmptyHistory },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.history).toEqual(mockEmptyHistory);
      expect(result.current.history?.snapshots).toHaveLength(0);
    });
  });

  describe('Refetch Function', () => {
    it('should provide refetch function', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.refetch).toBeInstanceOf(Function);
    });

    it('should call all refetch functions when refetch is called', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.refetch();
      });

      expect(mockSummaryRefetch).toHaveBeenCalled();
      expect(mockHistoryRefetch).toHaveBeenCalled();
      expect(mockTypesRefetch).toHaveBeenCalled();
    });
  });

  describe('Edge Cases', () => {
    it('should handle null history gracefully', async () => {
      configureQueries({
        summary: { data: mockBalanceSummary },
        history: { data: null },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.history).toBeNull();
      expect(result.current.summary).toEqual(mockBalanceSummary);
      expect(result.current.accountTypes).toEqual(mockAccountTypes);
    });

    it('should handle loading state from any query', () => {
      // Only summary is loading
      configureQueries({
        summary: { isLoading: true },
        history: { data: mockBalanceHistory },
        types: { data: mockAccountTypes },
      });

      const { result } = renderHook(() => useDashboardData(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(true);
    });
  });
});
