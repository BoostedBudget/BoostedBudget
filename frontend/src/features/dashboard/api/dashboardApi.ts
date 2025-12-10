import { apiClient } from '../../../api/axiosConfig'
import type {
  BalanceSummary,
  BalanceHistory,
  AccountTypesResponse,
  DashboardFilters,
  DashboardLayout,
} from '../types'

/**
 * Fetch balance summary with current totals and period comparison
 * GET /api/v1/dashboard/balance-summary
 *
 * Returns:
 * - Total balance across all accounts
 * - Breakdown by account type (depository, credit, investment, loan)
 * - Individual account balances
 * - Period comparison (vs 30 days ago)
 *
 * Issue #353: Balance Tracking Dashboard
 *
 * @param filters - Optional filters for account types and as-of date
 * @returns Balance summary data
 * @throws Error if request fails (401 unauthorized, 500 server error)
 */
export async function fetchBalanceSummary(
  filters?: DashboardFilters
): Promise<BalanceSummary> {
  const params = new URLSearchParams()

  if (filters?.accountTypes && filters.accountTypes.length > 0) {
    params.append('account_types', filters.accountTypes.join(','))
  }
  if (filters?.asOfDate) {
    params.append('as_of_date', filters.asOfDate)
  }

  const queryString = params.toString()
  const url = queryString
    ? `/v1/dashboard/balance-summary?${queryString}`
    : '/v1/dashboard/balance-summary'

  const response = await apiClient.get<BalanceSummary>(url)
  return response.data
}

/**
 * Fetch balance history for charting
 * GET /api/v1/dashboard/balance-history
 *
 * Returns time series data showing balance changes over time:
 * - Daily snapshots with total balance
 * - Optional breakdown by account type
 * - Configurable date range and granularity
 *
 * Issue #353: Balance Tracking Dashboard
 *
 * @param filters - Optional filters for date range, account types, and granularity
 * @returns Balance history data with snapshots
 * @throws Error if request fails (400 bad request, 401 unauthorized, 500 server error)
 */
export async function fetchBalanceHistory(
  filters?: DashboardFilters
): Promise<BalanceHistory> {
  const params = new URLSearchParams()

  if (filters?.startDate) {
    params.append('start_date', filters.startDate)
  }
  if (filters?.endDate) {
    params.append('end_date', filters.endDate)
  }
  if (filters?.accountTypes && filters.accountTypes.length > 0) {
    params.append('account_types', filters.accountTypes.join(','))
  }
  if (filters?.granularity) {
    params.append('granularity', filters.granularity)
  }

  const queryString = params.toString()
  const url = queryString
    ? `/v1/dashboard/balance-history?${queryString}`
    : '/v1/dashboard/balance-history'

  const response = await apiClient.get<BalanceHistory>(url)
  return response.data
}

/**
 * Fetch available account types for the user
 * GET /api/v1/dashboard/account-types
 *
 * Returns all account types the user has with:
 * - Type key and user-friendly label
 * - Number of accounts of this type
 * - Total balance for this type
 *
 * Used for populating account type filter dropdowns.
 *
 * Issue #353: Balance Tracking Dashboard
 *
 * @returns Account types with counts and totals
 * @throws Error if request fails (401 unauthorized, 500 server error)
 */
export async function fetchAccountTypes(): Promise<AccountTypesResponse> {
  const response = await apiClient.get<AccountTypesResponse>(
    '/v1/dashboard/account-types'
  )
  return response.data
}

/**
 * Fetch default dashboard layout configuration
 * GET /api/v1/dashboard/default-layout
 *
 * Returns the centralized default dashboard layout from YAML configuration:
 * - Widget configurations (positions, sizes, types)
 * - Layout version (for auto-reset detection)
 *
 * This is a PUBLIC endpoint (no authentication required) - used for:
 * - Initial dashboard setup for new users
 * - Reset to defaults functionality
 * - Version comparison to trigger auto-resets
 *
 * Issue #449: Centralize default dashboard layout configuration
 *
 * @returns Default dashboard layout from server config
 * @throws Error if request fails (500 server error if config is missing)
 */
export async function fetchDefaultDashboardLayout(): Promise<DashboardLayout> {
  const response = await apiClient.get<DashboardLayout>(
    '/v1/dashboard/default-layout'
  )
  return response.data
}
