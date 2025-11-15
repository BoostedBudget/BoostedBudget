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
  const changeColor = isPositiveChange ? 'text-green-600' : 'text-red-600'
  const changeIcon = isPositiveChange ? '↑' : '↓'

  return (
    <div className="bg-white rounded-lg shadow p-6">
      <div className="mb-2">
        <h2 className="text-sm font-medium text-gray-600">Total Balance</h2>
        <p className="text-xs text-gray-500">as of {formattedDate}</p>
      </div>

      <div className="mb-4">
        <p className="text-4xl font-bold text-gray-900">{formattedBalance}</p>
      </div>

      {hasComparison && (
        <div className="flex items-center space-x-2">
          <span className={`text-sm font-semibold ${changeColor}`}>
            {changeIcon}{' '}
            {new Intl.NumberFormat('en-US', {
              style: 'currency',
              currency: 'USD',
              signDisplay: 'never',
            }).format(Math.abs(period_comparison.change_amount))}
          </span>
          <span className="text-sm text-gray-600">
            ({period_comparison.change_percent.toFixed(2)}%)
          </span>
          <span className="text-sm text-gray-500">vs 30 days ago</span>
        </div>
      )}

      {!hasComparison && (
        <p className="text-sm text-gray-500">
          No historical data for comparison
        </p>
      )}
    </div>
  )
}
