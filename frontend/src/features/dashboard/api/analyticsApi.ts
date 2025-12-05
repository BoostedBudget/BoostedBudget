/**
 * Analytics API
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 *
 * Provides API functions for cash flow analytics endpoints.
 */

import { apiClient } from '../../../api/axiosConfig';
import type { CashFlowSummary, CashFlowHistory, CashFlowFilters } from '../types';

/**
 * Get cash flow summary for a date range
 */
export async function getCashFlowSummary(
  filters: CashFlowFilters
): Promise<CashFlowSummary> {
  const params = new URLSearchParams({
    start_date: filters.startDate,
    end_date: filters.endDate,
  });

  if (filters.accountTypes?.length) {
    params.set('account_types', filters.accountTypes.join(','));
  }

  if (filters.includeTransfers !== undefined) {
    params.set('include_transfers', String(filters.includeTransfers));
  }

  const response = await apiClient.get<CashFlowSummary>(
    `/v1/analytics/cash-flow-summary?${params.toString()}`
  );
  return response.data;
}

/**
 * Get cash flow history (time series) for charts
 */
export async function getCashFlowHistory(
  filters: CashFlowFilters
): Promise<CashFlowHistory> {
  const params = new URLSearchParams({
    start_date: filters.startDate,
    end_date: filters.endDate,
  });

  if (filters.grouping) {
    params.set('grouping', filters.grouping);
  }

  if (filters.accountTypes?.length) {
    params.set('account_types', filters.accountTypes.join(','));
  }

  if (filters.includeTransfers !== undefined) {
    params.set('include_transfers', String(filters.includeTransfers));
  }

  const response = await apiClient.get<CashFlowHistory>(
    `/v1/analytics/cash-flow-history?${params.toString()}`
  );
  return response.data;
}
