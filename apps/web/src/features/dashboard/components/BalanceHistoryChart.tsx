/**
 * Balance History Chart
 * Issue #418: Fresh implementation from scratch
 * Issue #525: Auto-scale axes on filter timeframe changes
 *
 * Pure ECharts visualization component for balance trends over time.
 * No title - WidgetWrapper provides it.
 * Supports single line (total balance) and multi-line breakdown by account type.
 */

import { useMemo } from 'react';
import type { EChartsOption } from 'echarts';
import {
  EChartsChart,
  useDarkMode,
  getCurrencyOption,
  ACCOUNT_TYPE_COLORS,
  formatCurrency,
  escapeHtml,
} from '@/shared/charts';
import { formatHistoryLabel } from '@/shared/dates';
import type { BalanceHistory } from '../types';

interface BalanceHistoryChartProps {
  history: BalanceHistory;
  showTypeBreakdown?: boolean;
  filterKey?: string;
}

export function BalanceHistoryChart({
  history,
  showTypeBreakdown = false,
}: BalanceHistoryChartProps) {
  const isDark = useDarkMode();

  const option = useMemo((): EChartsOption => {
    const base = getCurrencyOption(isDark);
    const { snapshots } = history;
    const accountTypes = !showTypeBreakdown || !snapshots.some((s) => s.by_type)
      ? []
      : [...new Set(snapshots.flatMap((s) => Object.keys((s.by_type as Record<string, number>) || {})))];

    const series: EChartsOption['series'] = showTypeBreakdown && accountTypes.length > 0
      ? accountTypes.map((typeKey) => {
          const color = ACCOUNT_TYPE_COLORS[typeKey as keyof typeof ACCOUNT_TYPE_COLORS] || '#6b7280';
          return {
            name: typeKey.charAt(0).toUpperCase() + typeKey.slice(1),
            type: 'line' as const,
            // `?? null` rather than `?? 0`: a day with no recorded balance for this account
            // type is a GAP, not a zero balance. ECharts renders null as a break in the line,
            // and connectNulls stays off so the break is visible instead of bridged. Same
            // anti-pattern as the zero-filled future buckets #1626 Phase 3 removed.
            data: snapshots.map((s) => (s.by_type as Record<string, number> | undefined)?.[typeKey] ?? null),
            smooth: true,
            // A MODIFIER of smooth, not a replacement — with smooth false it does nothing.
            // Stops the curve overshooting between points without flattening a genuine step.
            smoothMonotone: 'x' as const,
            showSymbol: true,
            symbolSize: 6,
            lineStyle: { color, width: 2 },
            itemStyle: { color },
          };
        })
      : [{
          name: 'Total Balance',
          type: 'line' as const,
          data: snapshots.map((s) => s.balance),
          smooth: true,
          smoothMonotone: 'x' as const,
          showSymbol: true,
          symbolSize: 6,
          lineStyle: { color: ACCOUNT_TYPE_COLORS.balance, width: 2 },
          itemStyle: { color: ACCOUNT_TYPE_COLORS.balance },
        }];

    return {
      ...base,
      xAxis: { type: 'category', data: snapshots.map((s) => formatHistoryLabel(s.date, 'MMM d')) },
      tooltip: {
        trigger: 'axis',
        formatter: (params: unknown) => {
          if (!Array.isArray(params) || params.length === 0) return '';
          const points = params as Array<{ seriesName: string; value: number | null; dataIndex: number }>;
          const snapshot = snapshots[points[0].dataIndex];
          const label = snapshot ? formatHistoryLabel(snapshot.date, 'MMMM d, yyyy') : '';
          // A null point is a gap; listing it would print a fabricated amount for a day this
          // account type has no balance for.
          const observed = points.filter((p) => p.value !== null && p.value !== undefined);
          return `<b>${escapeHtml(label)}</b><br>${observed.map((p) => `${escapeHtml(p.seriesName)}: ${formatCurrency(p.value as number)}`).join('<br>')}`;
        },
      },
      series,
    };
  }, [isDark, history, showTypeBreakdown]);

  return (
    <div className="h-full">
      <EChartsChart
        option={option}
        isDark={isDark}
        style={{ width: '100%', height: '100%', minHeight: 200 }}
      />
    </div>
  );
}
