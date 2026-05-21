/**
 * Contract test for @/shared/charts mock surface.
 * Verifies both OLD Plotly surface (preserved during Phase 5 transitional period)
 * and NEW ECharts surface are exported correctly from the mock.
 *
 * Phase 6.5: This test will be updated to ECharts-only + negative assertions
 * after all 5 chart consumers are migrated.
 */
import './shared-charts'
import { describe, it, expect } from 'bun:test'
import {
  // OLD Plotly surface (must stay until Phase 6.5)
  MockPlot,
  MockPlotly,
  mockUseDarkMode,
  mockGetBaseLayout,
  mockGetCurrencyLayout,
  mockGetBarChartLayout,
  mockCreateHorizontalReferenceLine,
  MOCK_DEFAULT_CONFIG,
  mockGetCurrencyTickFormat,
  mockGetCurrencyCompactTickFormat,
  mockCreateCurrencyHoverTemplate,
  mockCreateTimeSeriesHoverTemplate,
  MOCK_CHART_COLORS,
  MOCK_ACCOUNT_TYPE_COLORS,
  MOCK_CASH_FLOW_COLORS,
  MOCK_CATEGORY_COLORS,
  MOCK_THEME_COLORS,
  mockFormatCurrency,
  mockFormatCurrencyCompact,
  mockFormatCurrencyFull,
  // NEW ECharts surface
  MockEChartsChart,
  mockGetBaseOption,
  mockGetCurrencyOption,
  mockGetBarOption,
  mockCreateMarkLineData,
} from './shared-charts'
import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'

// ========================
// OLD Plotly surface checks
// ========================

describe('shared-charts mock: OLD Plotly surface (preserved until Phase 6.5)', () => {
  it('exports MockPlot as a callable mock', () => {
    expect(typeof MockPlot).toBe('function')
  })

  it('exports MockPlotly as an object', () => {
    expect(typeof MockPlotly).toBe('object')
  })

  it('exports mockUseDarkMode as a callable mock', () => {
    expect(typeof mockUseDarkMode).toBe('function')
  })

  it('exports old layout mocks', () => {
    expect(typeof mockGetBaseLayout).toBe('function')
    expect(typeof mockGetCurrencyLayout).toBe('function')
    expect(typeof mockGetBarChartLayout).toBe('function')
    expect(typeof mockCreateHorizontalReferenceLine).toBe('function')
  })

  it('exports MOCK_DEFAULT_CONFIG', () => {
    expect(MOCK_DEFAULT_CONFIG).toBeDefined()
    expect(MOCK_DEFAULT_CONFIG.responsive).toBe(true)
  })

  it('exports old formatter mocks', () => {
    expect(typeof mockGetCurrencyTickFormat).toBe('function')
    expect(typeof mockGetCurrencyCompactTickFormat).toBe('function')
    expect(typeof mockCreateCurrencyHoverTemplate).toBe('function')
    expect(typeof mockCreateTimeSeriesHoverTemplate).toBe('function')
    expect(typeof mockFormatCurrency).toBe('function')
    expect(typeof mockFormatCurrencyCompact).toBe('function')
    expect(typeof mockFormatCurrencyFull).toBe('function')
  })

  it('exports color constants', () => {
    expect(MOCK_CHART_COLORS).toBeDefined()
    expect(MOCK_ACCOUNT_TYPE_COLORS).toBeDefined()
    expect(MOCK_CASH_FLOW_COLORS).toBeDefined()
    expect(MOCK_CATEGORY_COLORS).toBeDefined()
    expect(MOCK_THEME_COLORS).toBeDefined()
  })
})

// ========================
// NEW ECharts surface checks
// ========================

describe('shared-charts mock: NEW ECharts surface', () => {
  it('exports MockEChartsChart as a callable mock', () => {
    expect(typeof MockEChartsChart).toBe('function')
  })

  it('MockEChartsChart renders data-testid="echarts-chart"', () => {
    render(React.createElement(MockEChartsChart, {
      option: { series: [{ name: 'Income', type: 'bar' }] },
    }))
    expect(screen.getByTestId('echarts-chart')).toBeDefined()
  })

  it('MockEChartsChart exposes data-series-count', () => {
    render(React.createElement(MockEChartsChart, {
      option: { series: [{ name: 'A', type: 'bar' }, { name: 'B', type: 'line' }] },
    }))
    expect(screen.getByTestId('echarts-chart').dataset.seriesCount).toBe('2')
  })

  it('MockEChartsChart exposes data-series-names as JSON', () => {
    render(React.createElement(MockEChartsChart, {
      option: { series: [{ name: 'Income', type: 'bar' }, { name: 'Expenses', type: 'bar' }] },
    }))
    expect(screen.getByTestId('echarts-chart').dataset.seriesNames).toBe('["Income","Expenses"]')
  })

  it('MockEChartsChart exposes data-series-types as JSON', () => {
    render(React.createElement(MockEChartsChart, {
      option: { series: [{ name: 'Slice', type: 'pie' }] },
    }))
    expect(screen.getByTestId('echarts-chart').dataset.seriesTypes).toBe('["pie"]')
  })

  it('MockEChartsChart exposes data-has-markline=false when no markLine', () => {
    render(React.createElement(MockEChartsChart, {
      option: { series: [{ name: 'A', type: 'bar' }] },
    }))
    expect(screen.getByTestId('echarts-chart').dataset.hasMarkline).toBe('false')
  })

  it('MockEChartsChart exposes data-has-markline=true when markLine present on any series', () => {
    render(React.createElement(MockEChartsChart, {
      option: {
        series: [
          { name: 'Income', type: 'bar', markLine: { data: [{ yAxis: 0 }] } },
          { name: 'Net', type: 'line' },
        ],
      },
    }))
    expect(screen.getByTestId('echarts-chart').dataset.hasMarkline).toBe('true')
  })

  it('MockEChartsChart exposes data-renderer=svg always', () => {
    render(React.createElement(MockEChartsChart, {
      option: { series: [] },
    }))
    expect(screen.getByTestId('echarts-chart').dataset.renderer).toBe('svg')
  })

  it('MockEChartsChart click fires onEvents.click with {dataIndex: N} ONLY (no name/data)', () => {
    let capturedEvent: unknown = null
    const onEvents = {
      click: (params: unknown) => { capturedEvent = params },
    }
    render(React.createElement(MockEChartsChart, {
      option: { series: [{ name: 'Groceries', type: 'pie', data: [{ value: 100, name: 'Food' }] }] },
      onEvents,
    }))
    // First data point trigger button
    const btn = screen.getByTestId('echarts-point-0')
    fireEvent.click(btn)
    expect(capturedEvent).toEqual({ dataIndex: 0 })
    expect((capturedEvent as Record<string, unknown>).name).toBeUndefined()
    expect((capturedEvent as Record<string, unknown>).data).toBeUndefined()
  })

  it('exports new option builder mocks', () => {
    expect(typeof mockGetBaseOption).toBe('function')
    expect(typeof mockGetCurrencyOption).toBe('function')
    expect(typeof mockGetBarOption).toBe('function')
    expect(typeof mockCreateMarkLineData).toBe('function')
  })
})
