/**
 * Portfolio Holdings Widget
 * Issue #1297: Compact holdings table with configurable topN and sort order.
 *
 * Builds an inline table from useGetHoldings — NOT the full HoldingsTable
 * component (too heavy for a small widget). Every monetary/name cell is
 * data-ph-mask'd because the table is hand-built and not DataTable-wrapped.
 */

import { useEffect, useMemo, useRef } from 'react'
import { BarChart2 } from 'lucide-react'
import { useGetHoldings } from '@boostedbudget/api-client/endpoints/investments/investments'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'
import { useDashboardContext } from '../context/DashboardContext'
import { registerWidget } from './registry'
import { WidgetEmptyState } from './WidgetEmptyState'
import { normalizeWidgetSettings } from './settings'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import type { WidgetProps, PortfolioHoldingsSettings } from '../types'

// Formatters for compact table display
const formatValue = (v: number | undefined) =>
  v == null
    ? '--'
    : new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 }).format(v)

const formatPct = (v: number | undefined) =>
  v == null
    ? '--'
    : `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`

interface CompactRowProps {
  holding: HoldingView
}

function CompactRow({ holding }: CompactRowProps) {
  const pct = holding.day_change_pct
  const isPositive = pct != null && pct > 0
  const isNegative = pct != null && pct < 0

  return (
    <tr data-testid="holding-row" className="border-b border-border last:border-0">
      <td className="py-1.5 pr-2 w-16">
        <span
          data-testid="holding-name"
          data-ph-mask
          className="text-xs font-mono font-medium text-foreground truncate block"
        >
          {holding.ticker_symbol ?? holding.security_name ?? '--'}
        </span>
      </td>
      <td className="py-1.5 pr-2 text-right">
        <span
          data-testid="holding-value"
          data-ph-mask
          className="text-xs text-foreground"
        >
          {formatValue(holding.institution_value)}
        </span>
      </td>
      <td className="py-1.5 text-right">
        <span
          data-testid="holding-day-change"
          data-ph-mask
          className={`text-xs font-medium ${
            isPositive ? 'text-success' : isNegative ? 'text-destructive' : 'text-muted-foreground'
          }`}
        >
          {formatPct(holding.day_change_pct)}
        </span>
      </td>
    </tr>
  )
}

function PortfolioHoldingsWidget(props: WidgetProps) {
  const { settings: rawSettings, onSettingsChange, isEditing } = props
  const { scope } = useDashboardContext()

  // #1706: the dashboard scope must reach the server-side read, not be filtered client-side —
  // a client filter would still pull another member's holdings into the browser. useMemo keeps
  // the params reference stable so the generated query key does not churn every render.
  const holdingsParams = useMemo(() => ({ scope }), [scope])

  const { data, isLoading, isError, error } = useGetHoldings(holdingsParams)
  const trackEvent = useTrackEvent()
  const viewedFired = useRef(false)

  const { topN, sortBy } = normalizeWidgetSettings(
    'portfolio-holdings',
    rawSettings as Record<string, unknown> | undefined
  ) as PortfolioHoldingsSettings

  useEffect(() => {
    if (!viewedFired.current && data != null) {
      viewedFired.current = true
      trackEvent(ANALYTICS_EVENTS.PORTFOLIO_HOLDINGS_WIDGET_VIEWED)
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
        {(error as Error)?.message ?? 'Failed to load holdings'}
      </div>
    )
  }

  if (!data || !data.holdings || data.holdings.length === 0) {
    return <WidgetEmptyState widgetType="portfolio-holdings" />
  }

  // Sort holdings by chosen dimension (descending)
  const sorted = [...data.holdings].sort((a, b) => {
    if (sortBy === 'day_change') {
      return (b.day_change_pct ?? 0) - (a.day_change_pct ?? 0)
    }
    return (b.institution_value ?? 0) - (a.institution_value ?? 0)
  })

  const displayed = sorted.slice(0, topN)

  return (
    <div className="h-full flex flex-col gap-2 p-1 overflow-auto">
      {/* Settings controls — edit mode only */}
      {isEditing && (
        <div className="flex gap-2 flex-wrap mb-1">
          <label className="flex items-center gap-1 text-xs text-muted-foreground">
            Show
            <select
              role="combobox"
              aria-label="Number of holdings"
              className="ml-1 text-xs border border-border rounded px-1 py-0.5 bg-background"
              value={topN}
              onChange={(e) => {
                const newTopN = parseInt(e.target.value, 10) as 5 | 10 | 20
                onSettingsChange?.({ topN: newTopN, sortBy })
              }}
            >
              <option value={5}>Top 5</option>
              <option value={10}>Top 10</option>
              <option value={20}>Top 20</option>
            </select>
          </label>
          <label className="flex items-center gap-1 text-xs text-muted-foreground">
            Sort by
            <select
              role="combobox"
              aria-label="Sort by"
              className="ml-1 text-xs border border-border rounded px-1 py-0.5 bg-background"
              value={sortBy}
              onChange={(e) => {
                const newSortBy = e.target.value as 'value' | 'day_change'
                onSettingsChange?.({ topN, sortBy: newSortBy })
              }}
            >
              <option value="value">Market Value</option>
              <option value="day_change">Day Change</option>
            </select>
          </label>
        </div>
      )}

      {/* Compact holdings table */}
      <div className="overflow-auto flex-1">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border">
              <th className="pb-1 text-left text-xs text-muted-foreground font-normal">Ticker</th>
              <th className="pb-1 text-right text-xs text-muted-foreground font-normal">Value</th>
              <th className="pb-1 text-right text-xs text-muted-foreground font-normal">Day %</th>
            </tr>
          </thead>
          <tbody>
            {displayed.map((holding) => (
              <CompactRow key={holding.id} holding={holding} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

registerWidget({
  type: 'portfolio-holdings',
  title: 'Portfolio Holdings',
  description: 'Top holdings by market value or day change with configurable depth.',
  icon: 'bar-chart',
  category: 'investments',
  defaultSize: { w: 6, h: 4 },
  minSize: { w: 4, h: 3 },
  Component: PortfolioHoldingsWidget,
  emptyState: {
    icon: <BarChart2 />,
    title: 'No holdings found',
    noAccountsDescription: 'Connect a brokerage account to see your holdings',
    noDataDescription: 'No holdings in your connected accounts',
  },
})

export { PortfolioHoldingsWidget }
