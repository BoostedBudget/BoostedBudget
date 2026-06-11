import { format, parseISO } from 'date-fns'
import type { BalanceSummary } from '../types'

interface BalanceOverviewProps {
  summary: BalanceSummary
}

/**
 * BalanceOverview component displays the current balance summary
 *
 * Features:
 * - Total balance across all accounts
 * - Period comparison (change vs 30 days ago)
 * - Visual indicator for positive/negative changes
 * - As-of date display
 *
 * Issue #353: Balance Tracking Dashboard
 */
export function BalanceOverview({ summary }: BalanceOverviewProps) {
  const {
    total_balance,
    as_of_date,
    period_comparison,
  } = summary

  const formattedDate = format(parseISO(as_of_date), 'MMM d, yyyy')
  const formattedBalance = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(total_balance)

  const hasComparison = period_comparison !== undefined && period_comparison !== null
  const isPositiveChange = hasComparison && period_comparison.change_amount >= 0

  return (
    <div className="h-full flex flex-col">
      <div className="mb-2">
        <h2 className="text-sm font-medium text-muted-foreground">Total Balance</h2>
        <p className="text-xs text-muted-foreground">as of {formattedDate}</p>
      </div>

      <div className="mb-4 flex-1 flex items-center">
        {/* StatCard exception (#1285): text-4xl headline in an <h2>; StatCard forces text-2xl + <p> label
            (heading-semantics loss + 33% shrink). Would break BalanceOverview.test.tsx:430 (text-foreground
            assertion) if trend were used. Stays as-is. */}
        <p className="text-4xl font-bold text-foreground">{formattedBalance}</p>
      </div>

      {hasComparison && (
        <div className="flex items-center space-x-2">
          <span className={`text-sm font-semibold ${isPositiveChange ? 'text-success' : 'text-destructive'}`}>
            {isPositiveChange ? '↑' : '↓'}{' '}
            {new Intl.NumberFormat('en-US', {
              style: 'currency',
              currency: 'USD',
              signDisplay: 'never',
            }).format(Math.abs(period_comparison.change_amount))}
          </span>
          <span className="text-sm text-muted-foreground">
            ({period_comparison.change_percent.toFixed(2)}%)
          </span>
          <span className="text-sm text-muted-foreground">vs 30 days ago</span>
        </div>
      )}

      {!hasComparison && (
        <p className="text-sm text-muted-foreground">
          No historical data for comparison
        </p>
      )}
    </div>
  )
}
