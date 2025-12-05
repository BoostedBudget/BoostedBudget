/**
 * Balance History Widget Tests
 *
 * Tests the BalanceHistoryWidget component including loading,
 * error, empty states, and breakdown toggle.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

// Mock dependencies before importing the component
vi.mock('../context/DashboardContext', () => ({
  useDashboardContext: vi.fn(),
}))

vi.mock('../hooks/useDashboardData', () => ({
  useDashboardData: vi.fn(),
}))

vi.mock('../components/BalanceHistoryChart', () => ({
  BalanceHistoryChart: ({ history, showTypeBreakdown }: {
    history: unknown
    showTypeBreakdown: boolean
  }) => (
    <div data-testid="balance-history-chart">
      History Chart - Breakdown: {showTypeBreakdown ? 'Yes' : 'No'}
    </div>
  ),
}))

vi.mock('./registry', () => ({
  registerWidget: vi.fn(),
}))

import { BalanceHistoryWidget } from './BalanceHistoryWidget'
import { useDashboardContext } from '../context/DashboardContext'
import { useDashboardData } from '../hooks/useDashboardData'

describe('BalanceHistoryWidget', () => {
  const mockFilters = {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    accountTypes: undefined,
  }

  const mockHistory = [
    { date: '2024-01-01', balance: 10000 },
    { date: '2024-01-15', balance: 10500 },
    { date: '2024-01-31', balance: 11000 },
  ]

  beforeEach(() => {
    vi.clearAllMocks()

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
        history: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceHistoryWidget widgetId="test-1" onRemove={vi.fn()} />)

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        history: null,
        loading: false,
        error: new Error('Failed to load history'),
        refetch: vi.fn(),
      })

      render(<BalanceHistoryWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByText('Failed to load history')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show no data message when history is null', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        history: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceHistoryWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByText('No history data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    it('should render BalanceHistoryChart with data', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceHistoryWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByTestId('balance-history-chart')).toBeInTheDocument()
    })
  })

  describe('Breakdown Toggle', () => {
    it('should show "Breakdown" button initially', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceHistoryWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByText('Breakdown')).toBeInTheDocument()
    })

    it('should toggle breakdown when button clicked', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceHistoryWidget widgetId="test-1" onRemove={vi.fn()} />)

      // Check initial state - breakdown is off
      expect(screen.getByTestId('balance-history-chart')).toHaveTextContent('Breakdown: No')

      fireEvent.click(screen.getByText('Breakdown'))

      // After toggle - breakdown is on
      expect(screen.getByText('Total')).toBeInTheDocument()
      expect(screen.getByTestId('balance-history-chart')).toHaveTextContent('Breakdown: Yes')
    })

    it('should toggle back to total view', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<BalanceHistoryWidget widgetId="test-1" onRemove={vi.fn()} />)

      // Click to show breakdown
      fireEvent.click(screen.getByText('Breakdown'))
      expect(screen.getByText('Total')).toBeInTheDocument()

      // Click to go back to total
      fireEvent.click(screen.getByText('Total'))
      expect(screen.getByText('Breakdown')).toBeInTheDocument()
    })
  })
})
