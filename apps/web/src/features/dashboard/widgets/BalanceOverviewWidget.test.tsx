/**
 * BalanceOverviewWidget Tests
 * Issue #575: Refactored to use centralized mocks
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import type { BalanceSummary } from '../types'

// Issue #575: Import centralized mock exports
import '@/test/mocks/registry'
import {
  mockUseDashboardContext,
  mockSetFilters,
  mockSetTimeframe,
  mockSetIsEditing,
} from '@/test/mocks/dashboard-context'
import {
  mockUseDashboardData,
  mockDashboardDataRefetch,
} from '@/test/mocks/dashboard-wrapper-hooks'

// Issue #575: Register useDashboardData mock inline (wrapper hooks are export-only)
// This must be BEFORE importing the component
mock.module('@/features/dashboard/hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))
mock.module('../hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))

// Child component mock is OK to keep inline (isolated to this test file)
mock.module('../components/BalanceOverview', () => ({
  BalanceOverview: ({ summary }: { summary: unknown }) => (
    <div data-testid="balance-overview">
      Balance Overview: {JSON.stringify(summary)}
    </div>
  ),
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

  // Default states for the centralized mocks
  const defaultContextState = {
    filters: {
      startDate: '2024-01-01',
      endDate: '2024-01-31',
      accountTypes: undefined,
    },
    setFilters: mockSetFilters,
    timeframe: 'this_month' as const,
    setTimeframe: mockSetTimeframe,
    isEditing: false,
    setIsEditing: mockSetIsEditing,
  }

  const defaultDataState = {
    summary: null,
    history: null,
    accountTypes: null,
    loading: false,
    error: null,
    refetch: mockDashboardDataRefetch,
  }

  const setMockDataState = (state: Partial<typeof defaultDataState>) => {
    mockUseDashboardData.mockReturnValue({ ...defaultDataState, ...state })
  }

  beforeEach(() => {
    mockUseDashboardContext.mockClear()
    mockUseDashboardData.mockClear()
    mockDashboardDataRefetch.mockClear()
    mockSetFilters.mockClear()
    mockSetTimeframe.mockClear()
    mockSetIsEditing.mockClear()
    mockUseDashboardContext.mockReturnValue(defaultContextState)
    mockUseDashboardData.mockReturnValue(defaultDataState)
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

    it('should show empty state when balance data is all zero with no accounts', () => {
      const emptyBalanceSummary: BalanceSummary = {
        as_of_date: '2024-01-31',
        total_balance: 0,
        by_type: {},
      }
      setMockDataState({ summary: emptyBalanceSummary })

      render(<BalanceOverviewWidget {...defaultProps} />)

      expect(screen.getByRole('status')).toBeInTheDocument()
      expect(screen.queryByTestId('balance-overview')).not.toBeInTheDocument()
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
