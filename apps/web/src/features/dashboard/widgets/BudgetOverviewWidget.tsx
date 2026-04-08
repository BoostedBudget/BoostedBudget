/**
 * Budget Overview Widget
 * Issue #30: Budget Dashboard UI
 *
 * Shows current budget progress in the dashboard widget system.
 */

import { Link } from 'react-router';
import { PieChart } from 'lucide-react';
import { useCurrentBudget } from '../../budgets/hooks/useCurrentBudget';
import { BudgetProgressBar } from '../../budgets/components/BudgetProgressBar';
import { EmptyState } from '@/components/ui/empty-state';
import { Button } from '@/components/ui/button';
import { registerWidget } from './registry';
import type { WidgetProps } from '../types';

/**
 * Format currency amount
 */
function formatCurrency(amount: number, currencyCode: string): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currencyCode,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount);
  } catch {
    return `${currencyCode} ${amount.toFixed(2)}`;
  }
}

function BudgetOverviewWidget(_props: WidgetProps) {
  const { budget, metrics, loading, error, hasNoBudget } = useCurrentBudget();

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
      </div>
    );
  }

  if (error && !hasNoBudget) {
    return (
      <div className="flex items-center justify-center h-full gap-2 text-destructive text-sm">
        <span className="flex items-center justify-center w-6 h-6 bg-destructive text-white rounded-full text-xs font-bold">!</span>
        <span>Error loading budget</span>
      </div>
    );
  }

  if (hasNoBudget || !budget) {
    return (
      <EmptyState
        size="compact"
        icon={<PieChart />}
        title="No active budget"
        description="Create a budget to track your spending"
        action={
          <Button size="sm" asChild>
            <Link to="/budgets">Create Budget</Link>
          </Button>
        }
        className="h-full"
      />
    );
  }

  const spent = metrics?.total_spent ?? 0;
  const remaining = metrics?.remaining_amount ?? 0;

  return (
    <div className="flex flex-col gap-4 h-full p-2">
      <div className="flex justify-between items-center">
        <h3 className="m-0 text-base font-semibold text-foreground">{budget.name}</h3>
        <span className="text-xs capitalize text-muted-foreground bg-muted px-2 py-0.5 rounded-full">{budget.period}</span>
      </div>

      <div className="text-2xl font-bold text-foreground">
        {formatCurrency(budget.amount, budget.currency_code)}
      </div>

      <BudgetProgressBar
        spent={spent}
        total={budget.amount}
        currencyCode={budget.currency_code}
        size="medium"
      />

      <div className="flex justify-between mt-auto">
        <div className="flex flex-col gap-0.5">
          <span className="text-xs text-muted-foreground">Remaining</span>
          <span className="text-sm font-semibold text-foreground">
            {formatCurrency(remaining, budget.currency_code)}
          </span>
        </div>
        <div className="flex flex-col gap-0.5">
          <span className="text-xs text-muted-foreground">Categories</span>
          <span className="text-sm font-semibold text-foreground">
            {budget.categories?.length ?? 0}
          </span>
        </div>
      </div>
    </div>
  );
}

// Register the widget
// Issue #468: Increased minSize.h from 2 to 3 for proper content rendering
registerWidget({
  type: 'budget-overview',
  title: 'Budget Overview',
  description: 'Current budget progress and spending summary',
  icon: 'chart-pie',
  category: 'overview',
  defaultSize: { w: 4, h: 3 },
  minSize: { w: 3, h: 3 },
  Component: BudgetOverviewWidget,
  emptyState: {
    icon: <PieChart />,
    title: 'No active budget',
    noAccountsDescription: 'Create a budget to track your spending',
    noDataDescription: 'Create a budget to track your spending',
    noAccountsAction: (
      <Button size="sm" asChild>
        <Link to="/budgets">Create Budget</Link>
      </Button>
    ),
  },
});

export { BudgetOverviewWidget };
