/**
 * Cash Flow Chart Widget
 * Issue #354: Cash Flow Analysis & Income/Expense Visualization
 * Issue #1169: Plotly to ECharts Migration
 */

import { useContext, useMemo } from 'react'
import { format, parseISO } from 'date-fns'
import { BarChart3 } from 'lucide-react'
import type { EChartsOption } from 'echarts'
import {
  EChartsChart,
  useDarkMode,
  getBarOption,
  createMarkLineData,
  CASH_FLOW_COLORS,
  formatCurrencyFull,
} from '@/shared/charts'
import { useCashFlowHistory } from '../hooks/useCashFlowData'
import { DashboardContext } from '../context/DashboardContext'
import { registerWidget } from './registry'
import { WidgetEmptyState } from './WidgetEmptyState'
import type { CashFlowPeriodData } from '../types'

interface ChartDataPoint {
  label: string
  income: number
  expenses: number
  net: number
}

function formatPeriodLabel(period: CashFlowPeriodData, grouping: string): string {
  const date = parseISO(period.period_start)
  switch (grouping) {
    case 'daily': return format(date, 'MMM d')
    case 'weekly': return `Week of ${format(date, 'MMM d')}`
    case 'monthly': return format(date, 'MMM yyyy')
    default: return format(date, 'MMM d')
  }
}

function CashFlowChartWidget() {
  const context = useContext(DashboardContext)
  const isDark = useDarkMode()

  const { data, loading, error } = useCashFlowHistory({
    filters: {
      startDate: context?.filters.startDate || '',
      endDate: context?.filters.endDate || '',
      accountTypes: context?.filters.accountTypes,
      includeTransfers: false,
    },
    enabled: Boolean(context?.filters.startDate && context?.filters.endDate),
  })

  const chartData = useMemo<ChartDataPoint[]>(() => {
    if (!data?.periods) return [];
    return data.periods.map((period) => ({
      label: formatPeriodLabel(period, data.grouping),
      income: period.income,
      expenses: period.expenses,
      net: period.net_cash_flow,
    }));
  }, [data]);

  const option = useMemo((): EChartsOption => {
    if (chartData.length === 0) return {}
    const base = getBarOption(isDark)
    const labels = chartData.map((d) => d.label)

    return {
      ...base,
      xAxis: { type: 'category', data: labels },
      yAxis: {
        ...(base.yAxis as object),
        axisLabel: {
          formatter: (v: unknown) => typeof v === 'number' ? formatCurrencyFull(v) : String(v),
        },
      },
      tooltip: {
        trigger: 'axis',
        formatter: (params: unknown) => {
          const points = params as Array<{ seriesName: string; value: number }>
          return points.map((p) => {
            const isNet = p.seriesName === 'Net Cash Flow'
            const color = isNet ? (p.value >= 0 ? 'green' : 'red') : 'inherit'
            return `<b style="color:${color}">${p.seriesName}</b>: ${formatCurrencyFull(p.value)}`
          }).join('<br>')
        },
      },
      series: [
        {
          name: 'Income',
          type: 'bar',
          data: chartData.map((d) => d.income),
          itemStyle: { color: CASH_FLOW_COLORS.income },
          barCategoryGap: '20%',
          // Attach markLine to Income (always present when data exists — Codex H2)
          markLine: createMarkLineData(0, isDark),
        },
        {
          name: 'Expenses',
          type: 'bar',
          data: chartData.map((d) => d.expenses),
          itemStyle: { color: CASH_FLOW_COLORS.expenses },
        },
        {
          name: 'Net Cash Flow',
          type: 'line',
          data: chartData.map((d) => d.net),
          smooth: false,
          symbol: 'circle',
          symbolSize: 6,
          lineStyle: { color: CASH_FLOW_COLORS.netPositive, width: 2 },
          itemStyle: { color: CASH_FLOW_COLORS.netPositive },
        },
      ],
    }
  }, [chartData, isDark])

  if (!context?.filters.startDate || !context?.filters.endDate) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400">
        Select a date range to view cash flow chart
      </div>
    )
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-full text-red-500 text-sm">
        {error}
      </div>
    )
  }

  if (!data || chartData.length === 0 || chartData.every(d => d.income === 0 && d.expenses === 0 && d.net === 0)) {
    return <WidgetEmptyState widgetType="cash-flow-chart" />
  }

  return (
    <div className="h-full flex flex-col">
      <div className="flex-1 min-h-0">
        <EChartsChart
          option={option}
          isDark={isDark}
          style={{ width: '100%', height: '100%', minHeight: 200 }}
        />
      </div>
    </div>
  )
}

registerWidget({
  type: 'cash-flow-chart',
  title: 'Cash Flow Chart',
  description: 'Bar chart showing income vs expenses with net cash flow trend',
  icon: 'chart-bar',
  category: 'charts',
  defaultSize: { w: 6, h: 4 },
  minSize: { w: 4, h: 4 },
  Component: CashFlowChartWidget,
  emptyState: {
    icon: <BarChart3 />,
    title: 'No cash flow data yet',
    noAccountsDescription: 'Connect a bank account to see your income and expenses',
    noDataDescription: 'No transactions found for this time period',
  },
})

export { CashFlowChartWidget }
