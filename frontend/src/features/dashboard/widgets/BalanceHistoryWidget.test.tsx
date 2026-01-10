import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
// Issue #575: Use centralized registry mock to prevent mock.module() pollution
import '@/test/mocks/registry'

const mockUseDashboardContext = mock(() => ({
  filters: {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    accountTypes: undefined,
  },
  setFilters: mock(() => {}),
  timeframe: 'month',
  setTimeframe: mock(() => {}),
  isEditing: false,
  setIsEditing: mock(() => {}),
}))

const mockUseDashboardData = mock(() => ({
  summary: null,
  history: null,
  accountTypes: null,
  loading: false,
  error: null,
  refetch: mock(() => {}),
}))

mock.module('../context/DashboardContext', () => ({
  useDashboardContext: mockUseDashboardContext,
}))

mock.module('../hooks/useDashboardData', () => ({
  useDashboardData: mockUseDashboardData,
}))

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

  const mockDashboardData = (overrides = {}) => ({
    summary: null,
    history: null,
    accountTypes: null,
    loading: false,
    error: null,
    refetch: mock(() => {}),
    ...overrides,
  })

  beforeEach(() => {
    mockUseDashboardContext.mockClear()
    mockUseDashboardData.mockClear()
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      mockUseDashboardData.mockReturnValue(mockDashboardData({ loading: true }))
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(document.querySelector('.animate-spin')).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      mockUseDashboardData.mockReturnValue(
        mockDashboardData({ error: new Error('Failed to load history') })
      )
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(screen.getByText('Failed to load history')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show no data message when history is null', () => {
      mockUseDashboardData.mockReturnValue(mockDashboardData())
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(screen.getByText('No history data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    it('should render BalanceHistoryChart with data', () => {
      mockUseDashboardData.mockReturnValue(mockDashboardData({ history: mockHistory }))
      render(<BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />)
      expect(screen.getByTestId('balance-history-chart')).toBeInTheDocument()
    })

    it('should use h-full for proper height propagation', () => {
      mockUseDashboardData.mockReturnValue(mockDashboardData({ history: mockHistory }))
      const { container } = render(
        <BalanceHistoryWidget widgetId="test-1" onRemove={mock(() => {})} />
      )
      expect((container.firstChild as HTMLElement)).toHaveClass('h-full')
    })
  })

  describe('Breakdown Toggle', () => {
    beforeEach(() => {
      mockUseDashboardData.mockReturnValue(mockDashboardData({ history: mockHistory }))
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
