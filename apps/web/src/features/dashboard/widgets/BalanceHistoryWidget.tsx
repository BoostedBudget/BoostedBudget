/**
 * Balance History Widget
 * Issue #418: Fresh implementation from scratch
 * Issue #525: Auto-scale axes on filter timeframe changes
 *
 * Wraps the BalanceHistoryChart component for use in the widget system.
 * Handles data fetching, loading/error/empty states, and breakdown toggle.
 */

import { useMemo, useState } from 'react'
import { TrendingUp } from 'lucide-react'
import { BalanceHistoryChart } from '../components/BalanceHistoryChart'
import { useDashboardContext } from '../context/DashboardContext'
import { useDashboardData } from '../hooks/useDashboardData'
import { registerWidget } from './registry'
import { WidgetEmptyState } from './WidgetEmptyState'
import type { WidgetProps } from '../types'

function BalanceHistoryWidget(_props: WidgetProps) {
  const { filters } = useDashboardContext()
  const [showTypeBreakdown, setShowTypeBreakdown] = useState(false)

  const dashboardFilters = useMemo(
    () => ({
      startDate: filters.startDate,
      endDate: filters.endDate,
      accountTypes: filters.accountTypes,
    }),
    [filters.startDate, filters.endDate, filters.accountTypes]
  )

  const filterKey = useMemo(
    () => JSON.stringify(dashboardFilters),
    [dashboardFilters]
  )

  const { history, loading, error } = useDashboardData(dashboardFilters)

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-full text-red-500 text-sm">
        {error.message}
      </div>
    )
  }

  if (!history || history.snapshots.length === 0) {
    return <WidgetEmptyState widgetType="balance-history" />
  }

  return (
    <div className="h-full relative">
      <BalanceHistoryChart
        history={history}
        showTypeBreakdown={showTypeBreakdown}
        filterKey={filterKey}
      />
      <div className="absolute top-0 right-0">
        <button
          onClick={() => setShowTypeBreakdown(!showTypeBreakdown)}
          className="px-2 py-1 text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 rounded hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
        >
          {showTypeBreakdown ? 'Total' : 'Breakdown'}
        </button>
      </div>
    </div>
  )
}

registerWidget({
  type: 'balance-history',
  title: 'Balance History',
  description: 'Historical balance trend chart with account type breakdown',
  icon: 'chart-line',
  category: 'charts',
  defaultSize: { w: 6, h: 4 },
  minSize: { w: 4, h: 4 },
  maxSize: { w: 12, h: 8 },
  Component: BalanceHistoryWidget,
  emptyState: {
    icon: <TrendingUp />,
    title: 'No balance history yet',
    noAccountsDescription: 'Connect a bank account to track your balances over time',
    noDataDescription: 'No balance data for this time period',
  },
})

export { BalanceHistoryWidget }
