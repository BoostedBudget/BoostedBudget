import { useState, useEffect, useCallback } from 'react'
import { format, eachDayOfInterval, parseISO } from 'date-fns'
import {
  fetchBalanceSummary,
  fetchBalanceHistory,
  fetchAccountTypes,
} from '../api/dashboardApi'
import type {
  BalanceSummary,
  BalanceHistory,
  BalanceHistorySnapshot,
  AccountTypesResponse,
  DashboardFilters,
} from '../types'

export interface UseDashboardDataResult {
  summary: BalanceSummary | null
  history: BalanceHistory | null
  accountTypes: AccountTypesResponse | null
  loading: boolean
  error: Error | null
  refetch: () => Promise<void>
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
 * Issue #353: Balance Tracking Dashboard
 *
 * @param filters - Optional filters for account types and date ranges
 * @returns Dashboard data with loading/error states and refetch function
 */
export function useDashboardData(
  filters?: DashboardFilters
): UseDashboardDataResult {
  const [summary, setSummary] = useState<BalanceSummary | null>(null)
  const [history, setHistory] = useState<BalanceHistory | null>(null)
  const [accountTypes, setAccountTypes] =
    useState<AccountTypesResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)

    try {
      // Fetch all dashboard data in parallel
      const [summaryData, historyData, typesData] = await Promise.all([
        fetchBalanceSummary(filters),
        fetchBalanceHistory(filters),
        fetchAccountTypes(),
      ])

      setSummary(summaryData)
      setAccountTypes(typesData)

      // Apply gap-filling to balance history
      if (historyData && historyData.snapshots.length > 0) {
        const filledHistory = fillBalanceHistoryGaps(historyData)
        setHistory(filledHistory)
      } else {
        setHistory(historyData)
      }
    } catch (err) {
      setError(
        err instanceof Error ? err : new Error('Failed to fetch dashboard data')
      )
    } finally {
      setLoading(false)
    }
  }, [filters])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  return {
    summary,
    history,
    accountTypes,
    loading,
    error,
    refetch: fetchData,
  }
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
  const { start_date, end_date, snapshots } = history

  // Parse dates
  const startDate = parseISO(start_date)
  const endDate = parseISO(end_date)

  // Generate all days in range
  const allDays = eachDayOfInterval({ start: startDate, end: endDate })

  // Create a map of existing snapshots by date
  const snapshotMap = new Map<string, BalanceHistorySnapshot>()
  snapshots.forEach((snapshot) => {
    const dateKey = format(parseISO(snapshot.date), 'yyyy-MM-dd')
    snapshotMap.set(dateKey, snapshot)
  })

  // Fill gaps with forward-filled values
  const filledSnapshots: BalanceHistorySnapshot[] = []
  let lastSnapshot: BalanceHistorySnapshot | null = null

  allDays.forEach((day) => {
    const dateKey = format(day, 'yyyy-MM-dd')
    const existingSnapshot = snapshotMap.get(dateKey)

    if (existingSnapshot) {
      // Use actual snapshot
      filledSnapshots.push(existingSnapshot)
      lastSnapshot = existingSnapshot
    } else if (lastSnapshot) {
      // Forward-fill from previous snapshot
      filledSnapshots.push({
        date: dateKey,
        balance: lastSnapshot.balance,
        by_type: lastSnapshot.by_type,
      })
    }
    // If no previous snapshot exists, skip this day (shouldn't happen after first snapshot)
  })

  return {
    ...history,
    snapshots: filledSnapshots,
  }
}
