/**
 * useDashboardData Hook
 * Issue #353: Balance Tracking Dashboard
 * Issue #564: Migrated to generated React Query hooks
 *
 * Custom hook for fetching and managing dashboard data.
 */

import { useMemo, useCallback } from 'react';
import { forwardFillByDay } from '@/shared/dates';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetBalanceSummary,
  useGetBalanceHistory,
  useGetAccountTypes,
  getGetBalanceSummaryQueryKey,
  getGetBalanceHistoryQueryKey,
  getGetAccountTypesQueryKey,
} from '@boostedbudget/api-client/endpoints/dashboard/dashboard';
import type {
  BalanceSummary,
  BalanceHistory,
  AccountTypesResponse,
  DashboardFilters,
} from '../types';

export interface UseDashboardDataResult {
  summary: BalanceSummary | null;
  history: BalanceHistory | null;
  accountTypes: AccountTypesResponse | null;
  loading: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

/**
 * Custom hook for fetching and managing dashboard data
 *
 * Features:
 * - Fetches balance summary, history, and account types
 * - Implements gap-filling for missing days in balance history
 * - Handles loading and error states
 * - Supports filters for account types and date ranges
 *
 * Gap-filling strategy:
 * - Forward-fill: Each day without a snapshot inherits the previous day's balance
 * - Creates a continuous data series for smooth chart rendering
 * - Preserves actual snapshot dates for reference
 *
 * @param filters - Optional filters for account types and date ranges
 * @returns Dashboard data with loading/error states and refetch function
 */
export function useDashboardData(filters?: DashboardFilters): UseDashboardDataResult {
  const queryClient = useQueryClient();

  // Build query params from filters
  const summaryParams = useMemo(() => {
    if (!filters) return undefined;
    return {
      account_types: filters.accountTypes?.join(','),
      as_of_date: filters.asOfDate,
      scope: filters.scope,
    };
  }, [filters]);

  const historyParams = useMemo(() => {
    if (!filters) return undefined;
    return {
      account_types: filters.accountTypes?.join(','),
      start_date: filters.startDate,
      end_date: filters.endDate,
      granularity: filters.granularity,
      scope: filters.scope,
    };
  }, [filters]);

  // Use the generated query hooks
  const summaryQuery = useGetBalanceSummary(summaryParams, {
    query: {
      staleTime: 30 * 1000, // 30 seconds
    },
  });

  const historyQuery = useGetBalanceHistory(historyParams, {
    query: {
      staleTime: 30 * 1000,
    },
  });

  const accountTypesQuery = useGetAccountTypes({
    query: {
      staleTime: 60 * 1000, // 1 minute
    },
  });

  // Combine loading states
  const loading = summaryQuery.isLoading || historyQuery.isLoading || accountTypesQuery.isLoading;

  // Get first error if any
  const error = useMemo<Error | null>(() => {
    const queryError = summaryQuery.error || historyQuery.error || accountTypesQuery.error;
    if (!queryError) return null;
    return queryError instanceof Error ? queryError : new Error('Failed to fetch dashboard data');
  }, [summaryQuery.error, historyQuery.error, accountTypesQuery.error]);

  // Convert query data to local types
  const summary = useMemo<BalanceSummary | null>(() => {
    return summaryQuery.data ? (summaryQuery.data as unknown as BalanceSummary) : null;
  }, [summaryQuery.data]);

  const accountTypes = useMemo<AccountTypesResponse | null>(() => {
    return accountTypesQuery.data ? (accountTypesQuery.data as unknown as AccountTypesResponse) : null;
  }, [accountTypesQuery.data]);

  // Apply gap-filling to balance history
  const history = useMemo<BalanceHistory | null>(() => {
    if (!historyQuery.data) return null;
    const historyData = historyQuery.data as unknown as BalanceHistory;
    if (historyData.snapshots && historyData.snapshots.length > 0) {
      return fillBalanceHistoryGaps(historyData);
    }
    return historyData;
  }, [historyQuery.data]);

  // Refetch function
  const refetch = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: getGetBalanceSummaryQueryKey(summaryParams),
      }),
      queryClient.invalidateQueries({
        queryKey: getGetBalanceHistoryQueryKey(historyParams),
      }),
      queryClient.invalidateQueries({
        queryKey: getGetAccountTypesQueryKey(),
      }),
    ]);
    await Promise.all([summaryQuery.refetch(), historyQuery.refetch(), accountTypesQuery.refetch()]);
  }, [queryClient, summaryParams, historyParams, summaryQuery, historyQuery, accountTypesQuery]);

  return {
    summary,
    history,
    accountTypes,
    loading,
    error,
    refetch,
  };
}

/**
 * Fill gaps in balance history with forward-filled values
 *
 * Strategy:
 * - For each day from start_date up to the fill horizon, ensure there's a snapshot
 * - If a day is missing, use the previous day's balance (forward-fill)
 * - This creates a continuous time series for smooth chart rendering
 *
 * The horizon is min(newest real snapshot, today) — NOT end_date, which can be in the future
 * and used to have the last real snapshot copied forward into days that had not happened yet
 * (#1626 Phase 3, Defect 1). See forwardFillByDay for the full reasoning, including why "the
 * array's last row" is not a sufficient cutoff on its own.
 *
 * @param history - Original balance history from API
 * @returns Balance history with filled gaps
 */
function fillBalanceHistoryGaps(history: BalanceHistory): BalanceHistory {
  const { start_date, end_date, snapshots } = history;

  return {
    ...history,
    snapshots: forwardFillByDay(snapshots, start_date, end_date, (previous, dateKey) => ({
      date: dateKey,
      balance: previous.balance,
      by_type: previous.by_type,
    })),
  };
}
