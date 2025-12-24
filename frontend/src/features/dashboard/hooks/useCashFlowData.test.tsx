/**
 * useCashFlowData Hook Tests
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 * Issue #564: Migrated to mock generated React Query hooks
 *
 * Tests for useCashFlowSummary and useCashFlowHistory hooks.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useCashFlowSummary, useCashFlowHistory } from './useCashFlowData';
import type { CashFlowSummary, CashFlowHistory, CashFlowFilters } from '../types';

// Use vi.hoisted for mock functions
const { mockSummaryRefetch, mockHistoryRefetch } = vi.hoisted(() => ({
  mockSummaryRefetch: vi.fn().mockResolvedValue({}),
  mockHistoryRefetch: vi.fn().mockResolvedValue({}),
}));

// Mock the generated hooks
vi.mock('@/api/generated/endpoints/analytics/analytics', () => ({
  useGetCashFlowSummary: vi.fn(),
  useGetCashFlowHistory: vi.fn(),
  getGetCashFlowSummaryQueryKey: vi.fn((params) => ['cash-flow-summary', params]),
  getGetCashFlowHistoryQueryKey: vi.fn((params) => ['cash-flow-history', params]),
}));

// Import the mocked hooks
import {
  useGetCashFlowSummary,
  useGetCashFlowHistory,
} from '@/api/generated/endpoints/analytics/analytics';

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

describe('useCashFlowData', () => {
  const mockFilters: CashFlowFilters = {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
  };

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
  };

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
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockSummaryRefetch.mockClear();
    mockHistoryRefetch.mockClear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Helper to configure summary query state
  function configureSummaryQuery(config: {
    data?: CashFlowSummary | null;
    isLoading?: boolean;
    error?: Error | null;
  }) {
    vi.mocked(useGetCashFlowSummary).mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockSummaryRefetch,
    } as unknown as ReturnType<typeof useGetCashFlowSummary>);
  }

  // Helper to configure history query state
  function configureHistoryQuery(config: {
    data?: CashFlowHistory | null;
    isLoading?: boolean;
    error?: Error | null;
  }) {
    vi.mocked(useGetCashFlowHistory).mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockHistoryRefetch,
    } as unknown as ReturnType<typeof useGetCashFlowHistory>);
  }

  describe('useCashFlowSummary', () => {
    describe('Initial State', () => {
      it('should return loading=true initially when enabled', () => {
        configureSummaryQuery({ isLoading: true });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(true);
      });

      it('should return data=null initially', () => {
        configureSummaryQuery({ isLoading: true });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.data).toBeNull();
      });

      it('should return error=null initially', () => {
        configureSummaryQuery({ isLoading: true });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.error).toBeNull();
      });
    });

    describe('Fetch on Mount', () => {
      it('should call the query hook with correct params', async () => {
        configureSummaryQuery({ data: mockSummary });

        renderHook(() => useCashFlowSummary({ filters: mockFilters }), {
          wrapper: createWrapper(),
        });

        expect(useGetCashFlowSummary).toHaveBeenCalledWith(
          expect.objectContaining({
            start_date: '2024-01-01',
            end_date: '2024-01-31',
          }),
          expect.any(Object)
        );
      });

      it('should set data from query response', async () => {
        configureSummaryQuery({ data: mockSummary });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        await waitFor(() => {
          expect(result.current.data).toEqual(mockSummary);
        });
      });

      it('should set loading=false after fetch', async () => {
        configureSummaryQuery({ data: mockSummary });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        await waitFor(() => {
          expect(result.current.loading).toBe(false);
        });
      });
    });

    describe('Error Handling', () => {
      it('should handle errors with Error message', async () => {
        configureSummaryQuery({ error: new Error('Network error') });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.error).toBe('Network error');
      });

      it('should set data=null on error', async () => {
        configureSummaryQuery({ error: new Error('Error') });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.data).toBeNull();
      });

      it('should set loading=false on error', async () => {
        configureSummaryQuery({ error: new Error('Error') });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(false);
      });
    });

    describe('Enabled Flag', () => {
      it('should not set loading when disabled', async () => {
        configureSummaryQuery({ isLoading: false });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters, enabled: false }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(false);
      });
    });

    describe('Missing Dates', () => {
      it('should not be loading when startDate is missing', () => {
        configureSummaryQuery({ isLoading: false });

        const incompleteFilters: CashFlowFilters = {
          startDate: '',
          endDate: '2024-01-31',
        };

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: incompleteFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(false);
      });

      it('should not be loading when endDate is missing', () => {
        configureSummaryQuery({ isLoading: false });

        const incompleteFilters: CashFlowFilters = {
          startDate: '2024-01-01',
          endDate: '',
        };

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: incompleteFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(false);
      });
    });

    describe('Refetch', () => {
      it('should call refetch when refetch is called', async () => {
        configureSummaryQuery({ data: mockSummary });

        const { result } = renderHook(
          () => useCashFlowSummary({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        await waitFor(() => {
          expect(result.current.loading).toBe(false);
        });

        await act(async () => {
          await result.current.refetch();
        });

        expect(mockSummaryRefetch).toHaveBeenCalled();
      });
    });
  });

  describe('useCashFlowHistory', () => {
    describe('Initial State', () => {
      it('should return loading=true initially when enabled', () => {
        configureHistoryQuery({ isLoading: true });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(true);
      });

      it('should return data=null initially', () => {
        configureHistoryQuery({ isLoading: true });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.data).toBeNull();
      });

      it('should return error=null initially', () => {
        configureHistoryQuery({ isLoading: true });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.error).toBeNull();
      });
    });

    describe('Fetch on Mount', () => {
      it('should call the query hook with correct params', async () => {
        configureHistoryQuery({ data: mockHistory });

        renderHook(() => useCashFlowHistory({ filters: mockFilters }), {
          wrapper: createWrapper(),
        });

        expect(useGetCashFlowHistory).toHaveBeenCalledWith(
          expect.objectContaining({
            start_date: '2024-01-01',
            end_date: '2024-01-31',
          }),
          expect.any(Object)
        );
      });

      it('should set data from query response', async () => {
        configureHistoryQuery({ data: mockHistory });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        await waitFor(() => {
          expect(result.current.data).toEqual(mockHistory);
        });
      });

      it('should set loading=false after fetch', async () => {
        configureHistoryQuery({ data: mockHistory });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        await waitFor(() => {
          expect(result.current.loading).toBe(false);
        });
      });
    });

    describe('Error Handling', () => {
      it('should handle errors with Error message', async () => {
        configureHistoryQuery({ error: new Error('Server error') });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.error).toBe('Server error');
      });

      it('should set data=null on error', async () => {
        configureHistoryQuery({ error: new Error('Error') });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.data).toBeNull();
      });

      it('should set loading=false on error', async () => {
        configureHistoryQuery({ error: new Error('Error') });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(false);
      });
    });

    describe('Enabled Flag', () => {
      it('should not set loading when disabled', async () => {
        configureHistoryQuery({ isLoading: false });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters, enabled: false }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(false);
      });
    });

    describe('Missing Dates', () => {
      it('should not be loading when startDate is missing', () => {
        configureHistoryQuery({ isLoading: false });

        const incompleteFilters: CashFlowFilters = {
          startDate: '',
          endDate: '2024-01-31',
        };

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: incompleteFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(false);
      });

      it('should not be loading when endDate is missing', () => {
        configureHistoryQuery({ isLoading: false });

        const incompleteFilters: CashFlowFilters = {
          startDate: '2024-01-01',
          endDate: '',
        };

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: incompleteFilters }),
          { wrapper: createWrapper() }
        );

        expect(result.current.loading).toBe(false);
      });
    });

    describe('Refetch', () => {
      it('should call refetch when refetch is called', async () => {
        configureHistoryQuery({ data: mockHistory });

        const { result } = renderHook(
          () => useCashFlowHistory({ filters: mockFilters }),
          { wrapper: createWrapper() }
        );

        await waitFor(() => {
          expect(result.current.loading).toBe(false);
        });

        await act(async () => {
          await result.current.refetch();
        });

        expect(mockHistoryRefetch).toHaveBeenCalled();
      });
    });
  });
});
