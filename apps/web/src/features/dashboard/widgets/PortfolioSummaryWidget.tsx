/**
 * Portfolio Summary Widget
 * Issue #1297: Investment widgets for the customizable dashboard.
 *
 * Displays total portfolio value, gain/loss, and day change using the
 * reusable PortfolioSummaryCard. Links to /insights?tab=portfolio.
 */

import { useEffect, useMemo, useRef } from 'react'
import { Link } from 'react-router'
import { TrendingUp } from 'lucide-react'
import { useGetPortfolioSummary } from '@boostedbudget/api-client/endpoints/investments/investments'
import { PortfolioSummaryCard } from '@/features/investments/components/PortfolioSummaryCard'
import { useDashboardContext } from '../context/DashboardContext'
import { registerWidget } from './registry'
import { WidgetEmptyState } from './WidgetEmptyState'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

function PortfolioSummaryWidget() {
  const { scope } = useDashboardContext()

  // #1706: scope must reach the server-side read (a client filter would still pull another
  // member's holdings into the browser). useMemo keeps the params reference stable so the
  // generated query key does not churn every render.
  const summaryParams = useMemo(() => ({ scope }), [scope])

  const { data, isLoading, isError, error } = useGetPortfolioSummary(summaryParams)
  const trackEvent = useTrackEvent()
  const viewedFired = useRef(false)

  useEffect(() => {
    if (!viewedFired.current && data != null) {
      viewedFired.current = true
      trackEvent(ANALYTICS_EVENTS.PORTFOLIO_SUMMARY_WIDGET_VIEWED)
    }
  }, [data, trackEvent])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex items-center justify-center h-full text-destructive text-sm">
        {(error as Error)?.message ?? 'Failed to load portfolio'}
      </div>
    )
  }

  if (!data || data.holdings_count === 0) {
    return <WidgetEmptyState widgetType="portfolio-summary" />
  }

  return (
    <div className="h-full flex flex-col gap-4 p-1 overflow-auto">
      <PortfolioSummaryCard summary={data} />
      <div className="mt-auto pt-2 border-t border-border">
        <Link
          to="/insights?tab=portfolio"
          className="text-sm text-primary hover:underline"
        >
          View portfolio →
        </Link>
      </div>
    </div>
  )
}

registerWidget({
  type: 'portfolio-summary',
  title: 'Portfolio Summary',
  description: 'Overview of your total investment portfolio value, gains/losses, and day change.',
  icon: 'trending-up',
  category: 'investments',
  defaultSize: { w: 6, h: 3 },
  minSize: { w: 4, h: 2 },
  Component: PortfolioSummaryWidget,
  emptyState: {
    icon: <TrendingUp />,
    title: 'No investment accounts',
    noAccountsDescription: 'Connect a brokerage account to see your portfolio summary',
    noDataDescription: 'No holdings found in your connected accounts',
  },
})

export { PortfolioSummaryWidget }
