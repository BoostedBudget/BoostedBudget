import { useMemo } from 'react'
import { format, parseISO } from 'date-fns'
import type { Data, Layout } from 'plotly.js'
import {
  Plot,
  useDarkMode,
  getBaseLayout,
  DEFAULT_CONFIG,
  CASH_FLOW_COLORS,
  formatCurrency,
} from '@/shared/charts'
import type { NetWorthHistory } from '@/features/dashboard/types'

const NET_WORTH_COLOR = '#3b82f6'

interface NetWorthHistoryChartProps {
  history: NetWorthHistory
  filterKey: string
}

export function NetWorthHistoryChart({ history, filterKey }: NetWorthHistoryChartProps) {
  const isDark = useDarkMode()
  const snapshots = history.snapshots
  const hasEnoughData = snapshots.length >= 2

  const dates = useMemo(
    () => snapshots.map((s) => format(parseISO(s.date), 'MMM d')),
    [snapshots]
  )

  const traces = useMemo((): Data[] => [
    {
      x: dates,
      y: snapshots.map((s) => s.net_worth),
      type: 'scatter',
      mode: 'lines',
      name: 'Net Worth',
      line: { color: NET_WORTH_COLOR, width: 2, shape: 'spline' },
      hovertemplate: snapshots.map(
        (s) => `<b>${format(parseISO(s.date), 'MMMM d, yyyy')}</b><br>Net Worth: ${formatCurrency(s.net_worth)}<extra></extra>`
      ),
    },
    {
      x: dates,
      y: snapshots.map((s) => s.total_assets),
      type: 'scatter',
      mode: 'lines',
      name: 'Assets',
      line: { color: CASH_FLOW_COLORS.netPositive, width: 1.5, shape: 'spline' },
      hovertemplate: snapshots.map(
        (s) => `<b>${format(parseISO(s.date), 'MMMM d, yyyy')}</b><br>Assets: ${formatCurrency(s.total_assets)}<extra></extra>`
      ),
    },
    {
      x: dates,
      y: snapshots.map((s) => s.total_liabilities),
      type: 'scatter',
      mode: 'lines',
      name: 'Liabilities',
      line: { color: CASH_FLOW_COLORS.netNegative, width: 1.5, shape: 'spline' },
      hovertemplate: snapshots.map(
        (s) => `<b>${format(parseISO(s.date), 'MMMM d, yyyy')}</b><br>Liabilities: ${formatCurrency(s.total_liabilities)}<extra></extra>`
      ),
    },
  ], [snapshots, dates])

  const layout = useMemo((): Partial<Layout> => {
    const base = getBaseLayout(isDark)
    return {
      ...base,
      margin: { t: 10, r: 20, b: 40, l: 70 },
      uirevision: filterKey,
      yaxis: { ...base.yaxis, tickformat: '$,.0f' },
    }
  }, [isDark, filterKey])

  if (!hasEnoughData) {
    return (
      <div className="flex items-center justify-center h-[300px] text-sm text-muted-foreground">
        Not enough history yet to display a chart.
      </div>
    )
  }

  return (
    <div className="h-[300px] sm:h-[400px] lg:h-[500px]">
      <Plot
        data={traces}
        layout={layout}
        config={DEFAULT_CONFIG}
        style={{ width: '100%', height: '100%' }}
        useResizeHandler
      />
    </div>
  )
}
