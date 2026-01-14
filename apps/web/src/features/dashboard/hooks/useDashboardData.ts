/**
 * useDashboardData Hook
 * Issue #353: Balance Tracking Dashboard
 * Issue #564: Migrated to generated React Query hooks
 *
 * Custom hook for fetching and managing dashboard data.
 */

import { useMemo, useCallback } from 'react';
import { format, eachDayOfInterval, parseISO } from 'date-fns';
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
  BalanceHistorySnapshot,
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
    };
  }, [filters]);

  const historyParams = useMemo(() => {
    if (!filters) return undefined;
    return {
      account_types: filters.accountTypes?.join(','),
      start_date: filters.startDate,
      end_date: filters.endDate,
      granularity: filters.granularity,
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
 * - For each day in the range [start_date, end_date], ensure there's a snapshot
 * - If a day is missing, use the previous day's balance (forward-fill)
 * - This creates a continuous time series for smooth chart rendering
 *
 * @param history - Original balance history from API
 * @returns Balance history with filled gaps
 */
function fillBalanceHistoryGaps(history: BalanceHistory): BalanceHistory {
  const { start_date, end_date, snapshots } = history;

  // Parse dates
  const startDate = parseISO(start_date);
  const endDate = parseISO(end_date);

  // Generate all days in range
  const allDays = eachDayOfInterval({ start: startDate, end: endDate });

  // Create a map of existing snapshots by date
  const snapshotMap = new Map<string, BalanceHistorySnapshot>();
  snapshots.forEach((snapshot) => {
    const dateKey = format(parseISO(snapshot.date), 'yyyy-MM-dd');
    snapshotMap.set(dateKey, snapshot);
  });

  // Fill gaps with forward-filled values
  const filledSnapshots: BalanceHistorySnapshot[] = [];
  let lastSnapshot: BalanceHistorySnapshot | null = null;

  allDays.forEach((day) => {
    const dateKey = format(day, 'yyyy-MM-dd');
    const existingSnapshot = snapshotMap.get(dateKey);

    if (existingSnapshot) {
      // Use actual snapshot
      filledSnapshots.push(existingSnapshot);
      lastSnapshot = existingSnapshot;
    } else if (lastSnapshot) {
      // Forward-fill from previous snapshot
      filledSnapshots.push({
        date: dateKey,
        balance: lastSnapshot.balance,
        by_type: lastSnapshot.by_type,
      });
    }
    // If no previous snapshot exists, skip this day (shouldn't happen after first snapshot)
  });

  return {
    ...history,
    snapshots: filledSnapshots,
  };
}
