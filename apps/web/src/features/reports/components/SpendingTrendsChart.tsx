/**
 * Spending Trends Chart
 * Issue #953: Spending Reports Page
 * Issue #1169: Plotly to ECharts Migration
 *
 * Stacked bar chart showing top 5 spending categories over time.
 * Other categories are bucketed into "Other".
 */

import { useMemo } from 'react'
import { format, parseISO } from 'date-fns'
import type { EChartsOption } from 'echarts'
import {
  EChartsChart,
  useDarkMode,
  getBarOption,
  CATEGORY_COLORS,
  formatCurrencyFull,
} from '@/shared/charts'
import { formatPFCPrimary } from '@/shared/categories/formatPfc'
import type { SpendingTrendsResponse } from '@boostedbudget/api-client/models'

interface SpendingTrendsChartProps {
  data: SpendingTrendsResponse | null
  loading: boolean
  className?: string
}

export function SpendingTrendsChart({ data, loading, className }: SpendingTrendsChartProps) {
  const isDark = useDarkMode()

  const option = useMemo((): EChartsOption => {
    const periods = data?.periods ?? [];
    const topCategories = data?.top_categories ?? [];

    if (periods.length === 0 || topCategories.length === 0) return {};

    const allCategories = [...topCategories];
    if (periods.some((p) => p.categories?.some((c) => c.category === 'OTHER'))) {
      allCategories.push('OTHER');
    }

    const xLabels = periods.map((p) => {
      try {
        return format(parseISO(p.period_start), 'MMM yyyy');
      } catch {
        return p.period_start;
      }
    });

    const findDisplayName = (cat: string): string => {
      if (cat === 'OTHER') return 'Other';
      for (const p of periods) {
        const found = p.categories?.find((c) => c.category === cat);
        if (found?.pfc_primary_display) return found.pfc_primary_display;
      }
      if (cat.startsWith('custom:')) return 'Custom Category';
      return formatPFCPrimary(cat);
    };

    const base = getBarOption(isDark);
    const series = allCategories.map((cat, i) => {
      const displayName = findDisplayName(cat);
      return {
        type: 'bar' as const,
        name: displayName,
        stack: 'total',
        data: periods.map((p) => p.categories?.find((c) => c.category === cat)?.total ?? 0),
        itemStyle: { color: CATEGORY_COLORS[i % CATEGORY_COLORS.length] },
      };
    });

    return {
      ...base,
      xAxis: { type: 'category', data: xLabels },
      yAxis: {
        ...(base.yAxis as object),
        axisLabel: {
          formatter: (v: unknown) => (typeof v === 'number' ? formatCurrencyFull(v) : String(v)),
        },
      },
      tooltip: {
        trigger: 'axis',
        formatter: (params: unknown) => {
          if (!Array.isArray(params)) return '';
          const points = params as Array<{ seriesName: string; value: number }>;
          return points.map((p) => `${p.seriesName}: ${formatCurrencyFull(p.value)}`).join('<br>');
        },
      },
      series,
    };
  }, [data, isDark]);

  if (loading) {
    return (
      <div className={className} aria-busy="true" aria-label="Loading trends chart" data-testid="trends-chart">
        <div className="h-64 animate-pulse rounded-lg bg-muted" />
      </div>
    );
  }

  if (!data?.periods || data.periods.length === 0) {
    return (
      <div className={className} role="status" data-testid="trends-chart">
        <p className="text-sm text-muted-foreground">No trend data available for this period.</p>
      </div>
    );
  }

  return (
    <div className={className} data-testid="trends-chart">
      <EChartsChart
        option={option}
        isDark={isDark}
        style={{ width: '100%', height: '320px' }}
      />
    </div>
  )
}
