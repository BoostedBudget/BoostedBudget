/**
 * Displays net worth value, period comparison, and assets/liabilities breakdown.
 * Optionally shows a sparkline trend chart.
 */

import { NetWorthSparkline } from './NetWorthSparkline';
import type { NetWorthSummary, NetWorthHistory } from '../types';

interface NetWorthDisplayProps {
  summary: NetWorthSummary;
  history?: NetWorthHistory;
  showSparkline?: boolean;
}

function formatCurrency(value: number): string {
  return value.toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  });
}

function formatPercent(value: number): string {
  return `${value >= 0 ? '+' : ''}${value.toFixed(2)}%`;
}

export function NetWorthDisplay({ summary, history, showSparkline }: NetWorthDisplayProps) {
  const isPositive = summary.net_worth >= 0;
  const changeIsPositive = (summary.period_comparison?.net_worth_change ?? 0) >= 0;

  return (
    <div className="flex flex-col h-full">
      {/* Net Worth Value - prominent display */}
      <div
        className={`text-3xl font-bold ${
          isPositive ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'
        }`}
      >
        {formatCurrency(summary.net_worth)}
      </div>

      {/* Period Change with user-friendly label */}
      {summary.period_comparison && (
        <div className="flex items-center gap-1 text-sm mt-1">
          <span className={changeIsPositive ? 'text-green-500' : 'text-red-500'}>
            {changeIsPositive ? '↑' : '↓'}
          </span>
          <span>{formatCurrency(summary.period_comparison.net_worth_change)}</span>
          <span className="text-gray-500">
            ({formatPercent(summary.period_comparison.net_worth_change_percent)})
          </span>
          <span className="text-gray-400 text-xs">vs last month</span>
        </div>
      )}

      {/* Optional Sparkline Chart */}
      {showSparkline && history && history.snapshots.length > 0 && (
        <div className="mt-3 h-16">
          <NetWorthSparkline data={history.snapshots} />
        </div>
      )}

      {/* Assets & Liabilities Breakdown */}
      <div className="mt-auto pt-3 space-y-2 border-t border-gray-100 dark:border-gray-700">
        <div className="flex justify-between text-sm">
          <span className="text-gray-600 dark:text-gray-400">Assets</span>
          <span className="text-green-600 dark:text-green-400 font-medium">
            {formatCurrency(summary.total_assets)}
          </span>
        </div>
        <div className="flex justify-between text-sm">
          <span className="text-gray-600 dark:text-gray-400">Liabilities</span>
          <span className="text-red-600 dark:text-red-400 font-medium">
            {formatCurrency(summary.total_liabilities)}
          </span>
        </div>
      </div>
    </div>
  );
}
