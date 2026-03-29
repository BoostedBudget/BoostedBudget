/**
 * Custom hook for fetching net worth summary and history data.
 */

import { useMemo, useCallback } from 'react';
import { format, eachDayOfInterval, parseISO } from 'date-fns';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetNetWorth,
  useGetNetWorthHistory,
  getGetNetWorthQueryKey,
  getGetNetWorthHistoryQueryKey,
} from '@boostedbudget/api-client/endpoints/dashboard/dashboard';
import type {
  NetWorthSummary,
  NetWorthHistory,
  NetWorthHistorySnapshot,
  DashboardFilters,
} from '../types';

export interface UseNetWorthDataResult {
  summary: NetWorthSummary | null;
  history: NetWorthHistory | null;
  loading: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

/**
 * Custom hook for fetching and managing net worth data
 *
 * Features:
 * - Fetches net worth summary and history
 * - Implements gap-filling for missing days in history
 * - Handles loading and error states
 * - Supports filters for account types and date ranges
 *
 * Gap-filling strategy:
 * - Forward-fill: Each day without a snapshot inherits the previous day's values
 * - Creates a continuous data series for smooth chart rendering
 *
 * @param filters - Optional filters for account types and date ranges
 * @returns Net worth data with loading/error states and refetch function
 */
export function useNetWorthData(filters?: DashboardFilters): UseNetWorthDataResult {
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
  const summaryQuery = useGetNetWorth(summaryParams, {
    query: {
      staleTime: 30 * 1000, // 30 seconds
    },
  });

  const historyQuery = useGetNetWorthHistory(historyParams, {
    query: {
      staleTime: 30 * 1000,
    },
  });

  // Combine loading states
  const loading = summaryQuery.isLoading || historyQuery.isLoading;

  // Get first error if any
  const error = useMemo<Error | null>(() => {
    const queryError = summaryQuery.error || historyQuery.error;
    if (!queryError) return null;
    return queryError instanceof Error ? queryError : new Error('Failed to fetch net worth data');
  }, [summaryQuery.error, historyQuery.error]);

  // Convert query data to local types
  const summary = useMemo<NetWorthSummary | null>(() => {
    return summaryQuery.data ? (summaryQuery.data as unknown as NetWorthSummary) : null;
  }, [summaryQuery.data]);

  // Apply gap-filling to net worth history
  const history = useMemo<NetWorthHistory | null>(() => {
    if (!historyQuery.data) return null;
    const historyData = historyQuery.data as unknown as NetWorthHistory;
    if (historyData.snapshots && historyData.snapshots.length > 0) {
      return fillNetWorthHistoryGaps(historyData);
    }
    return historyData;
  }, [historyQuery.data]);

  // Refetch function
  const refetch = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: getGetNetWorthQueryKey(summaryParams),
      }),
      queryClient.invalidateQueries({
        queryKey: getGetNetWorthHistoryQueryKey(historyParams),
      }),
    ]);
    await Promise.all([summaryQuery.refetch(), historyQuery.refetch()]);
  }, [queryClient, summaryParams, historyParams, summaryQuery, historyQuery]);

  return {
    summary,
    history,
    loading,
    error,
    refetch,
  };
}

/**
 * Fill gaps in net worth history with forward-filled values
 *
 * Strategy:
 * - For each day in the range [start_date, end_date], ensure there's a snapshot
 * - If a day is missing, use the previous day's values (forward-fill)
 * - This creates a continuous time series for smooth chart rendering
 *
 * @param history - Original net worth history from API
 * @returns Net worth history with filled gaps
 */
function fillNetWorthHistoryGaps(history: NetWorthHistory): NetWorthHistory {
  const { start_date, end_date, snapshots } = history;

  // Parse dates
  const startDate = parseISO(start_date);
  const endDate = parseISO(end_date);

  // Generate all days in range
  const allDays = eachDayOfInterval({ start: startDate, end: endDate });

  // Create a map of existing snapshots by date
  const snapshotMap = new Map<string, NetWorthHistorySnapshot>();
  snapshots.forEach((snapshot) => {
    const dateKey = format(parseISO(snapshot.date), 'yyyy-MM-dd');
    snapshotMap.set(dateKey, snapshot);
  });

  // Fill gaps with forward-filled values
  const filledSnapshots: NetWorthHistorySnapshot[] = [];
  let lastSnapshot: NetWorthHistorySnapshot | null = null;

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
        net_worth: lastSnapshot.net_worth,
        total_assets: lastSnapshot.total_assets,
        total_liabilities: lastSnapshot.total_liabilities,
      });
    }
    // If no previous snapshot exists, skip this day
  });

  return {
    ...history,
    snapshots: filledSnapshots,
  };
}
