/**
 * Cash Flow Chart Widget Tests
 *
 * Tests the CashFlowChartWidget component including loading,
 * error, and success states with the Plotly chart rendering.
 *
 * Issue #354: Customizable Dashboard Widget System
 * Issue #445: Migrated from Recharts to Plotly
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
// Issue #575: Use centralized registry mock to prevent mock.module() pollution
import '@/test/mocks/registry'

const mockUseCashFlowHistory = mock(() => ({
  data: null,
  loading: false,
  error: null,
  refetch: mock(() => {}),
}))

mock.module('../hooks/useCashFlowData', () => ({
  useCashFlowHistory: mockUseCashFlowHistory,
}))

mock.module('@/shared/charts', () => ({
  Plot: mock(({ data, layout, config, style, useResizeHandler }: {
    data: Array<{ name: string }>
    layout: { shapes?: unknown }
    config: { responsive?: boolean }
    style: React.CSSProperties
    useResizeHandler: boolean
  }) => (
    <div
      data-testid="plotly-chart"
      data-trace-count={data.length}
      data-trace-names={JSON.stringify(data.map((d) => d.name))}
      data-use-resize-handler={useResizeHandler}
      data-responsive={config?.responsive}
      data-has-shapes={layout?.shapes ? 'true' : 'false'}
      style={style}
    >
      Plotly Chart Mock
    </div>
  )),
  useDarkMode: mock(() => false),
  getBarChartLayout: mock(() => ({
    autosize: true,
    margin: { t: 10, r: 10, b: 40, l: 60 },
    barmode: 'group',
    xaxis: { gridcolor: '#e5e7eb' },
    yaxis: { gridcolor: '#e5e7eb' },
    legend: { font: {} },
  })),
  createHorizontalReferenceLine: mock(() => [
    { type: 'line', y0: 0, y1: 0, line: { color: '#e5e7eb' } },
  ]),
  DEFAULT_CONFIG: {
    responsive: true,
    displayModeBar: 'hover',
    scrollZoom: false,
    modeBarButtonsToRemove: ['lasso2d', 'select2d'],
    displaylogo: false,
  },
  CASH_FLOW_COLORS: {
    income: '#3b82f6',
    expenses: '#f97316',
    netPositive: '#10b981',
    netNegative: '#ef4444',
  },
  formatCurrencyFull: mock((value: number) =>
    `$${value.toLocaleString('en-US', { minimumFractionDigits: 2 })}`
  ),
  formatCurrencyCompact: mock((value: number) => {
    if (Math.abs(value) >= 1000) return `$${(value / 1000).toFixed(1)}k`
    return `$${value.toFixed(0)}`
  }),
}))

import { CashFlowChartWidget } from './CashFlowChartWidget'
import {
  DashboardContext,
  DashboardContextValue,
} from '../context/DashboardContext'

// Wrapper with context
const TestWrapper = ({
  children,
  contextValue,
}: {
  children: React.ReactNode
  contextValue: DashboardContextValue | null
}) => (
  <DashboardContext.Provider value={contextValue}>
    {children}
  </DashboardContext.Provider>
)

describe('CashFlowChartWidget', () => {
  const mockContextValue: DashboardContextValue = {
    filters: {
      startDate: '2024-01-01',
      endDate: '2024-01-31',
    },
    setFilters: mock(() => {}),
    timeframe: 'month',
    setTimeframe: mock(() => {}),
    isEditing: false,
    setIsEditing: mock(() => {}),
  }

  const mockHistoryData = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    grouping: 'weekly' as const,
    periods: [
      {
        period_start: '2024-01-01',
        period_end: '2024-01-07',
        income: 1200,
        expenses: 800,
        net_cash_flow: 400,
        transaction_count: 10,
      },
      {
        period_start: '2024-01-08',
        period_end: '2024-01-14',
        income: 1100,
        expenses: 750,
        net_cash_flow: 350,
        transaction_count: 8,
      },
    ],
  }

  beforeEach(() => {
    mockUseCashFlowHistory.mockClear()
  })

  describe('State Handling', () => {
    it('should show prompt when no date range', () => {
      const contextWithoutDates = {
        ...mockContextValue,
        filters: { startDate: '', endDate: '' },
      }

      mockUseCashFlowHistory.mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={contextWithoutDates}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(
        screen.getByText('Select a date range to view cash flow chart')
      ).toBeInTheDocument()
    })

    it('should show loading spinner when loading', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: null,
        loading: true,
        error: null,
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })

    it('should show error message when error occurs', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: null,
        loading: false,
        error: 'Failed to load chart data',
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(screen.getByText('Failed to load chart data')).toBeInTheDocument()
    })

    it('should show no data message when data is null', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(
        screen.getByText('No cash flow data available for this period')
      ).toBeInTheDocument()
    })

    it('should show no data message when periods array is empty', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: { ...mockHistoryData, periods: [] },
        loading: false,
        error: null,
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(
        screen.getByText('No cash flow data available for this period')
      ).toBeInTheDocument()
    })
  })

  describe('Plotly Chart Rendering', () => {
    it('should render Plotly chart component', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: mockHistoryData,
        loading: false,
        error: null,
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument()
    })

    it('should render 3 traces with correct names and configuration', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: mockHistoryData,
        loading: false,
        error: null,
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      const chart = screen.getByTestId('plotly-chart')
      expect(chart.getAttribute('data-trace-count')).toBe('3')

      const traceNames = JSON.parse(
        chart.getAttribute('data-trace-names') || '[]'
      )
      expect(traceNames).toContain('Income')
      expect(traceNames).toContain('Expenses')
      expect(traceNames).toContain('Net Cash Flow')
    })

    it('should have reference line and responsive config enabled', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: mockHistoryData,
        loading: false,
        error: null,
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      const chart = screen.getByTestId('plotly-chart')
      expect(chart.getAttribute('data-has-shapes')).toBe('true')
      expect(chart.getAttribute('data-responsive')).toBe('true')
      expect(chart.getAttribute('data-use-resize-handler')).toBe('true')
    })
  })

  describe('Context Integration', () => {
    it('should pass correct filters to useCashFlowHistory', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: mock(() => {}),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(mockUseCashFlowHistory).toHaveBeenCalledWith(
        expect.objectContaining({
          filters: expect.objectContaining({
            startDate: '2024-01-01',
            endDate: '2024-01-31',
          }),
          enabled: true,
        })
      )
    })
  })

  describe('Layout and Styling - Issues #420, #422', () => {
    it('should have proper flex layout and responsive dimensions', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: mockHistoryData,
        loading: false,
        error: null,
        refetch: mock(() => {}),
      })

      const { container } = render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      const outerWrapper = container.firstChild as HTMLElement
      expect(outerWrapper).toHaveClass('h-full')
      expect(outerWrapper).toHaveClass('flex')
      expect(outerWrapper).toHaveClass('flex-col')

      const plotChart = screen.getByTestId('plotly-chart')
      const chartWrapper = plotChart.parentElement
      expect(chartWrapper).toHaveClass('flex-1')
      expect(chartWrapper).toHaveClass('min-h-0')

      expect(plotChart).toHaveStyle({ width: '100%', height: '100%' })
      expect(plotChart).toHaveStyle({ minHeight: '200px' })
    })
  })
})
