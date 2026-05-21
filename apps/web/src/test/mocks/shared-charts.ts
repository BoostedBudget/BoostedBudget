// Issue #575: Centralized @/shared/charts mock
// This mocks the shared charts module (Plot, useDarkMode, layout utilities, colors, formatters)
// for component tests that need to test in isolation from Plotly.

import { mock } from 'bun:test'
import React from 'react'

// === Plot component mock ===
const PlotMockComponent = ({
  data,
  layout,
  config,
  style,
  useResizeHandler,
  onClick,
}: {
  data: Array<{ name: string; labels?: string[] }>
  layout: { shapes?: unknown }
  config: { responsive?: boolean }
  style: React.CSSProperties
  useResizeHandler: boolean
  onClick?: (event: { points: Array<{ pointIndex: number }> }) => void
}) => {
  // Build hidden per-label test-trigger buttons for the first labelled trace (e.g. pie charts).
  // Tests can use getByTestId('plotly-point-N') + fireEvent.click() to simulate Plotly point clicks.
  const firstLabels = (data[0] as { labels?: string[] } | undefined)?.labels
  const triggerButtons = onClick && firstLabels
    ? firstLabels.map((_: string, idx: number) =>
        React.createElement('button', {
          key: idx,
          'aria-hidden': 'true',
          style: { display: 'none' },
          'data-testid': `plotly-point-${idx}`,
          onClick: () => onClick({ points: [{ pointIndex: idx }] }),
        })
      )
    : []
  return React.createElement('div', {
    'data-testid': 'plotly-chart',
    'data-trace-count': data.length,
    'data-trace-names': JSON.stringify(data.map((d) => d.name)),
    'data-use-resize-handler': useResizeHandler,
    'data-responsive': config?.responsive,
    'data-has-shapes': layout?.shapes ? 'true' : 'false',
    style,
  }, 'Plotly Chart Mock', ...triggerButtons)
}
export const MockPlot = mock(PlotMockComponent)

// === Plotly instance mock ===
export const MockPlotly = {}

// === useDarkMode hook mock ===
export const mockUseDarkMode = mock(() => false)

// === Layout utilities ===
export const mockGetBaseLayout = mock(() => ({
  autosize: true,
  margin: { t: 10, r: 10, b: 40, l: 60 },
  xaxis: { gridcolor: '#e5e7eb' },
  yaxis: { gridcolor: '#e5e7eb' },
}))

export const mockGetCurrencyLayout = mock(() => ({
  ...mockGetBaseLayout(),
  yaxis: {
    gridcolor: '#e5e7eb',
    tickprefix: '$',
  },
}))

export const mockGetBarChartLayout = mock(() => ({
  autosize: true,
  margin: { t: 10, r: 10, b: 40, l: 60 },
  barmode: 'group',
  xaxis: { gridcolor: '#e5e7eb' },
  yaxis: { gridcolor: '#e5e7eb' },
  legend: { font: {} },
}))

export const mockCreateHorizontalReferenceLine = mock(() => [
  { type: 'line', y0: 0, y1: 0, line: { color: '#e5e7eb' } },
])

export const MOCK_DEFAULT_CONFIG = {
  responsive: true,
  displayModeBar: 'hover',
  scrollZoom: false,
  modeBarButtonsToRemove: ['lasso2d', 'select2d'],
  displaylogo: false,
}

// === Color constants ===
export const MOCK_CHART_COLORS = {
  primary: '#3b82f6',
  secondary: '#8b5cf6',
  success: '#10b981',
  warning: '#f59e0b',
  error: '#ef4444',
}

export const MOCK_ACCOUNT_TYPE_COLORS = {
  depository: '#3b82f6',
  investment: '#8b5cf6',
  credit: '#ef4444',
  loan: '#f59e0b',
  other: '#6b7280',
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
  light: {
    background: '#ffffff',
    text: '#1f2937',
    grid: '#e5e7eb',
  },
  dark: {
    background: '#1f2937',
    text: '#f9fafb',
    grid: '#374151',
  },
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

export const mockGetCurrencyTickFormat = mock(() => '$,.0f')
export const mockGetCurrencyCompactTickFormat = mock(() => '$~s')
export const mockCreateCurrencyHoverTemplate = mock(
  () => '%{y:$,.2f}<extra></extra>'
)
export const mockCreateTimeSeriesHoverTemplate = mock(
  () => '%{x}<br>%{y:$,.2f}<extra></extra>'
)

// === EChartsChart mock ===
type SeriesEntry = { name?: string; type?: string; markLine?: unknown; data?: unknown[] }
type EChartsOption = { series?: SeriesEntry[] }

const EChartsChartMockComponent = ({
  option = {},
  style,
  onEvents,
}: {
  option?: EChartsOption
  style?: React.CSSProperties
  isDark?: boolean
  className?: string
  notMerge?: boolean
  lazyUpdate?: boolean
  onEvents?: Record<string, (params: { dataIndex: number }) => void>
}) => {
  const series = option?.series ?? []
  const hasMarkline = series.some((s) => s.markLine !== undefined)

  // Hidden per-series trigger buttons for testing click events.
  // Fires onEvents.click with { dataIndex: N } ONLY — no name/data per plan Codex H1.
  const triggerButtons = onEvents?.click
    ? series.map((_: SeriesEntry, idx: number) =>
        React.createElement('button', {
          key: idx,
          'aria-hidden': 'true',
          style: { display: 'none' },
          'data-testid': `echarts-point-${idx}`,
          onClick: () => onEvents.click!({ dataIndex: idx }),
        })
      )
    : []

  return React.createElement(
    'div',
    {
      'data-testid': 'echarts-chart',
      'data-series-count': series.length,
      'data-series-names': JSON.stringify(series.map((s) => s.name)),
      'data-series-types': JSON.stringify(series.map((s) => s.type)),
      'data-has-markline': hasMarkline ? 'true' : 'false',
      'data-renderer': 'svg',
      style,
    },
    'ECharts Mock',
    ...triggerButtons
  )
}
export const MockEChartsChart = mock(EChartsChartMockComponent)

// === New ECharts option builder mocks ===
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

// === Mock the module (both OLD Plotly surface + NEW ECharts surface) ===
mock.module('@/shared/charts', () => ({
  // --- OLD Plotly surface (preserved until Phase 6.5) ---
  Plot: MockPlot,
  Plotly: MockPlotly,
  useDarkMode: mockUseDarkMode,
  DEFAULT_CONFIG: MOCK_DEFAULT_CONFIG,
  getBaseLayout: mockGetBaseLayout,
  getCurrencyLayout: mockGetCurrencyLayout,
  getBarChartLayout: mockGetBarChartLayout,
  createHorizontalReferenceLine: mockCreateHorizontalReferenceLine,
  getCurrencyTickFormat: mockGetCurrencyTickFormat,
  getCurrencyCompactTickFormat: mockGetCurrencyCompactTickFormat,
  createCurrencyHoverTemplate: mockCreateCurrencyHoverTemplate,
  createTimeSeriesHoverTemplate: mockCreateTimeSeriesHoverTemplate,
  // --- NEW ECharts surface ---
  EChartsChart: MockEChartsChart,
  getBaseOption: mockGetBaseOption,
  getCurrencyOption: mockGetCurrencyOption,
  getBarOption: mockGetBarOption,
  createMarkLineData: mockCreateMarkLineData,
  // --- Shared (unchanged) ---
  CHART_COLORS: MOCK_CHART_COLORS,
  ACCOUNT_TYPE_COLORS: MOCK_ACCOUNT_TYPE_COLORS,
  CASH_FLOW_COLORS: MOCK_CASH_FLOW_COLORS,
  CATEGORY_COLORS: MOCK_CATEGORY_COLORS,
  THEME_COLORS: MOCK_THEME_COLORS,
  formatCurrency: mockFormatCurrency,
  formatCurrencyCompact: mockFormatCurrencyCompact,
  formatCurrencyFull: mockFormatCurrencyFull,
}))
