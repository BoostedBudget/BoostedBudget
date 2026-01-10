import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import type { NetWorthSummary, NetWorthHistory } from '../types'
// Issue #575: Use centralized registry mock to prevent mock.module() pollution
import '@/test/mocks/registry'

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
  timeframe: 'month' as const,
  setTimeframe: mockSetTimeframe,
  isEditing: false,
  setIsEditing: mockSetIsEditing,
}))

const mockRefetch = mock(() => {})

const defaultDataState = {
  summary: null,
  history: null,
  loading: false,
  error: null,
  refetch: mockRefetch,
}

const mockUseNetWorthData = mock(() => defaultDataState)

mock.module('../context/DashboardContext', () => ({
  useDashboardContext: mockUseDashboardContext,
}))

mock.module('../hooks/useNetWorthData', () => ({
  useNetWorthData: mockUseNetWorthData,
}))

import { NetWorthWidget } from './NetWorthWidget'

describe('NetWorthWidget', () => {
  const mockSummary: NetWorthSummary = {
    as_of_date: '2024-01-31',
    net_worth: 50000,
    total_assets: 75000,
    total_liabilities: 25000,
    by_category: {
      assets: {
        category: 'assets',
        total: 75000,
        account_count: 3,
        by_type: {},
      },
      liabilities: {
        category: 'liabilities',
        total: 25000,
        account_count: 2,
        by_type: {},
      },
    },
    period_comparison: {
      previous_net_worth: 45000,
      previous_assets: 70000,
      previous_liabilities: 25000,
      net_worth_change: 5000,
      net_worth_change_percent: 11.11,
    },
  }

  const mockHistory: NetWorthHistory = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    granularity: 'daily',
    snapshots: [
      { date: '2024-01-01', net_worth: 45000, total_assets: 70000, total_liabilities: 25000 },
      { date: '2024-01-31', net_worth: 50000, total_assets: 75000, total_liabilities: 25000 },
    ],
  }

  const setMockDataState = (state: Partial<typeof defaultDataState>) => {
    mockUseNetWorthData.mockReturnValue({ ...defaultDataState, ...state })
  }

  beforeEach(() => {
    mockUseDashboardContext.mockClear()
    mockUseNetWorthData.mockClear()
    mockRefetch.mockClear()
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      setMockDataState({ loading: true })

      render(<NetWorthWidget />)

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      setMockDataState({ error: new Error('Network error') })

      render(<NetWorthWidget />)

      expect(screen.getByText('Network error')).toBeInTheDocument()
    })

    it('should show error text in red', () => {
      setMockDataState({ error: new Error('API Error') })

      render(<NetWorthWidget />)

      const errorText = screen.getByText('API Error')
      expect(errorText).toHaveClass('text-red-500')
    })
  })

  describe('Empty State', () => {
    it('should show no data message when summary is null', () => {
      setMockDataState({})

      render(<NetWorthWidget />)

      expect(screen.getByText('No data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      setMockDataState({ summary: mockSummary, history: mockHistory })
    })

    it('should render net worth value', () => {
      render(<NetWorthWidget />)
      expect(screen.getByText(/\$50,000/)).toBeInTheDocument()
    })

    it('should display positive net worth with green color', () => {
      render(<NetWorthWidget />)
      const netWorthElement = screen.getByText(/\$50,000/)
      expect(netWorthElement).toHaveClass('text-green-600')
    })

    it('should display negative net worth with red color', () => {
      const negativeNetWorthSummary: NetWorthSummary = {
        ...mockSummary,
        net_worth: -10000,
        total_assets: 15000,
        total_liabilities: 25000,
      }
      setMockDataState({ summary: negativeNetWorthSummary, history: mockHistory })

      render(<NetWorthWidget />)

      const netWorthElement = screen.getByText(/-\$10,000/)
      expect(netWorthElement).toHaveClass('text-red-600')
    })

    it('should display assets and liabilities breakdown', () => {
      render(<NetWorthWidget />)

      expect(screen.getByText('Assets')).toBeInTheDocument()
      expect(screen.getByText('Liabilities')).toBeInTheDocument()
      expect(screen.getByText(/\$75,000/)).toBeInTheDocument()
      expect(screen.getByText(/\$25,000/)).toBeInTheDocument()
    })

    it('should display period comparison when available', () => {
      render(<NetWorthWidget />)

      expect(screen.getByText(/\$5,000/)).toBeInTheDocument()
      expect(screen.getByText(/11\.11%/)).toBeInTheDocument()
    })
  })

  describe('Sparkline Toggle', () => {
    beforeEach(() => {
      setMockDataState({ summary: mockSummary, history: mockHistory })
    })

    it('should render sparkline toggle button', () => {
      render(<NetWorthWidget />)
      expect(screen.getByText('Show Trend')).toBeInTheDocument()
    })

    it('should hide sparkline by default', () => {
      render(<NetWorthWidget />)
      expect(screen.queryByTestId('net-worth-sparkline')).not.toBeInTheDocument()
    })

    it('should show sparkline when toggle is clicked', () => {
      render(<NetWorthWidget />)

      const toggleButton = screen.getByText('Show Trend')
      fireEvent.click(toggleButton)

      expect(screen.getByText('Hide Trend')).toBeInTheDocument()
      expect(screen.getByTestId('net-worth-sparkline')).toBeInTheDocument()
    })

    it('should have user-friendly tooltip on toggle', () => {
      render(<NetWorthWidget />)

      const toggleButton = screen.getByText('Show Trend')
      expect(toggleButton.closest('button')).toHaveAttribute('title', expect.stringContaining('trend'))
    })
  })

  describe('Context Integration', () => {
    it('should use filters from dashboard context', () => {
      setMockDataState({ summary: mockSummary, history: mockHistory })

      render(<NetWorthWidget />)

      expect(mockUseNetWorthData).toHaveBeenCalledWith(
        expect.objectContaining({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      )
    })
  })
})
