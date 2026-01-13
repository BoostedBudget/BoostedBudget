/**
 * Account Cards Widget
 * Issue #354: Customizable Dashboard Widget System
 *
 * Wraps the existing AccountBalanceCards component for use in the widget system.
 */

import { useMemo } from 'react';
import { AccountBalanceCards } from '../components/AccountBalanceCards';
import { useDashboardContext } from '../context/DashboardContext';
import { useDashboardData } from '../hooks/useDashboardData';
import { registerWidget } from './registry';
import type { WidgetProps } from '../types';

function AccountCardsWidget(_props: WidgetProps) {
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

  if (!summary) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400 text-sm">
        No account data available
      </div>
    );
  }

  return (
    <div className="h-full">
      <AccountBalanceCards summary={summary} />
    </div>
  );
}

// Register the widget
// Issue #468: Increased minSize.h from 2 to 3 for proper content rendering
registerWidget({
  type: 'account-cards',
  title: 'Account Cards',
  description: 'Individual account balance cards',
  icon: 'credit-card',
  category: 'accounts',
  defaultSize: { w: 12, h: 3 },
  minSize: { w: 6, h: 3 },
  Component: AccountCardsWidget,
});

export { AccountCardsWidget };
