/**
 * Cash Flow Chart Widget
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 * Issue #445: Migrated from Recharts to Plotly
 *
 * Displays a dual bar chart showing income and expenses per period
 * with a net cash flow trend line overlay.
 */

import { useMemo, useContext } from 'react';
import { format, parseISO } from 'date-fns';
import type { Data, Layout } from 'plotly.js';
import {
  Plot,
  useDarkMode,
  getBarChartLayout,
  createHorizontalReferenceLine,
  DEFAULT_CONFIG,
  CASH_FLOW_COLORS,
  formatCurrencyFull,
} from '@/shared/charts';
import { useCashFlowHistory } from '../hooks/useCashFlowData';
import { DashboardContext } from '../context/DashboardContext';
import { registerWidget } from './registry';
import type { WidgetProps, CashFlowPeriodData } from '../types';

interface ChartDataPoint {
  label: string;
  income: number;
  expenses: number;
  net: number;
}

function formatPeriodLabel(
  period: CashFlowPeriodData,
  grouping: string
): string {
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

function CashFlowChartWidget(_props: WidgetProps) {
  const context = useContext(DashboardContext);
  const isDark = useDarkMode();

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

  // Build Plotly traces
  const traces = useMemo((): Data[] => {
    if (chartData.length === 0) return [];

    return [
      // Income bar
      {
        x: chartData.map((d) => d.label),
        y: chartData.map((d) => d.income),
        type: 'bar' as const,
        name: 'Income',
        marker: { color: CASH_FLOW_COLORS.income },
        hovertemplate: chartData.map(
          (d) => `<b>Income</b>: ${formatCurrencyFull(d.income)}<extra></extra>`
        ),
      },
      // Expenses bar
      {
        x: chartData.map((d) => d.label),
        y: chartData.map((d) => d.expenses),
        type: 'bar' as const,
        name: 'Expenses',
        marker: { color: CASH_FLOW_COLORS.expenses },
        hovertemplate: chartData.map(
          (d) =>
            `<b>Expenses</b>: ${formatCurrencyFull(d.expenses)}<extra></extra>`
        ),
      },
      // Net cash flow line
      {
        x: chartData.map((d) => d.label),
        y: chartData.map((d) => d.net),
        type: 'scatter' as const,
        mode: 'lines+markers' as const,
        name: 'Net Cash Flow',
        line: {
          color: CASH_FLOW_COLORS.netPositive,
          width: 2,
        },
        marker: { size: 6 },
        hovertemplate: chartData.map((d) => {
          const color = d.net >= 0 ? 'green' : 'red';
          return `<b style="color:${color}">Net</b>: ${formatCurrencyFull(d.net)}<extra></extra>`;
        }),
      },
    ];
  }, [chartData]);

  // Build layout with theme support
  const layout = useMemo((): Partial<Layout> => {
    const base = getBarChartLayout(isDark);
    return {
      ...base,
      margin: { t: 10, r: 10, b: 40, l: 60 },
      yaxis: {
        ...base.yaxis,
        tickformat: '$.2s', // Compact format like $1.2k
      },
      shapes: createHorizontalReferenceLine(0, isDark),
    };
  }, [isDark]);

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
        <Plot
          data={traces}
          layout={layout}
          config={DEFAULT_CONFIG}
          style={{ width: '100%', height: '100%', minHeight: 200 }}
          useResizeHandler={true}
        />
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
