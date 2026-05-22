// Centralized @/shared/charts mock for component tests (ECharts surface).

import { mock } from 'bun:test'
import React from 'react'

// === useDarkMode hook mock ===
export const mockUseDarkMode = mock(() => false)

// === Color constants ===
export const MOCK_CHART_COLORS = {
  primary: '#3b82f6',
  secondary: '#8b5cf6',
  success: '#10b981',
  warning: '#f59e0b',
  error: '#ef4444',
}

export const MOCK_ACCOUNT_TYPE_COLORS = {
  balance: '#3b82f6',
  depository: '#10b981',
  credit: '#ef4444',
  investment: '#8b5cf6',
  loan: '#f59e0b',
}

export const MOCK_CASH_FLOW_COLORS = {
  income: '#3b82f6',
  expenses: '#f97316',
  netPositive: '#10b981',
  netNegative: '#ef4444',
}

export const MOCK_CATEGORY_COLORS = [
  '#3b82f6', '#10b981', '#f59e0b', '#8b5cf6', '#f97316',
  '#06b6d4', '#ec4899', '#84cc16', '#6366f1', '#14b8a6',
] as const

export const MOCK_THEME_COLORS = {
  background: { light: '#ffffff', dark: '#1f2937' },
  text: { light: '#1f2937', dark: '#f9fafb' },
  grid: { light: '#e5e7eb', dark: '#374151' },
}

// === Formatter mocks ===
export const mockFormatCurrency = mock(
  (value: number) => `$${value.toLocaleString('en-US', { minimumFractionDigits: 2 })}`
)

export const mockFormatCurrencyCompact = mock((value: number) => {
  if (Math.abs(value) >= 1000) return `$${(value / 1000).toFixed(1)}k`
  return `$${value.toFixed(0)}`
})

export const mockFormatCurrencyFull = mock(
  (value: number) => `$${value.toLocaleString('en-US', { minimumFractionDigits: 2 })}`
)

// === ECharts option builder mocks ===
export const mockGetBaseOption = mock(() => ({
  animation: true,
  animationDuration: 400,
  grid: { top: 10, right: 10, bottom: 40, left: 60 },
  xAxis: { type: 'category' },
  yAxis: { type: 'value' },
  legend: { orient: 'horizontal', bottom: 0, left: 'center' },
  tooltip: { trigger: 'item' },
}))

export const mockGetCurrencyOption = mock(() => ({
  ...mockGetBaseOption(),
  yAxis: { type: 'value', axisLabel: { formatter: (v: number) => `$${v}` } },
}))

export const mockGetBarOption = mock(() => ({
  ...mockGetCurrencyOption(),
  tooltip: { trigger: 'axis' },
}))

export const mockCreateMarkLineData = mock((yValue: number) => ({
  silent: true,
  symbol: ['none', 'none'],
  animation: false,
  data: [{ yAxis: yValue, lineStyle: { type: 'dotted', color: '#e5e7eb' } }],
}))

// === EChartsChart mock ===
type SeriesEntry = { name?: string; type?: string; markLine?: unknown; data?: unknown[] }
type EChartsOption = { series?: SeriesEntry[] }

const EChartsChartMockComponent = ({
  option = {},
  style,
  isDark,
  onEvents,
}: {
  option?: EChartsOption
  style?: React.CSSProperties
  isDark?: boolean
  onEvents?: Record<string, (params: { dataIndex: number }) => void>
}) => {
  const series = option?.series ?? []
  // Internal series (name starting with __) are implementation details — exclude from
  // visible counts/names so tests assert on the user-facing chart surface only.
  const visibleSeries = series.filter((s) => !s.name?.startsWith('__'))
  const hasMarkline = series.some((s) => s.markLine !== undefined)
  const marklineSeries = series.filter((s) => s.markLine !== undefined).map((s) => s.name ?? '')

  // Hidden trigger buttons for testing click events.
  // For pie/donut charts (single series with data[]), buttons correspond to each data point.
  // For multi-series charts, buttons correspond to each series.
  // Fires onEvents.click with { dataIndex: N } ONLY — no name/data per Codex H1.
  const firstSeriesData = series[0]?.data as unknown[] | undefined
  const triggerCount = onEvents?.click
    ? (Array.isArray(firstSeriesData) && firstSeriesData.length > 0 ? firstSeriesData.length : series.length)
    : 0
  const triggerButtons = Array.from({ length: triggerCount }, (_, idx) =>
    React.createElement('button', {
      key: idx,
      'aria-hidden': 'true',
      style: { display: 'none' },
      'data-testid': `echarts-point-${idx}`,
      onClick: () => onEvents?.click?.({ dataIndex: idx }),
    })
  )

  return React.createElement(
    'div',
    {
      'data-testid': 'echarts-chart',
      'data-series-count': visibleSeries.length,
      'data-series-names': JSON.stringify(visibleSeries.map((s) => s.name)),
      'data-series-types': JSON.stringify(visibleSeries.map((s) => s.type)),
      'data-has-markline': hasMarkline ? 'true' : 'false',
      'data-markline-series': JSON.stringify(marklineSeries),
      'data-renderer': 'svg',
      'data-is-dark': isDark ? 'true' : 'false',
      style,
    },
    'ECharts Mock',
    ...triggerButtons
  )
}
export const MockEChartsChart = mock(EChartsChartMockComponent)

// escapeHtml is a pure utility — pass through the real implementation, no mock needed
import { escapeHtml } from '../../shared/charts/escapeHtml'

// === Mock the module ===
mock.module('@/shared/charts', () => ({
  EChartsChart: MockEChartsChart,
  useDarkMode: mockUseDarkMode,
  getBaseOption: mockGetBaseOption,
  getCurrencyOption: mockGetCurrencyOption,
  getBarOption: mockGetBarOption,
  createMarkLineData: mockCreateMarkLineData,
  CHART_COLORS: MOCK_CHART_COLORS,
  ACCOUNT_TYPE_COLORS: MOCK_ACCOUNT_TYPE_COLORS,
  CASH_FLOW_COLORS: MOCK_CASH_FLOW_COLORS,
  CATEGORY_COLORS: MOCK_CATEGORY_COLORS,
  THEME_COLORS: MOCK_THEME_COLORS,
  formatCurrency: mockFormatCurrency,
  formatCurrencyCompact: mockFormatCurrencyCompact,
  formatCurrencyFull: mockFormatCurrencyFull,
  escapeHtml,
}))
