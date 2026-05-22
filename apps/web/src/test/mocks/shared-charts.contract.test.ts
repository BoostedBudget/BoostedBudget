/**
 * Contract test for @/shared/charts mock surface.
 * ECharts-only surface. Negative assertions verify deprecated legacy exports
 * are absent from both the mock and the barrel.
 */
import './shared-charts'
import { describe, it, expect } from 'bun:test'
import {
  // NEW ECharts surface
  MockEChartsChart,
  mockUseDarkMode,
  mockGetBaseOption,
  mockGetCurrencyOption,
  mockGetBarOption,
  mockCreateMarkLineData,
  MOCK_CHART_COLORS,
  MOCK_ACCOUNT_TYPE_COLORS,
  MOCK_CASH_FLOW_COLORS,
  MOCK_CATEGORY_COLORS,
  MOCK_THEME_COLORS,
  mockFormatCurrency,
  mockFormatCurrencyCompact,
  mockFormatCurrencyFull,
} from './shared-charts'
import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'

// ========================
// NEGATIVE assertions — deprecated legacy exports must be absent
// Regression guard: if any of these reappear, the migration regressed.
// ========================

// Forbidden mock exports (legacy chart library surface)
const FORBIDDEN_MOCK_EXPORTS = [
  'MockPlot', 'MockPlotly', 'mockGetBaseLayout', 'mockGetCurrencyLayout',
  'mockGetBarChartLayout', 'MOCK_DEFAULT_CONFIG',
] as const

// Forbidden barrel exports (legacy chart library surface)
const FORBIDDEN_BARREL_EXPORTS = [
  'Plot', 'Plotly', 'getBaseLayout', 'getCurrencyLayout',
  'getBarChartLayout', 'DEFAULT_CONFIG',
] as const

describe('shared-charts mock: deprecated legacy surface must be absent', () => {
  for (const name of FORBIDDEN_MOCK_EXPORTS) {
    it(`mock does not export ${name}`, async () => {
      const mod = await import('./shared-charts')
      expect((mod as Record<string, unknown>)[name]).toBeUndefined()
    })
  }
})

describe('shared-charts barrel: deprecated legacy surface must be absent', () => {
  for (const name of FORBIDDEN_BARREL_EXPORTS) {
    it(`barrel (@/shared/charts) does not export ${name}`, async () => {
      const barrel = await import('@/shared/charts')
      expect((barrel as Record<string, unknown>)[name]).toBeUndefined()
    })
  }
})

// ========================
// NEW ECharts surface checks
// ========================

describe('shared-charts mock: ECharts surface', () => {
  it('exports MockEChartsChart as a callable mock', () => {
    expect(typeof MockEChartsChart).toBe('function')
  })

  it('exports mockUseDarkMode as a callable mock', () => {
    expect(typeof mockUseDarkMode).toBe('function')
  })

  it('exports option builder mocks', () => {
    expect(typeof mockGetBaseOption).toBe('function')
    expect(typeof mockGetCurrencyOption).toBe('function')
    expect(typeof mockGetBarOption).toBe('function')
    expect(typeof mockCreateMarkLineData).toBe('function')
  })

  it('exports formatter mocks', () => {
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
    const btn = screen.getByTestId('echarts-point-0')
    fireEvent.click(btn)
    expect(capturedEvent).toEqual({ dataIndex: 0 })
    expect((capturedEvent as Record<string, unknown>).name).toBeUndefined()
    expect((capturedEvent as Record<string, unknown>).data).toBeUndefined()
  })
})
