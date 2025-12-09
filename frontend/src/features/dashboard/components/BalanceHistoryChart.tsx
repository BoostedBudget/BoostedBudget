/**
 * Balance History Chart
 * Issue #353: Balance Tracking Dashboard
 * Issue #445: Migrated from Recharts to Plotly
 *
 * Displays balance trends over time using an interactive line chart.
 * Supports single line (total balance) and multi-line breakdown by account type.
 */

import { useMemo } from 'react';
import { format, parseISO } from 'date-fns';
import type { Data, Layout } from 'plotly.js';
import {
  Plot,
  useDarkMode,
  getBaseLayout,
  DEFAULT_CONFIG,
  ACCOUNT_TYPE_COLORS,
  formatCurrency,
} from '@/shared/charts';
import type { BalanceHistory } from '../types';

interface BalanceHistoryChartProps {
  history: BalanceHistory;
  showTypeBreakdown?: boolean;
}

/**
 * BalanceHistoryChart component displays balance trends over time
 *
 * Features:
 * - Interactive line chart with Plotly
 * - Responsive design with resize handling
 * - Optional breakdown by account type
 * - Formatted currency values
 * - Date-based x-axis
 * - Hover tooltips with detailed information
 * - Dark mode support
 * - Zoom/pan toolbar on hover
 */
export function BalanceHistoryChart({
  history,
  showTypeBreakdown = false,
}: BalanceHistoryChartProps) {
  const isDark = useDarkMode();

  // Transform data for Plotly
  const chartData = useMemo(() => {
    return history.snapshots.map((snapshot) => ({
      date: format(parseISO(snapshot.date), 'MMM d'),
      fullDate: snapshot.date,
      balance: snapshot.balance,
      ...(showTypeBreakdown && snapshot.by_type ? snapshot.by_type : {}),
    }));
  }, [history.snapshots, showTypeBreakdown]);

  // Get available account types from data
  const accountTypes = useMemo(() => {
    if (!showTypeBreakdown || !history.snapshots.some((s) => s.by_type)) {
      return [];
    }
    return Object.keys(history.snapshots[0]?.by_type || {});
  }, [history.snapshots, showTypeBreakdown]);

  // Build Plotly traces
  const traces = useMemo((): Data[] => {
    if (!showTypeBreakdown) {
      // Single line for total balance
      return [
        {
          x: chartData.map((d) => d.date),
          y: chartData.map((d) => d.balance),
          type: 'scatter' as const,
          mode: 'lines+markers' as const,
          name: 'Total Balance',
          line: {
            color: ACCOUNT_TYPE_COLORS.balance,
            width: 2,
            shape: 'spline',
          },
          marker: { size: 6 },
          hovertemplate: chartData.map(
            (d) =>
              `<b>${format(parseISO(d.fullDate), 'MMMM d, yyyy')}</b><br>` +
              `Total Balance: ${formatCurrency(d.balance)}<extra></extra>`
          ),
        },
      ];
    }

    // Multiple lines for account type breakdown
    return accountTypes.map((typeKey) => ({
      x: chartData.map((d) => d.date),
      y: chartData.map((d) => (d as Record<string, unknown>)[typeKey] as number ?? 0),
      type: 'scatter' as const,
      mode: 'lines+markers' as const,
      name: typeKey.charAt(0).toUpperCase() + typeKey.slice(1),
      line: {
        color:
          ACCOUNT_TYPE_COLORS[typeKey as keyof typeof ACCOUNT_TYPE_COLORS] ||
          '#6b7280',
        width: 2,
        shape: 'spline',
      },
      marker: { size: 6 },
      hovertemplate: chartData.map((d) => {
        const value = (d as Record<string, unknown>)[typeKey] as number ?? 0;
        return (
          `<b>${format(parseISO(d.fullDate), 'MMMM d, yyyy')}</b><br>` +
          `${typeKey.charAt(0).toUpperCase() + typeKey.slice(1)}: ${formatCurrency(value)}<extra></extra>`
        );
      }),
    }));
  }, [chartData, showTypeBreakdown, accountTypes]);

  // Build layout with theme support
  const layout = useMemo((): Partial<Layout> => {
    const base = getBaseLayout(isDark);
    return {
      ...base,
      margin: { t: 10, r: 20, b: 40, l: 70 },
      yaxis: {
        ...base.yaxis,
        tickformat: '$,.0f',
      },
      legend: {
        ...base.legend,
        font: { ...base.legend?.font, size: 14 },
      },
    };
  }, [isDark]);

  if (history.snapshots.length === 0) {
    return (
      <div className="h-full flex flex-col">
        <h2 className="flex-shrink-0 text-lg font-semibold text-gray-900 dark:text-white mb-4">
          Balance History
        </h2>
        <div className="flex-1 flex items-center justify-center">
          <p className="text-gray-500 dark:text-gray-400">
            No balance history available
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col">
      <h2 className="flex-shrink-0 text-lg font-semibold text-gray-900 dark:text-white mb-4">
        Balance History
      </h2>

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
