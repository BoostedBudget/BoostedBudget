/**
 * Cash Flow Summary Widget
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 *
 * Displays income, expenses, and net cash flow summary cards
 * with period comparison indicators.
 */

import { useMemo, useContext } from 'react';
import {
  ArrowUpIcon,
  ArrowDownIcon,
  ArrowsRightLeftIcon,
} from '@heroicons/react/24/solid';
import { BarChart3 } from 'lucide-react';
import { useCashFlowSummary } from '../hooks/useCashFlowData';
import { DashboardContext } from '../context/DashboardContext';
import { registerWidget } from './registry';
import { WidgetEmptyState } from './WidgetEmptyState';
import type { WidgetProps } from '../types';

const formatCurrency = (value: number) =>
  new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 }).format(value);

interface SummaryCardProps {
  title: string;
  value: number;
  count?: number;
  changeAmount?: number;
  changePercent?: number;
  color: 'blue' | 'orange' | 'green' | 'red';
  icon: React.ComponentType<{ className?: string }>;
}

function SummaryCard({
  title,
  value,
  count,
  changeAmount,
  changePercent,
  color,
  icon: Icon,
}: SummaryCardProps) {
  const colorClasses = {
    blue: 'text-blue-600 bg-blue-100 dark:text-blue-400 dark:bg-blue-900/30',
    orange: 'text-orange-600 bg-orange-100 dark:text-orange-400 dark:bg-orange-900/30',
    green: 'text-green-600 bg-green-100 dark:text-green-400 dark:bg-green-900/30',
    red: 'text-red-600 bg-red-100 dark:text-red-400 dark:bg-red-900/30',
  };

  const formatPercent = (percent: number) => {
    const sign = percent >= 0 ? '+' : '';
    return `${sign}${percent.toFixed(1)}%`;
  };

  const isPositiveChange = changeAmount !== undefined && changeAmount >= 0;

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 mb-2">
        <div className={`p-1.5 rounded-lg ${colorClasses[color]}`}>
          <Icon className="h-4 w-4" />
        </div>
        <span className="text-sm font-medium text-gray-600 dark:text-gray-400">
          {title}
        </span>
      </div>

      <div className="flex-1 flex flex-col justify-center">
        <div className="text-2xl font-bold text-gray-900 dark:text-white">
          {formatCurrency(value)}
        </div>

        {count !== undefined && (
          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
            {count} transaction{count !== 1 ? 's' : ''}
          </div>
        )}

        {changeAmount !== undefined && changePercent !== undefined && (
          <div className="flex items-center gap-1 mt-1">
            <span
              className={`inline-flex items-center text-xs font-medium ${
                isPositiveChange
                  ? 'text-green-600 dark:text-green-400'
                  : 'text-red-600 dark:text-red-400'
              }`}
            >
              {isPositiveChange ? (
                <ArrowUpIcon className="h-3 w-3 mr-0.5" />
              ) : (
                <ArrowDownIcon className="h-3 w-3 mr-0.5" />
              )}
              {formatPercent(changePercent)}
            </span>
            <span className="text-xs text-gray-500 dark:text-gray-400">
              vs prior period
            </span>
          </div>
        )}
      </div>
    </div>
  );
}

function CashFlowSummaryWidget(_props: WidgetProps) {
  const context = useContext(DashboardContext);
  const filters = useMemo(
    () => ({
      startDate: context?.filters.startDate || '',
      endDate: context?.filters.endDate || '',
      accountTypes: context?.filters.accountTypes,
      includeTransfers: false,
    }),
    [context?.filters]
  );

  const { data, loading, error } = useCashFlowSummary({
    filters,
    enabled: Boolean(filters.startDate && filters.endDate),
  });

  if (!context?.filters.startDate || !context?.filters.endDate) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400">
        Select a date range to view cash flow
      </div>
    );
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-full text-red-500">
        {error}
      </div>
    );
  }

  if (!data || (data.income === 0 && data.expenses === 0 && data.income_count === 0 && data.expense_count === 0)) {
    return <WidgetEmptyState widgetType="cash-flow-summary" />;
  }

  const netIsPositive = data.net_cash_flow >= 0;
  const comparison = data.period_comparison;

  return (
    <div className="grid grid-cols-3 gap-4 h-full overflow-hidden">
      <SummaryCard
        title="Income"
        value={data.income}
        count={data.income_count}
        changeAmount={comparison?.income_change_amount}
        changePercent={comparison?.income_change_percent}
        color="blue"
        icon={ArrowUpIcon}
      />
      <SummaryCard
        title="Expenses"
        value={data.expenses}
        count={data.expense_count}
        changeAmount={comparison?.expense_change_amount}
        changePercent={comparison?.expense_change_percent}
        color="orange"
        icon={ArrowDownIcon}
      />
      <SummaryCard
        title="Net Cash Flow"
        value={data.net_cash_flow}
        changeAmount={comparison?.net_change_amount}
        changePercent={comparison?.net_change_percent}
        color={netIsPositive ? 'green' : 'red'}
        icon={ArrowsRightLeftIcon}
      />
    </div>
  );
}

// Register the widget
// Issue #468: Increased minSize.h from 2 to 3 for proper content rendering
registerWidget({
  type: 'cash-flow-summary',
  title: 'Cash Flow Summary',
  description: 'Income, expenses, and net cash flow with period comparison',
  icon: 'chart-bar',
  category: 'overview',
  defaultSize: { w: 8, h: 3 },
  minSize: { w: 6, h: 3 },
  Component: CashFlowSummaryWidget,
  emptyState: {
    icon: <BarChart3 />,
    title: 'No cash flow data yet',
    noAccountsDescription: 'Connect a bank account to see income and expense summaries',
    noDataDescription: 'No transactions found for this time period',
  },
});

export { CashFlowSummaryWidget };
