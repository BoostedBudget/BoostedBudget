import { useMemo } from 'react'
import type { EChartsOption } from 'echarts'
import {
  EChartsChart,
  useDarkMode,
  getCurrencyOption,
  CASH_FLOW_COLORS,
  formatCurrency,
  escapeHtml,
} from '@/shared/charts'
import { formatHistoryLabel } from '@/shared/dates'
import type { NetWorthHistory } from '@/features/dashboard/types'

const NET_WORTH_COLOR = '#3b82f6'

interface NetWorthHistoryChartProps {
  history: NetWorthHistory
}

export function NetWorthHistoryChart({ history }: NetWorthHistoryChartProps) {
  const isDark = useDarkMode()
  const { snapshots } = history

  const option = useMemo((): EChartsOption => {
    const base = getCurrencyOption(isDark);
    return {
      ...base,
      xAxis: { type: 'category', data: snapshots.map((s) => formatHistoryLabel(s.date, 'MMM d')) },
      tooltip: {
        trigger: 'axis',
        formatter: (params: unknown) => {
          if (!Array.isArray(params) || params.length === 0) return '';
          const points = params as Array<{ seriesName: string; value: number; dataIndex: number }>;
          const snapshot = snapshots[points[0].dataIndex];
          const label = snapshot ? formatHistoryLabel(snapshot.date, 'MMMM d, yyyy') : '';
          return `<b>${escapeHtml(label)}</b><br>${points.map((p) => `${escapeHtml(p.seriesName)}: ${formatCurrency(p.value)}`).join('<br>')}`;
        },
      },
      series: [
        {
          name: 'Net Worth',
          type: 'line',
          data: snapshots.map((s) => s.net_worth),
          smooth: true,
          showSymbol: false,
          lineStyle: { color: NET_WORTH_COLOR, width: 2 },
          itemStyle: { color: NET_WORTH_COLOR },
        },
        {
          name: 'Assets',
          type: 'line',
          data: snapshots.map((s) => s.total_assets),
          smooth: true,
          showSymbol: false,
          lineStyle: { color: CASH_FLOW_COLORS.netPositive, width: 1.5 },
          itemStyle: { color: CASH_FLOW_COLORS.netPositive },
        },
        {
          name: 'Liabilities',
          type: 'line',
          data: snapshots.map((s) => s.total_liabilities),
          smooth: true,
          showSymbol: false,
          lineStyle: { color: CASH_FLOW_COLORS.netNegative, width: 1.5 },
          itemStyle: { color: CASH_FLOW_COLORS.netNegative },
        },
      ],
    };
  }, [isDark, snapshots]);

  if (snapshots.length < 2) {
    return (
      <div className="flex items-center justify-center h-[300px] text-sm text-muted-foreground">
        Not enough history yet to display a chart.
      </div>
    );
  }

  return (
    <div className="h-[300px] sm:h-[400px] lg:h-[500px]" data-testid="net-worth-history-chart">
      <EChartsChart
        option={option}
        isDark={isDark}
        style={{ width: '100%', height: '100%' }}
      />
    </div>
  )
}
