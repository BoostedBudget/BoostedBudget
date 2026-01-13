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
}: {
  data: Array<{ name: string }>
  layout: { shapes?: unknown }
  config: { responsive?: boolean }
  style: React.CSSProperties
  useResizeHandler: boolean
}) =>
  React.createElement('div', {
    'data-testid': 'plotly-chart',
    'data-trace-count': data.length,
    'data-trace-names': JSON.stringify(data.map((d) => d.name)),
    'data-use-resize-handler': useResizeHandler,
    'data-responsive': config?.responsive,
    'data-has-shapes': layout?.shapes ? 'true' : 'false',
    style,
    children: 'Plotly Chart Mock',
  })
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

// === Mock the module ===
mock.module('@/shared/charts', () => ({
  Plot: MockPlot,
  Plotly: MockPlotly,
  useDarkMode: mockUseDarkMode,
  DEFAULT_CONFIG: MOCK_DEFAULT_CONFIG,
  getBaseLayout: mockGetBaseLayout,
  getCurrencyLayout: mockGetCurrencyLayout,
  getBarChartLayout: mockGetBarChartLayout,
  createHorizontalReferenceLine: mockCreateHorizontalReferenceLine,
  CHART_COLORS: MOCK_CHART_COLORS,
  ACCOUNT_TYPE_COLORS: MOCK_ACCOUNT_TYPE_COLORS,
  CASH_FLOW_COLORS: MOCK_CASH_FLOW_COLORS,
  THEME_COLORS: MOCK_THEME_COLORS,
  formatCurrency: mockFormatCurrency,
  formatCurrencyCompact: mockFormatCurrencyCompact,
  formatCurrencyFull: mockFormatCurrencyFull,
  getCurrencyTickFormat: mockGetCurrencyTickFormat,
  getCurrencyCompactTickFormat: mockGetCurrencyCompactTickFormat,
  createCurrencyHoverTemplate: mockCreateCurrencyHoverTemplate,
  createTimeSeriesHoverTemplate: mockCreateTimeSeriesHoverTemplate,
}))
