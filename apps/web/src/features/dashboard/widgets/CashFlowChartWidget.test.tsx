/**
 * Cash Flow Chart Widget Tests
 *
 * Tests the CashFlowChartWidget component including loading,
 * error, and success states with the Plotly chart rendering.
 *
 * Issue #354: Customizable Dashboard Widget System
 * Issue #445: Migrated from Recharts to Plotly
 * Issue #575: Refactored to use centralized mocks (no inline mock.module)
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'

// Issue #575: Use centralized mocks to prevent mock.module() pollution
// No inline mock.module() calls - mocks are preloaded via bunfig.toml
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
    // Reset to default return value
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

      mockUseCashFlowHistory.mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: mockCashFlowHistoryRefetch,
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
        refetch: mockCashFlowHistoryRefetch,
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
        refetch: mockCashFlowHistoryRefetch,
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(screen.getByText('Failed to load chart data')).toBeInTheDocument()
    })

    it('should show empty state when data is null', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: mockCashFlowHistoryRefetch,
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('should show empty state when periods array is empty', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: { ...mockHistoryData, periods: [] },
        loading: false,
        error: null,
        refetch: mockCashFlowHistoryRefetch,
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={mock(() => {})} />
        </TestWrapper>
      )

      expect(screen.getByRole('status')).toBeInTheDocument()
    })
  })

  describe('Plotly Chart Rendering', () => {
    it('should render Plotly chart component', () => {
      mockUseCashFlowHistory.mockReturnValue({
        data: mockHistoryData,
        loading: false,
        error: null,
        refetch: mockCashFlowHistoryRefetch,
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
        refetch: mockCashFlowHistoryRefetch,
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
        refetch: mockCashFlowHistoryRefetch,
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
        refetch: mockCashFlowHistoryRefetch,
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
        refetch: mockCashFlowHistoryRefetch,
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
