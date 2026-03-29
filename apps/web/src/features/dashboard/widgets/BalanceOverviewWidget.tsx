/**
 * Balance Overview Widget
 * Issue #354: Customizable Dashboard Widget System
 *
 * Wraps the existing BalanceOverview component for use in the widget system.
 */

import { useMemo } from 'react';
import { Wallet } from 'lucide-react';
import { BalanceOverview } from '../components/BalanceOverview';
import { useDashboardContext } from '../context/DashboardContext';
import { useDashboardData } from '../hooks/useDashboardData';
import { registerWidget } from './registry';
import { WidgetEmptyState } from './WidgetEmptyState';
import type { WidgetProps } from '../types';

function BalanceOverviewWidget(_props: WidgetProps) {
  const { filters } = useDashboardContext();

  // useMemo to prevent infinite re-renders from new object references
  const dashboardFilters = useMemo(() => ({
    startDate: filters.startDate,
    endDate: filters.endDate,
    accountTypes: filters.accountTypes,
  }), [filters.startDate, filters.endDate, filters.accountTypes]);

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

  if (!summary || (summary.total_balance === 0 && Object.keys(summary.by_type).length === 0)) {
    return <WidgetEmptyState widgetType="balance-overview" />;
  }

  return (
    <div className="h-full">
      <BalanceOverview summary={summary} />
    </div>
  );
}

// Register the widget
// Issue #468: Increased minSize.h from 2 to 3 for proper content rendering
registerWidget({
  type: 'balance-overview',
  title: 'Balance Overview',
  description: 'Current balance summary with period comparison',
  icon: 'banknotes',
  category: 'overview',
  defaultSize: { w: 4, h: 3 },
  minSize: { w: 3, h: 3 },
  Component: BalanceOverviewWidget,
  emptyState: {
    icon: <Wallet />,
    title: 'No balance data yet',
    noAccountsDescription: 'Connect a bank account to see your balance overview',
    noDataDescription: 'No balance data available',
  },
});

export { BalanceOverviewWidget };
