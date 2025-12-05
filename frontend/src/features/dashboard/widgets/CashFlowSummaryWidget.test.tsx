/**
 * Cash Flow Summary Widget Tests
 *
 * Tests the CashFlowSummaryWidget component including loading,
 * error, and success states with summary cards.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import React from 'react'

// Mock hooks and registry
vi.mock('../hooks/useCashFlowData', () => ({
  useCashFlowSummary: vi.fn(),
}))

vi.mock('./registry', () => ({
  registerWidget: vi.fn(),
}))

import { CashFlowSummaryWidget } from './CashFlowSummaryWidget'
import { useCashFlowSummary } from '../hooks/useCashFlowData'
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

describe('CashFlowSummaryWidget', () => {
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

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Missing Date Range', () => {
    it('should show prompt when no start date', () => {
      const contextWithoutDates = {
        ...mockContextValue,
        filters: { startDate: '', endDate: '' },
      }

      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={contextWithoutDates}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('Select a date range to view cash flow')).toBeInTheDocument()
    })
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: null,
        loading: false,
        error: 'Network error',
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('Network error')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show no data message when data is null', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('No cash flow data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    it('should render three summary cards', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: mockSummaryData,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('Income')).toBeInTheDocument()
      expect(screen.getByText('Expenses')).toBeInTheDocument()
      expect(screen.getByText('Net Cash Flow')).toBeInTheDocument()
    })

    it('should display formatted currency values', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: mockSummaryData,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      // Check for formatted currency (e.g., "$5,000")
      expect(screen.getByText('$5,000')).toBeInTheDocument() // Income
      expect(screen.getByText('$3,000')).toBeInTheDocument() // Expenses
      expect(screen.getByText('$2,000')).toBeInTheDocument() // Net
    })

    it('should display transaction counts', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: mockSummaryData,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(screen.getByText('10 transactions')).toBeInTheDocument()
      expect(screen.getByText('25 transactions')).toBeInTheDocument()
    })

    it('should display period comparison percentages', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: mockSummaryData,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      // Check for formatted percentages
      expect(screen.getByText('+11.1%')).toBeInTheDocument() // Income change
      expect(screen.getByText('+7.1%')).toBeInTheDocument() // Expense change
      expect(screen.getByText('+17.6%')).toBeInTheDocument() // Net change
    })

    it('should show vs prior period text', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: mockSummaryData,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      // Should appear for each card with comparison
      const priorPeriodTexts = screen.getAllByText('vs prior period')
      expect(priorPeriodTexts.length).toBe(3)
    })
  })

  describe('Context Integration', () => {
    it('should pass correct filters to useCashFlowSummary', () => {
      vi.mocked(useCashFlowSummary).mockReturnValue({
        data: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(
        <TestWrapper contextValue={mockContextValue}>
          <CashFlowSummaryWidget widgetId="test-1" onRemove={vi.fn()} />
        </TestWrapper>
      )

      expect(useCashFlowSummary).toHaveBeenCalledWith(
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
