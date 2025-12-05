/**
 * Cash Flow Chart Widget Tests
 *
 * Tests the CashFlowChartWidget component including loading,
 * error, and success states with the chart rendering.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import React from 'react'

// Mock hooks and registry
vi.mock('../hooks/useCashFlowData', () => ({
  useCashFlowHistory: vi.fn(),
}))

vi.mock('./registry', () => ({
  registerWidget: vi.fn(),
}))

// Mock recharts
vi.mock('recharts', () => ({
  ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="responsive-container">{children}</div>
  ),
  ComposedChart: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="composed-chart">{children}</div>
  ),
  Bar: () => <div data-testid="bar" />,
  Line: () => <div data-testid="line" />,
  XAxis: () => <div data-testid="x-axis" />,
  YAxis: () => <div data-testid="y-axis" />,
  CartesianGrid: () => <div data-testid="cartesian-grid" />,
  Tooltip: () => <div data-testid="tooltip" />,
  Legend: () => <div data-testid="legend" />,
  ReferenceLine: () => <div data-testid="reference-line" />,
}))

import { CashFlowChartWidget } from './CashFlowChartWidget'
import { useCashFlowHistory } from '../hooks/useCashFlowData'
import { DashboardContext, DashboardContextValue } from '../context/DashboardContext'

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
    setFilters: vi.fn(),
    timeframe: 'month',
    setTimeframe: vi.fn(),
    isEditing: false,
    setIsEditing: vi.fn(),
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
    vi.clearAllMocks()
  })

  describe('Missing Date Range', () => {
    it('should show prompt when no date range', () => {
      const contextWithoutDates = {
        ...mockContextValue,
        filters: { startDate: '', endDate: '' },
      }

      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={contextWithoutDates}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('Select a date range to view cash flow chart')).toBeInTheDocument()
    })
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: null,
        loading: false,
        error: 'Failed to load chart data',
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('Failed to load chart data')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show no data message when data is null', () => {
      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('No cash flow data available for this period')).toBeInTheDocument()
    })

    it('should show no data message when periods array is empty', () => {
      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: { ...mockHistoryData, periods: [] },
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('No cash flow data available for this period')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    it('should render chart components', () => {
      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: mockHistoryData,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByTestId('responsive-container')).toBeInTheDocument()
      expect(screen.getByTestId('composed-chart')).toBeInTheDocument()
    })

    it('should render bars and line', () => {
      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: mockHistoryData,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      // Two bars (income, expenses) + one line (net)
      const bars = screen.getAllByTestId('bar')
      expect(bars.length).toBe(2) // Income and Expenses bars

      expect(screen.getByTestId('line')).toBeInTheDocument() // Net cash flow line
    })

    it('should render axis and legend', () => {
      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: mockHistoryData,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByTestId('x-axis')).toBeInTheDocument()
      expect(screen.getByTestId('y-axis')).toBeInTheDocument()
      expect(screen.getByTestId('legend')).toBeInTheDocument()
      expect(screen.getByTestId('tooltip')).toBeInTheDocument()
    })
  })

  describe('Context Integration', () => {
    it('should pass correct filters to useCashFlowHistory', () => {
      vi.mocked(useCashFlowHistory).mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowChartWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(useCashFlowHistory).toHaveBeenCalledWith(
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
})
