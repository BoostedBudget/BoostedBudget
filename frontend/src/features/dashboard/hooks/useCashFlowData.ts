/**
 * Cash Flow Data Hook
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 *
 * Provides cash flow data fetching with loading and error states.
 */

import { useState, useEffect, useCallback } from 'react';
import { getCashFlowSummary, getCashFlowHistory } from '../api/analyticsApi';
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
  const [data, setData] = useState<CashFlowSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    if (!enabled || !filters.startDate || !filters.endDate) {
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const result = await getCashFlowSummary(filters);
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch cash flow summary');
      setData(null);
    } finally {
      setLoading(false);
    }
  }, [filters, enabled]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  return { data, loading, error, refetch: fetchData };
}

/**
 * Hook for fetching cash flow history data
 */
export function useCashFlowHistory(
  options: UseCashFlowDataOptions
): UseCashFlowHistoryResult {
  const { filters, enabled = true } = options;
  const [data, setData] = useState<CashFlowHistory | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    if (!enabled || !filters.startDate || !filters.endDate) {
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const result = await getCashFlowHistory(filters);
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch cash flow history');
      setData(null);
    } finally {
      setLoading(false);
    }
  }, [filters, enabled]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  return { data, loading, error, refetch: fetchData };
}
