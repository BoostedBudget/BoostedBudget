/**
 * Cash Flow Chart Widget
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 *
 * Displays a dual bar chart showing income and expenses per period
 * with a net cash flow trend line overlay.
 */

import { useMemo, useContext } from 'react';
import {
  ComposedChart,
  Bar,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceLine,
} from 'recharts';
import { format, parseISO } from 'date-fns';
import { useCashFlowHistory } from '../hooks/useCashFlowData';
import { DashboardContext } from '../context/DashboardContext';
import { registerWidget } from './registry';
import type { WidgetProps, CashFlowPeriodData } from '../types';

// Colorblind-safe palette
const COLORS = {
  income: '#3b82f6', // Blue
  expenses: '#f97316', // Orange
  net: '#10b981', // Green
  negativeNet: '#ef4444', // Red
};

interface ChartDataPoint {
  label: string;
  income: number;
  expenses: number;
  net: number;
}

function formatPeriodLabel(period: CashFlowPeriodData, grouping: string): string {
  const date = parseISO(period.period_start);

  switch (grouping) {
    case 'daily':
      return format(date, 'MMM d');
    case 'weekly':
      return `Week of ${format(date, 'MMM d')}`;
    case 'monthly':
      return format(date, 'MMM yyyy');
    default:
      return format(date, 'MMM d');
  }
}

function formatCurrency(value: number): string {
  if (Math.abs(value) >= 1000) {
    return `$${(value / 1000).toFixed(1)}k`;
  }
  return `$${value.toFixed(0)}`;
}

function CashFlowChartWidget(_props: WidgetProps) {
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

  const { data, loading, error } = useCashFlowHistory({
    filters,
    enabled: Boolean(filters.startDate && filters.endDate),
  });

  const chartData = useMemo<ChartDataPoint[]>(() => {
    if (!data?.periods) return [];

    return data.periods.map((period) => ({
      label: formatPeriodLabel(period, data.grouping),
      income: period.income,
      expenses: period.expenses,
      net: period.net_cash_flow,
    }));
  }, [data]);

  if (!context?.filters.startDate || !context?.filters.endDate) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400">
        Select a date range to view cash flow chart
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

  if (!data || chartData.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400">
        No cash flow data available for this period
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col">
      <div className="flex-1 min-h-0">
        <ResponsiveContainer width="100%" height="100%" minHeight={200}>
        <ComposedChart
          data={chartData}
          margin={{ top: 10, right: 10, left: 0, bottom: 0 }}
        >
          <CartesianGrid strokeDasharray="3 3" className="stroke-gray-200 dark:stroke-gray-700" />
          <XAxis
            dataKey="label"
            tick={{ fontSize: 11, fill: 'currentColor' }}
            tickLine={false}
            axisLine={{ stroke: 'currentColor', strokeOpacity: 0.1 }}
            className="text-gray-600 dark:text-gray-400"
          />
          <YAxis
            tickFormatter={formatCurrency}
            tick={{ fontSize: 11, fill: 'currentColor' }}
            tickLine={false}
            axisLine={false}
            className="text-gray-600 dark:text-gray-400"
          />
          <Tooltip
            contentStyle={{
              backgroundColor: 'rgb(255 255 255 / 95%)',
              border: '1px solid rgb(229 231 235)',
              borderRadius: '0.5rem',
              padding: '0.75rem',
            }}
            formatter={(value: number, name: string) => [
              new Intl.NumberFormat('en-US', {
                style: 'currency',
                currency: 'USD',
              }).format(value),
              name.charAt(0).toUpperCase() + name.slice(1),
            ]}
            labelStyle={{ fontWeight: 600, marginBottom: '0.25rem' }}
          />
          <Legend
            wrapperStyle={{ paddingTop: '0.5rem' }}
            iconType="rect"
            formatter={(value) => (
              <span className="text-sm text-gray-700 dark:text-gray-300 capitalize">
                {value}
              </span>
            )}
          />
          <ReferenceLine y={0} stroke="currentColor" strokeOpacity={0.2} />
          <Bar
            dataKey="income"
            name="income"
            fill={COLORS.income}
            radius={[4, 4, 0, 0]}
            maxBarSize={50}
          />
          <Bar
            dataKey="expenses"
            name="expenses"
            fill={COLORS.expenses}
            radius={[4, 4, 0, 0]}
            maxBarSize={50}
          />
          <Line
            type="monotone"
            dataKey="net"
            name="net cash flow"
            stroke={COLORS.net}
            strokeWidth={2}
            dot={{ fill: COLORS.net, strokeWidth: 0, r: 3 }}
            activeDot={{ r: 5, strokeWidth: 0 }}
          />
        </ComposedChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}

// Register the widget
registerWidget({
  type: 'cash-flow-chart',
  title: 'Cash Flow Chart',
  description: 'Bar chart showing income vs expenses with net cash flow trend',
  icon: 'chart-bar',
  category: 'charts',
  defaultSize: { w: 6, h: 4 },
  minSize: { w: 4, h: 3 },
  Component: CashFlowChartWidget,
});

export { CashFlowChartWidget };
