/**
 * Cash Flow Data Hook
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 * Issue #564: Migrated to generated React Query hooks
 *
 * Provides cash flow data fetching with loading and error states.
 */

import { useMemo, useCallback } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetCashFlowSummary as useGetCashFlowSummaryQuery,
  useGetCashFlowHistory as useGetCashFlowHistoryQuery,
  getGetCashFlowSummaryQueryKey,
  getGetCashFlowHistoryQueryKey,
} from '@boostedbudget/api-client/endpoints/analytics/analytics';
import type { CashFlowSummary, CashFlowHistory, CashFlowFilters } from '../types';

interface UseCashFlowDataOptions {
  filters: CashFlowFilters;
  enabled?: boolean;
}

interface UseCashFlowSummaryResult {
  data: CashFlowSummary | null;
  loading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
}

interface UseCashFlowHistoryResult {
  data: CashFlowHistory | null;
  loading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
}

/**
 * Hook for fetching cash flow summary data
 */
export function useCashFlowSummary(
  options: UseCashFlowDataOptions
): UseCashFlowSummaryResult {
  const { filters, enabled = true } = options;
  const queryClient = useQueryClient();

  // Convert filters to API params
  const params = useMemo(() => {
    if (!filters.startDate || !filters.endDate) {
      return undefined;
    }
    return {
      start_date: filters.startDate,
      end_date: filters.endDate,
      account_types: filters.accountTypes?.join(','),
    };
  }, [filters]);

  const shouldFetch = enabled && !!params;

  const {
    data: queryData,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetCashFlowSummaryQuery(params!, {
    query: {
      enabled: shouldFetch,
      staleTime: 30 * 1000, // 30 seconds
    },
  });

  // Convert query data to local type
  const data = useMemo<CashFlowSummary | null>(() => {
    if (!queryData) return null;
    return queryData as unknown as CashFlowSummary;
  }, [queryData]);

  // Convert error to string
  const error = useMemo<string | null>(() => {
    if (!queryError) return null;
    return queryError instanceof Error
      ? queryError.message
      : 'Failed to fetch cash flow summary';
  }, [queryError]);

  // Refetch function
  const refetch = useCallback(async () => {
    if (params) {
      await queryClient.invalidateQueries({
        queryKey: getGetCashFlowSummaryQueryKey(params),
      });
      await queryRefetch();
    }
  }, [queryClient, queryRefetch, params]);

  return {
    data,
    loading: shouldFetch ? isLoading : false,
    error,
    refetch,
  };
}

/**
 * Hook for fetching cash flow history data
 */
export function useCashFlowHistory(
  options: UseCashFlowDataOptions
): UseCashFlowHistoryResult {
  const { filters, enabled = true } = options;
  const queryClient = useQueryClient();

  // Convert filters to API params
  const params = useMemo(() => {
    if (!filters.startDate || !filters.endDate) {
      return undefined;
    }
    return {
      start_date: filters.startDate,
      end_date: filters.endDate,
      account_types: filters.accountTypes?.join(','),
      grouping: filters.grouping,
    };
  }, [filters]);

  const shouldFetch = enabled && !!params;

  const {
    data: queryData,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetCashFlowHistoryQuery(params!, {
    query: {
      enabled: shouldFetch,
      staleTime: 30 * 1000, // 30 seconds
    },
  });

  // Convert query data to local type
  const data = useMemo<CashFlowHistory | null>(() => {
    if (!queryData) return null;
    return queryData as unknown as CashFlowHistory;
  }, [queryData]);

  // Convert error to string
  const error = useMemo<string | null>(() => {
    if (!queryError) return null;
    return queryError instanceof Error
      ? queryError.message
      : 'Failed to fetch cash flow history';
  }, [queryError]);

  // Refetch function
  const refetch = useCallback(async () => {
    if (params) {
      await queryClient.invalidateQueries({
        queryKey: getGetCashFlowHistoryQueryKey(params),
      });
      await queryRefetch();
    }
  }, [queryClient, queryRefetch, params]);

  return {
    data,
    loading: shouldFetch ? isLoading : false,
    error,
    refetch,
  };
}
