import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'

const mockRefetch = mock(() => {})
const mockUseCashFlowSummary = mock(() => ({
  data: null,
  loading: true,
  error: null,
  refetch: mockRefetch,
}))

mock.module('../hooks/useCashFlowData', () => ({
  useCashFlowSummary: mockUseCashFlowSummary,
}))

mock.module('./registry', () => ({
  registerWidget: mock(() => {}),
}))

import { CashFlowSummaryWidget } from './CashFlowSummaryWidget'
import { DashboardContext, DashboardContextValue } from '../context/DashboardContext'

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

describe('CashFlowSummaryWidget', () => {
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

  const mockSummaryData = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    income: 5000,
    expenses: 3000,
    net_cash_flow: 2000,
    income_count: 10,
    expense_count: 25,
    period_comparison: {
      previous_income: 4500,
      previous_expenses: 2800,
      previous_net_cash_flow: 1700,
      income_change_amount: 500,
      income_change_percent: 11.11,
      expense_change_amount: 200,
      expense_change_percent: 7.14,
      net_change_amount: 300,
      net_change_percent: 17.65,
    },
  }

  const defaultProps = {
    widgetId: 'test-1',
    onRemove: mock(() => {}),
  }

  const setMockCashFlowState = (state: Partial<ReturnType<typeof mockUseCashFlowSummary>>) => {
    mockUseCashFlowSummary.mockReturnValue({
      data: null,
      loading: false,
      error: null,
      refetch: mockRefetch,
      ...state,
    })
  }

  beforeEach(() => {
    mockUseCashFlowSummary.mockClear()
    mockRefetch.mockClear()
  })

  describe('Missing Date Range', () => {
    it('should show prompt when no start date', () => {
      const contextWithoutDates = {
        ...mockContextValue,
        filters: { startDate: '', endDate: '' },
      }

      setMockCashFlowState({})

      render(
        <TestWrapper contextValue={contextWithoutDates}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      expect(screen.getByText('Select a date range to view cash flow')).toBeInTheDocument()
    })
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      setMockCashFlowState({ loading: true })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      setMockCashFlowState({ error: 'Network error' })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      expect(screen.getByText('Network error')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show no data message when data is null', () => {
      setMockCashFlowState({})

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      expect(screen.getByText('No cash flow data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      setMockCashFlowState({ data: mockSummaryData })
    })

    it('should render three summary cards', () => {
      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      expect(screen.getByText('Income')).toBeInTheDocument()
      expect(screen.getByText('Expenses')).toBeInTheDocument()
      expect(screen.getByText('Net Cash Flow')).toBeInTheDocument()
    })

    it('should display formatted currency values', () => {
      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      expect(screen.getByText('$5,000')).toBeInTheDocument()
      expect(screen.getByText('$3,000')).toBeInTheDocument()
      expect(screen.getByText('$2,000')).toBeInTheDocument()
    })

    it('should display transaction counts', () => {
      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      expect(screen.getByText('10 transactions')).toBeInTheDocument()
      expect(screen.getByText('25 transactions')).toBeInTheDocument()
    })

    it('should display period comparison percentages', () => {
      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      expect(screen.getByText('+11.1%')).toBeInTheDocument()
      expect(screen.getByText('+7.1%')).toBeInTheDocument()
      expect(screen.getByText('+17.6%')).toBeInTheDocument()
    })

    it('should show vs prior period text', () => {
      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      const priorPeriodTexts = screen.getAllByText('vs prior period')
      expect(priorPeriodTexts.length).toBe(3)
    })
  })

  describe('Context Integration', () => {
    it('should pass correct filters to useCashFlowSummary', () => {
      setMockCashFlowState({})

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget {...defaultProps} />
        </TestWrapper>
      )

      expect(mockUseCashFlowSummary).toHaveBeenCalledWith(
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
