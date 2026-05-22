/**
 * Cash Flow Chart Widget Tests
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'

import '@/test/mocks/registry'
import '@/test/mocks/shared-charts'
import {
  mockUseCashFlowHistory,
  mockCashFlowHistoryRefetch,
} from '@/test/mocks/cash-flow-hooks'
import { mockUseDarkMode, MockEChartsChart } from '@/test/mocks/shared-charts'

import { CashFlowChartWidget } from './CashFlowChartWidget'
import {
  DashboardContext,
  DashboardContextValue,
} from '../context/DashboardContext'

const TestWrapper = ({
  children,
  contextValue = mockContextValue,
}: {
  children: React.ReactNode
  contextValue?: DashboardContextValue | null
}) => (
  <DashboardContext.Provider value={contextValue}>
    {children}
  </DashboardContext.Provider>
)

const mockContextValue: DashboardContextValue = {
    filters: {
      startDate: '2024-01-01',
      endDate: '2024-01-31',
    },
    setFilters: mock(() => {}),
    timeframe: 'this_month',
    setTimeframe: mock(() => {}),
    isEditing: false,
    setIsEditing: mock(() => {}),
    hasAccounts: false,
    accountsLoading: false,
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

describe('CashFlowChartWidget', () => {
  beforeEach(() => {
    mockUseCashFlowHistory.mockClear()
    mockCashFlowHistoryRefetch.mockClear()
    mockUseDarkMode.mockReturnValue(false)
    mockUseCashFlowHistory.mockReturnValue({
      data: null,
      loading: false,
      error: null,
      refetch: mockCashFlowHistoryRefetch,
    })
  })

  describe('State Handling', () => {
    it('should show prompt when no date range', () => {
      const contextWithoutDates = {
        ...mockContextValue,
        filters: { startDate: '', endDate: '' },
      }
      mockUseCashFlowHistory.mockReturnValue({ data: null, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper contextValue={contextWithoutDates}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByText('Select a date range to view cash flow chart')).toBeInTheDocument()
    })

    it('should show loading spinner when loading', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: null, loading: true, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(document.querySelector('.animate-spin')).toBeInTheDocument()
    })

    it('should show error message when error occurs', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: null, loading: false, error: 'Failed to load chart data', refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByText('Failed to load chart data')).toBeInTheDocument()
    })

    it('should show empty state when data is null', () => {
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('should show empty state when periods array is empty', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: { ...mockHistoryData, periods: [] }, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByRole('status')).toBeInTheDocument()
    })
  })

  describe('ECharts Rendering', () => {
    it('should render ECharts chart component', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })

    it('should render 3 series with correct names', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      const chart = screen.getByTestId('echarts-chart')
      expect(chart.dataset.seriesCount).toBe('3')
      const names = JSON.parse(chart.dataset.seriesNames ?? '[]') as string[]
      expect(names).toContain('Income')
      expect(names).toContain('Expenses')
      expect(names).toContain('Net Cash Flow')
    })

    it('reference line is decoupled from legend-visible series — Income toggle cannot hide y=0 line (Codex Third Loop C1)', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
        series?: Array<{ name?: string; markLine?: unknown }>
        legend?: { data?: string[] }
      }
      // Visible series must NOT carry markLine — toggling them via legend cannot hide the y=0 line.
      const visibleNames = ['Income', 'Expenses', 'Net Cash Flow']
      for (const name of visibleNames) {
        const s = option.series?.find((s) => s.name === name)
        expect(s?.markLine).toBeUndefined()
      }
      // Hidden series (__refline__) hosts the reference line.
      const reflineSeries = option.series?.find((s) => s.name === '__refline__')
      expect(reflineSeries?.markLine).toBeDefined()
      // legend.data excludes __refline__ — user cannot toggle it off.
      expect(option.legend?.data).toEqual(visibleNames)
    })

    it('data-renderer is always svg', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByTestId('echarts-chart').dataset.renderer).toBe('svg')
    })
  })

  describe('Context Integration', () => {
    it('should pass correct filters to useCashFlowHistory', () => {
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(mockUseCashFlowHistory).toHaveBeenCalledWith(
        expect.objectContaining({
          filters: expect.objectContaining({ startDate: '2024-01-01', endDate: '2024-01-31' }),
          enabled: true,
        })
      )
    })
  })

  describe('Layout and Styling', () => {
    it('should have proper flex layout', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      const { container } = render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      const outerWrapper = container.firstChild as HTMLElement
      expect(outerWrapper).toHaveClass('h-full')
      expect(outerWrapper).toHaveClass('flex')
      expect(outerWrapper).toHaveClass('flex-col')
      const chart = screen.getByTestId('echarts-chart')
      expect(chart.parentElement).toHaveClass('flex-1')
      expect(chart.parentElement).toHaveClass('min-h-0')
    })
  })

  describe('Dark Mode', () => {
    it('reflects dark mode state in isDark prop', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      mockUseDarkMode.mockReturnValue(true)
      const { unmount } = render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
      unmount()

      mockUseDarkMode.mockReturnValue(false)
      render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
    })
  })

  it('tooltip formatter escapes malicious series names (XSS regression — #1169)', () => {
    mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
    render(<TestWrapper><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
    const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      tooltip?: { formatter?: (p: unknown) => string }
    }
    const formatted = option.tooltip!.formatter!([{ seriesName: '<script>alert(1)</script>', value: 400 }])
    expect(formatted).not.toMatch(/<(script|img|svg)/i)
    expect(formatted).toContain('&lt;script')
  })
})
