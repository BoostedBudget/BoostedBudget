// Issue #972: Savings Transfer Automation — dashboard widget
import { useEffect, useRef } from 'react'
import { PiggyBank, TrendingUp, TrendingDown } from 'lucide-react'
import type { WidgetProps } from '../types'
import { useSavingsImpactData } from '../hooks/useSavingsImpactData'
import { registerWidget } from './registry'
import { WidgetEmptyState } from './WidgetEmptyState'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

// Whole-dollar formatter for aggregate headline stats (intentionally scannable).
const formatCurrency = (amount: number) =>
  new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 0,
  }).format(amount)

// Exact-cents formatter for per-item actual amounts (GAP CB: 250.75 not $251).
const formatExactCurrency = (amount: number) =>
  new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount)

const formatDate = (isoString: string) =>
  new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }).format(new Date(isoString))

function SavingsImpactWidget(_props: WidgetProps) {
  const { data, loading, error } = useSavingsImpactData()
  const trackEvent = useTrackEvent()
  const viewedFired = useRef(false)

  // Fire once when data first becomes available; ignore subsequent refetch references.
  useEffect(() => {
    if (!viewedFired.current && data !== null && data !== undefined) {
      viewedFired.current = true
      trackEvent(ANALYTICS_EVENTS.SAVINGS_IMPACT_VIEWED)
    }
  }, [data, trackEvent])

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

  if (!data) {
    return <WidgetEmptyState widgetType="savings-impact" />
  }

  const momChange = data.month_amount - (data.prev_month_amount ?? 0)
  const momPositive = momChange >= 0

  return (
    <div className="h-full flex flex-col gap-4 p-1">
      {/* Total saved */}
      {/* StatCard exception (#1285): one of three stacked sections in a composite widget; converting only
          the top block fragments the widget. Stays as-is. */}
      <div>
        <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">
          Total Moved to Savings
        </p>
        <p
          className="text-2xl font-bold text-success"
          data-ph-mask
        >
          {formatCurrency(data.total_amount)}
        </p>
        <p className="text-xs text-muted-foreground mt-0.5">
          across {data.completed_count} transfer{data.completed_count !== 1 ? 's' : ''}
        </p>
      </div>

      {/* This month */}
      <div className="border-t border-border pt-3">
        <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">
          This Month
        </p>
        <p
          className="text-lg font-semibold"
          data-ph-mask
        >
          {formatCurrency(data.month_amount)}
        </p>

        {/* Month-over-month change */}
        {data.prev_month_amount !== undefined && (
          <div className={`flex items-center gap-1 mt-1 text-xs font-medium ${momPositive ? 'text-success' : 'text-destructive'}`}>
            {momPositive ? (
              <TrendingUp className="h-3 w-3" aria-hidden="true" />
            ) : (
              <TrendingDown className="h-3 w-3" aria-hidden="true" />
            )}
            <span data-ph-mask>
              {momPositive ? '+' : ''}{formatCurrency(momChange)} vs last month
            </span>
          </div>
        )}
      </div>

      {/* Recent completed suggestions — AC#1 */}
      <div className="border-t border-border pt-3">
        <p className="text-xs text-muted-foreground uppercase tracking-wide mb-2">
          Recent Transfers
        </p>
        {data.recent_items && data.recent_items.length > 0 ? (
          <ul className="space-y-2">
            {data.recent_items.map((item) => (
              <li
                key={item.id}
                data-testid="savings-recent-item"
                className="flex items-center justify-between text-xs"
              >
                <div className="flex flex-col min-w-0">
                  <span
                    data-testid="savings-recent-destination"
                    data-ph-mask
                    className="text-foreground truncate"
                  >
                    {item.to_account_name ?? 'Transfer destination'}
                  </span>
                  {item.completed_at && (
                    <span
                      data-testid="savings-recent-date"
                      className="text-muted-foreground"
                    >
                      {formatDate(item.completed_at)}
                    </span>
                  )}
                </div>
                <span
                  data-testid="savings-recent-amount"
                  data-ph-mask
                  className="font-medium text-success ml-2 shrink-0"
                >
                  {formatExactCurrency(item.actual_amount)}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p
            data-testid="savings-recent-empty"
            className="text-xs text-muted-foreground"
          >
            No completed transfers yet
          </p>
        )}
      </div>
    </div>
  )
}

registerWidget({
  type: 'savings-impact',
  title: 'Savings Impact',
  description: "Track how much you've moved to savings through automated transfer suggestions.",
  icon: 'piggy-bank',
  category: 'overview',
  defaultSize: { w: 4, h: 3 },
  minSize: { w: 3, h: 3 },
  Component: SavingsImpactWidget,
  emptyState: {
    icon: <PiggyBank />,
    title: 'No savings transfers yet',
    noAccountsDescription: 'Connect a bank account and set up savings rules',
    noDataDescription: 'Set up a savings transfer rule to get started',
  },
})

export { SavingsImpactWidget }
