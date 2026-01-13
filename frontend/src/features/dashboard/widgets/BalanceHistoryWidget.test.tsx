/**
 * BalanceHistoryWidget Tests
 * Issue #575: Refactored to use centralized mocks
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'

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
mock.module('../components/BalanceHistoryChart', () => ({
  BalanceHistoryChart: ({
    showTypeBreakdown,
  }: {
    history: unknown
    showTypeBreakdown: boolean
  }) => (
    <div data-testid="balance-history-chart">
      History Chart - Breakdown: {showTypeBreakdown ? 'Yes' : 'No'}
    </div>
  ),
}))

import { BalanceHistoryWidget } from './BalanceHistoryWidget'
import type { BalanceHistory } from '../types'

describe('BalanceHistoryWidget', () => {
  const mockHistory: BalanceHistory = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    granularity: 'daily',
    snapshots: [
      { date: '2024-01-01', balance: 10000 },
      { date: '2024-01-15', balance: 10500 },
      { date: '2024-01-31', balance: 11000 },
    ],
  }

  // Default states for the centralized mocks
  const defaultContextState = {
    filters: {
      startDate: '2024-01-01',
      endDate: '2024-01-31',
      accountTypes: undefined,
    },
    setFilters: mockSetFilters,
    timeframe: 'month' as const,
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
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(document.querySelector('.animate-spin')).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      setMockDataState({ error: new Error('Failed to load history') })
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(screen.getByText('Failed to load history')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show no data message when history is null', () => {
      setMockDataState({})
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(screen.getByText('No history data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    it('should render BalanceHistoryChart with data', () => {
      setMockDataState({ history: mockHistory })
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(screen.getByTestId('balance-history-chart')).toBeInTheDocument()
    })

    it('should use h-full for proper height propagation', () => {
      setMockDataState({ history: mockHistory })
      const { container } = render(
        <BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />
      )
      expect(container.firstChild as HTMLElement).toHaveClass('h-full')
    })
  })

  describe('Breakdown Toggle', () => {
    beforeEach(() => {
      setMockDataState({ history: mockHistory })
    })

    it('should show "Breakdown" button initially', () => {
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(screen.getByText('Breakdown')).toBeInTheDocument()
    })

    it('should toggle breakdown when button clicked', () => {
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(screen.getByTestId('balance-history-chart')).toHaveTextContent('Breakdown: No')
      fireEvent.click(screen.getByText('Breakdown'))
      expect(screen.getByText('Total')).toBeInTheDocument()
      expect(screen.getByTestId('balance-history-chart')).toHaveTextContent('Breakdown: Yes')
    })

    it('should toggle back to total view', () => {
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      fireEvent.click(screen.getByText('Breakdown'))
      expect(screen.getByText('Total')).toBeInTheDocument()
      fireEvent.click(screen.getByText('Total'))
      expect(screen.getByText('Breakdown')).toBeInTheDocument()
    })
  })
})
