import { differenceInHours, parseISO, format } from 'date-fns'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { NetWorthSparkline } from './NetWorthSparkline'
import { ChangeIndicator } from '@/features/net-worth/components/ChangeIndicator'
import type { NetWorthSummary, NetWorthHistory } from '../types'

interface NetWorthDisplayProps {
  summary: NetWorthSummary
  history?: NetWorthHistory
  showSparkline?: boolean
  linkTo?: string
}

function formatCurrency(value: number): string {
  return value.toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

export function NetWorthDisplay({ summary, history, showSparkline, linkTo }: NetWorthDisplayProps) {
  const isPositive = summary.net_worth >= 0
  const comparison = summary.period_comparison

  let changeVariant: 'positive' | 'negative' | 'neutral' = 'neutral'
  if (comparison) {
    if (comparison.net_worth_change > 0) changeVariant = 'positive'
    else if (comparison.net_worth_change < 0) changeVariant = 'negative'
  }

  const asOf = parseISO(summary.as_of_date)
  const isStale = differenceInHours(new Date(), asOf) > 24

  const headline = (
    <span
      data-ph-mask
      className={cn('text-3xl font-bold', isPositive ? 'text-success' : 'text-destructive')}
    >
      {formatCurrency(summary.net_worth)}
    </span>
  )

  return (
    <div className="flex flex-col h-full">
      {linkTo ? (
        <Link
          to={linkTo}
          aria-label={`View net worth details — ${formatCurrency(summary.net_worth)}`}
          className="hover:opacity-80 transition-opacity w-fit"
        >
          {headline}
        </Link>
      ) : (
        headline
      )}

      <p className="text-xs text-muted-foreground mt-1">
        as of {format(asOf, 'MMM d, yyyy')}{isStale && ' (may be stale)'}
      </p>

      {comparison && (
        <div className="mt-1">
          <ChangeIndicator
            variant={changeVariant}
            amount={comparison.net_worth_change}
            percent={comparison.net_worth_change_percent}
          />
        </div>
      )}

      {showSparkline && history && history.snapshots.length > 0 && (
        <div className="mt-3 h-16">
          <NetWorthSparkline data={history.snapshots} />
        </div>
      )}

      <div className="mt-auto pt-3 space-y-2 border-t border-border">
        <div className="flex justify-between text-sm">
          <span className="text-muted-foreground">Assets</span>
          <span data-ph-mask className="text-success font-medium">
            {formatCurrency(summary.total_assets)}
          </span>
        </div>
        <div className="flex justify-between text-sm">
          <span className="text-muted-foreground">Liabilities</span>
          <span data-ph-mask className="text-destructive font-medium">
            {formatCurrency(summary.total_liabilities)}
          </span>
        </div>
      </div>
    </div>
  )
}
