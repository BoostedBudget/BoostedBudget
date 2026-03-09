/**
 * Dashboard widget displaying current net worth (assets - liabilities),
 * with optional configurable sparkline trend chart.
 */

import { useState, useMemo } from 'react';
import { TrendingUp } from 'lucide-react';
import { NetWorthDisplay } from '../components/NetWorthDisplay';
import { useDashboardContext } from '../context/DashboardContext';
import { useNetWorthData } from '../hooks/useNetWorthData';
import { registerWidget } from './registry';
import { WidgetEmptyState } from './WidgetEmptyState';

function NetWorthWidget() {
  const { filters } = useDashboardContext();
  const [showSparkline, setShowSparkline] = useState(false);

  // Memoize filters to prevent infinite re-renders
  const dashboardFilters = useMemo(
    () => ({
      startDate: filters.startDate,
      endDate: filters.endDate,
      accountTypes: filters.accountTypes,
    }),
    [filters.startDate, filters.endDate, filters.accountTypes]
  );

  const { summary, history, loading, error } = useNetWorthData(dashboardFilters);

  // Loading state
  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="flex items-center justify-center h-full text-red-500 text-sm">
        {error.message}
      </div>
    );
  }

  // Empty state
  if (!summary || (summary.total_assets === 0 && summary.total_liabilities === 0 && Object.keys(summary.by_category).length === 0)) {
    return <WidgetEmptyState widgetType="net-worth" />;
  }

  return (
    <div className="h-full flex flex-col">
      {/* Toggle with user-friendly tooltip */}
      <div className="flex justify-end mb-2">
        <button
          onClick={() => setShowSparkline(!showSparkline)}
          className="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 flex items-center gap-1 px-2 py-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
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

      <NetWorthDisplay summary={summary} history={showSparkline ? history ?? undefined : undefined} showSparkline={showSparkline} />
    </div>
  );
}

// Register the widget
registerWidget({
  type: 'net-worth',
  title: 'Net Worth',
  description:
    'Track your total net worth with assets and liabilities breakdown. Toggle trend chart on/off.',
  icon: 'chart-line', // Uses TrendingUp icon
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
});

export { NetWorthWidget };
