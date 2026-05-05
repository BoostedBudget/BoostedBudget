import { useState, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { TrendingUp } from 'lucide-react'
import { NetWorthDisplay } from '../components/NetWorthDisplay'
import { useDashboardContext } from '../context/DashboardContext'
import { useNetWorthData } from '../hooks/useNetWorthData'
import { isNetWorthEmpty } from '../utils/netWorth'
import { registerWidget } from './registry'
import { WidgetEmptyState } from './WidgetEmptyState'

function NetWorthWidget() {
  const { filters } = useDashboardContext()
  const [showSparkline, setShowSparkline] = useState(false)

  const dashboardFilters = useMemo(
    () => ({
      startDate: filters.startDate,
      endDate: filters.endDate,
      accountTypes: filters.accountTypes,
    }),
    [filters.startDate, filters.endDate, filters.accountTypes]
  )

  const { summary, history, loading, error } = useNetWorthData(dashboardFilters)

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-full text-destructive text-sm">
        {error.message}
      </div>
    )
  }

  if (!summary || isNetWorthEmpty(summary)) {
    return <WidgetEmptyState widgetType="net-worth" />
  }

  return (
    <div className="h-full flex flex-col">
      <div className="flex justify-end mb-2">
        <button
          onClick={() => setShowSparkline(!showSparkline)}
          className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1 px-2 py-1 rounded hover:bg-muted transition-colors"
          title={
            showSparkline
              ? 'Hide trend chart - shows only your current net worth value'
              : 'Show trend chart - displays how your net worth has changed over time'
          }
        >
          <TrendingUp className="h-3 w-3" />
          {showSparkline ? 'Hide Trend' : 'Show Trend'}
        </button>
      </div>

      <NetWorthDisplay
        summary={summary}
        history={showSparkline ? history ?? undefined : undefined}
        showSparkline={showSparkline}
        linkTo="/net-worth"
      />

      <div className="mt-auto pt-2 border-t border-border">
        <Link
          to="/net-worth"
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          View details →
        </Link>
      </div>
    </div>
  )
}

registerWidget({
  type: 'net-worth',
  title: 'Net Worth',
  description:
    'Track your total net worth with assets and liabilities breakdown. Toggle trend chart on/off.',
  icon: 'chart-line',
  category: 'overview',
  defaultSize: { w: 4, h: 4 },
  minSize: { w: 3, h: 3 },
  maxSize: { w: 6, h: 6 },
  Component: NetWorthWidget,
  emptyState: {
    icon: <TrendingUp />,
    title: 'No net worth data yet',
    noAccountsDescription: 'Connect a bank account to track your net worth',
    noDataDescription: 'No net worth data available',
  },
})

export { NetWorthWidget }
