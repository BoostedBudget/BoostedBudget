import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import type { BalanceSummary } from '../types'

const mockSetFilters = mock(() => {})
const mockSetTimeframe = mock(() => {})
const mockSetIsEditing = mock(() => {})
const mockUseDashboardContext = mock(() => ({
  filters: {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    accountTypes: undefined,
  },
  setFilters: mockSetFilters,
  timeframe: 'month',
  setTimeframe: mockSetTimeframe,
  isEditing: false,
  setIsEditing: mockSetIsEditing,
}))

const mockRefetch = mock(() => {})
const mockUseDashboardData = mock(() => ({
  summary: null,
  history: null,
  accountTypes: null,
  loading: true,
  error: null,
  refetch: mockRefetch,
}))

mock.module('../context/DashboardContext', () => ({
  useDashboardContext: mockUseDashboardContext,
}))

mock.module('../hooks/useDashboardData', () => ({
  useDashboardData: mockUseDashboardData,
}))

mock.module('../components/BalanceOverview', () => ({
  BalanceOverview: ({ summary }: { summary: unknown }) => (
    <div data-testid="balance-overview">
      Balance Overview: {JSON.stringify(summary)}
    </div>
  ),
}))

mock.module('./registry', () => ({
  registerWidget: mock(() => {}),
}))

import { BalanceOverviewWidget } from './BalanceOverviewWidget'

describe('BalanceOverviewWidget', () => {
  const mockSummary: BalanceSummary = {
    as_of_date: '2024-01-31',
    total_balance: 10000,
    by_type: {},
    period_comparison: {
      previous_balance: 9500,
      change_amount: 500,
      change_percent: 5.26,
    },
  }

  const defaultProps = {
    widgetId: 'test-1',
    onRemove: mock(() => {}),
  }

  const setMockDataState = (state: Partial<ReturnType<typeof mockUseDashboardData>>) => {
    mockUseDashboardData.mockReturnValue({
      summary: null,
      history: null,
      accountTypes: null,
      loading: false,
      error: null,
      refetch: mockRefetch,
      ...state,
    })
  }

  beforeEach(() => {
    mockUseDashboardContext.mockClear()
    mockUseDashboardData.mockClear()
    mockRefetch.mockClear()
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      setMockDataState({ loading: true })

      render(<BalanceOverviewWidget {...defaultProps} />)

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      setMockDataState({ error: new Error('Network error') })

      render(<BalanceOverviewWidget {...defaultProps} />)

      expect(screen.getByText('Network error')).toBeInTheDocument()
    })

    it('should show error text in red', () => {
      setMockDataState({ error: new Error('API Error') })

      render(<BalanceOverviewWidget {...defaultProps} />)

      const errorText = screen.getByText('API Error')
      expect(errorText).toHaveClass('text-red-500')
    })
  })

  describe('Empty State', () => {
    it('should show no data message when summary is null', () => {
      setMockDataState({})

      render(<BalanceOverviewWidget {...defaultProps} />)

      expect(screen.getByText('No data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      setMockDataState({ summary: mockSummary })
    })

    it('should render BalanceOverview component with data', () => {
      render(<BalanceOverviewWidget {...defaultProps} />)
      expect(screen.getByTestId('balance-overview')).toBeInTheDocument()
    })

    it('should pass summary to BalanceOverview component', () => {
      render(<BalanceOverviewWidget {...defaultProps} />)
      expect(screen.getByTestId('balance-overview')).toHaveTextContent('10000')
    })
  })

  describe('Context Integration', () => {
    it('should use filters from dashboard context', () => {
      setMockDataState({ summary: mockSummary })

      render(<BalanceOverviewWidget {...defaultProps} />)

      expect(mockUseDashboardData).toHaveBeenCalledWith(
        expect.objectContaining({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      )
    })
  })
})
