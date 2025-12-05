/**
 * Balance Overview Widget Tests
 *
 * Tests the BalanceOverviewWidget component including loading,
 * error, and success states.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'

// Mock dependencies before importing the component
vi.mock('../context/DashboardContext', () => ({
  useDashboardContext: vi.fn(),
}))

vi.mock('../hooks/useDashboardData', () => ({
  useDashboardData: vi.fn(),
}))

vi.mock('../components/BalanceOverview', () => ({
  BalanceOverview: ({ summary }: { summary: unknown }) => (
    <div data-testid="balance-overview">
      Balance Overview: {JSON.stringify(summary)}
    </div>
  ),
}))

vi.mock('./registry', () => ({
  registerWidget: vi.fn(),
}))

import { BalanceOverviewWidget } from './BalanceOverviewWidget'
import { useDashboardContext } from '../context/DashboardContext'
import { useDashboardData } from '../hooks/useDashboardData'
import type { BalanceSummary } from '../types'

describe('BalanceOverviewWidget', () => {
  const mockFilters = {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    accountTypes: undefined,
  }

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

  beforeEach(() => {
    vi.clearAllMocks()

    // Default mock for context
    vi.mocked(useDashboardContext).mockReturnValue({
      filters: mockFilters,
      setFilters: vi.fn(),
      timeframe: 'month',
      setTimeframe: vi.fn(),
      isEditing: false,
      setIsEditing: vi.fn(),
    })
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceOverviewWidget widgetId="test-1" onRemove={vi.fn()} />)

      // Check for spinner animation class
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: new Error('Network error'),
        refetch: vi.fn(),
      })

      render(<BalanceOverviewWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByText('Network error')).toBeInTheDocument()
    })

    it('should show error text in red', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: new Error('API Error'),
        refetch: vi.fn(),
      })

      render(<BalanceOverviewWidget widgetId="test-1" onRemove={vi.fn()} />)

      const errorText = screen.getByText('API Error')
      expect(errorText).toHaveClass('text-red-500')
    })
  })

  describe('Empty State', () => {
    it('should show no data message when summary is null', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceOverviewWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByText('No data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    it('should render BalanceOverview component with data', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceOverviewWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByTestId('balance-overview')).toBeInTheDocument()
    })

    it('should pass summary to BalanceOverview component', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceOverviewWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByTestId('balance-overview')).toHaveTextContent('10000')
    })
  })

  describe('Context Integration', () => {
    it('should use filters from dashboard context', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceOverviewWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(useDashboardData).toHaveBeenCalledWith(
        expect.objectContaining({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      )
    })
  })
})
