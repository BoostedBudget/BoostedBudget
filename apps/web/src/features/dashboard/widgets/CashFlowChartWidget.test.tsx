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

import { CashFlowChartWidget } from './CashFlowChartWidget'
import {
  DashboardContext,
  DashboardContextValue,
} from '../context/DashboardContext'

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

  beforeEach(() => {
    mockUseCashFlowHistory.mockClear()
    mockCashFlowHistoryRefetch.mockClear()
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
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(document.querySelector('.animate-spin')).toBeInTheDocument()
    })

    it('should show error message when error occurs', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: null, loading: false, error: 'Failed to load chart data', refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByText('Failed to load chart data')).toBeInTheDocument()
    })

    it('should show empty state when data is null', () => {
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('should show empty state when periods array is empty', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: { ...mockHistoryData, periods: [] }, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByRole('status')).toBeInTheDocument()
    })
  })

  describe('ECharts Rendering', () => {
    it('should render ECharts chart component', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })

    it('should render 3 series with correct names', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      const chart = screen.getByTestId('echarts-chart')
      expect(chart.dataset.seriesCount).toBe('3')
      const names = JSON.parse(chart.dataset.seriesNames ?? '[]') as string[]
      expect(names).toContain('Income')
      expect(names).toContain('Expenses')
      expect(names).toContain('Net Cash Flow')
    })

    it('should have reference line (markLine) on Income series only', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      const chart = screen.getByTestId('echarts-chart')
      expect(chart.dataset.hasMarkline).toBe('true')
      // Regression guard (Codex H2): markLine must be on Income (always visible),
      // NOT on Net Cash Flow (legend-hideable — hiding it would hide the y=0 reference line).
      const marklineSeries = JSON.parse(chart.dataset.marklineSeries ?? '[]') as string[]
      expect(marklineSeries).toContain('Income')
      expect(marklineSeries).not.toContain('Net Cash Flow')
    })

    it('data-renderer is always svg', () => {
      mockUseCashFlowHistory.mockReturnValue({ data: mockHistoryData, loading: false, error: null, refetch: mockCashFlowHistoryRefetch })
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
      expect(screen.getByTestId('echarts-chart').dataset.renderer).toBe('svg')
    })
  })

  describe('Context Integration', () => {
    it('should pass correct filters to useCashFlowHistory', () => {
      render(<TestWrapper contextValue={mockContextValue}><CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} /></TestWrapper>)
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
})
