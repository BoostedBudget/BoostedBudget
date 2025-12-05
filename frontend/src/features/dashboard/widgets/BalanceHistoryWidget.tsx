/**
 * Balance History Widget
 * Issue #354: Customizable Dashboard Widget System
 *
 * Wraps the existing BalanceHistoryChart component for use in the widget system.
 */

import { useState } from 'react';
import { BalanceHistoryChart } from '../components/BalanceHistoryChart';
import { useDashboardContext } from '../context/DashboardContext';
import { useDashboardData } from '../hooks/useDashboardData';
import { registerWidget } from './registry';
import type { WidgetProps } from '../types';

function BalanceHistoryWidget(_props: WidgetProps) {
  const { filters } = useDashboardContext();
  const [showTypeBreakdown, setShowTypeBreakdown] = useState(false);

  const dashboardFilters = {
    startDate: filters.startDate,
    endDate: filters.endDate,
    accountTypes: filters.accountTypes,
  };

  const { history, loading, error } = useDashboardData(dashboardFilters);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-full text-red-500 text-sm">
        {error.message}
      </div>
    );
  }

  if (!history) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400 text-sm">
        No history data available
      </div>
    );
  }

  return (
    <div className="h-full relative">
      <BalanceHistoryChart history={history} showTypeBreakdown={showTypeBreakdown} />
      <div className="absolute top-0 right-0">
        <button
          onClick={() => setShowTypeBreakdown(!showTypeBreakdown)}
          className="px-2 py-1 text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 rounded hover:bg-gray-200 dark:hover:bg-gray-600"
        >
          {showTypeBreakdown ? 'Total' : 'Breakdown'}
        </button>
      </div>
    </div>
  );
}

// Register the widget
registerWidget({
  type: 'balance-history',
  title: 'Balance History',
  description: 'Historical balance trend chart',
  icon: 'chart-line',
  category: 'charts',
  defaultSize: { w: 6, h: 4 },
  minSize: { w: 4, h: 3 },
  Component: BalanceHistoryWidget,
});

export { BalanceHistoryWidget };
