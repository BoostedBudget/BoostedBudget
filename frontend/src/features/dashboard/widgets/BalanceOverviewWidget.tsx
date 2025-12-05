/**
 * Balance Overview Widget
 * Issue #354: Customizable Dashboard Widget System
 *
 * Wraps the existing BalanceOverview component for use in the widget system.
 */

import { BalanceOverview } from '../components/BalanceOverview';
import { useDashboardContext } from '../context/DashboardContext';
import { useDashboardData } from '../hooks/useDashboardData';
import { registerWidget } from './registry';
import type { WidgetProps } from '../types';

function BalanceOverviewWidget(_props: WidgetProps) {
  const { filters } = useDashboardContext();

  const dashboardFilters = {
    startDate: filters.startDate,
    endDate: filters.endDate,
    accountTypes: filters.accountTypes,
  };

  const { summary, loading, error } = useDashboardData(dashboardFilters);

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

  if (!summary) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400 text-sm">
        No data available
      </div>
    );
  }

  return (
    <div className="h-full">
      <BalanceOverview summary={summary} />
    </div>
  );
}

// Register the widget
registerWidget({
  type: 'balance-overview',
  title: 'Balance Overview',
  description: 'Current balance summary with period comparison',
  icon: 'banknotes',
  category: 'overview',
  defaultSize: { w: 4, h: 3 },
  minSize: { w: 3, h: 2 },
  Component: BalanceOverviewWidget,
});

export { BalanceOverviewWidget };
