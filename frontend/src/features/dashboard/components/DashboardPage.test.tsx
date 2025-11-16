/**
 * DashboardPage Component Tests
 *
 * Tests the main dashboard view with balance overview, charts,
 * filters, and state management.
 *
 * Issue #353: Balance Tracking Dashboard
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DashboardPage } from './DashboardPage'
import { useDashboardData } from '../hooks/useDashboardData'
import type {
  BalanceSummary,
  BalanceHistory,
  AccountTypesResponse,
} from '../types'

// Mock the useDashboardData hook
vi.mock('../hooks/useDashboardData')

// Mock child components for simpler testing
vi.mock('./BalanceOverview', () => ({
  BalanceOverview: ({ summary }: { summary: BalanceSummary }) => (
    <div data-testid="balance-overview">
      Balance Overview: ${summary.total_balance}
    </div>
  ),
}))

vi.mock('./BalanceHistoryChart', () => ({
  BalanceHistoryChart: ({
    history,
    showTypeBreakdown,
  }: {
    history: BalanceHistory
    showTypeBreakdown: boolean
  }) => (
    <div data-testid="balance-history-chart">
      Chart: {history.snapshots.length} snapshots
      {showTypeBreakdown ? ' (breakdown)' : ' (total)'}
    </div>
  ),
}))

vi.mock('./AccountTypeFilter', () => ({
  AccountTypeFilter: ({
    accountTypes,
    selectedTypes,
    onChange,
  }: {
    accountTypes: AccountTypesResponse
    selectedTypes: string[]
    onChange: (types: string[]) => void
  }) => (
    <div data-testid="account-type-filter">
      <button onClick={() => onChange(['depository'])}>
        Filter: {accountTypes.types.length} types
      </button>
      <span>Selected: {selectedTypes.length}</span>
    </div>
  ),
}))

vi.mock('./TimeframeSelector', () => ({
  TimeframeSelector: ({
    onRangeChange,
  }: {
    onRangeChange: (start: string, end: string) => void
  }) => (
    <div data-testid="timeframe-selector">
      <button onClick={() => onRangeChange('2025-10-01', '2025-11-15')}>
        Change Timeframe
      </button>
    </div>
  ),
}))

vi.mock('./AccountBalanceCards', () => ({
  AccountBalanceCards: ({ summary }: { summary: BalanceSummary }) => (
    <div data-testid="account-balance-cards">
      Accounts: {Object.keys(summary.by_type).length} types
    </div>
  ),
}))

describe('DashboardPage', () => {
  const mockSummary: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 15000.50,
    by_type: {
      depository: {
        balance: 5000.00,
        account_count: 2,
        accounts: [
          { id: 'acc-1', name: 'Checking', balance: 3000.00 },
          { id: 'acc-2', name: 'Savings', balance: 2000.00 },
        ],
      },
      credit: {
        balance: -2000.00,
        account_count: 1,
        accounts: [
          { id: 'acc-3', name: 'Credit Card', balance: -2000.00 },
        ],
      },
    },
    period_comparison: {
      previous_balance: 14000.00,
      change_amount: 1000.50,
      change_percent: 7.15,
    },
  }

  const mockHistory: BalanceHistory = {
    start_date: '2025-10-16',
    end_date: '2025-11-15',
    granularity: 'daily',
    snapshots: [
      {
        date: '2025-10-16',
        balance: 14000.00,
        by_type: { depository: 4500.00, credit: -1500.00 },
      },
      {
        date: '2025-11-15',
        balance: 15000.50,
        by_type: { depository: 5000.00, credit: -2000.00 },
      },
    ],
  }

  const mockAccountTypes: AccountTypesResponse = {
    types: [
      { key: 'depository', label: 'Checking & Savings', count: 2, total_balance: 5000.00 },
      { key: 'credit', label: 'Credit Cards', count: 1, total_balance: -2000.00 },
    ],
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading=true', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('Loading dashboard...')).toBeInTheDocument()

      // Check for spinner element
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })

    it('should display "Loading dashboard..." text', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('Loading dashboard...')).toBeInTheDocument()
    })

    it('should not render dashboard content while loading', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.queryByTestId('balance-overview')).not.toBeInTheDocument()
      expect(screen.queryByTestId('balance-history-chart')).not.toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error exists', () => {
      // Arrange
      const error = new Error('Failed to fetch dashboard data')
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('Failed to Load Dashboard')).toBeInTheDocument()
      expect(screen.getByText('Failed to fetch dashboard data')).toBeInTheDocument()
    })

    it('should display error details from error.message', () => {
      // Arrange
      const error = new Error('Network timeout error')
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('Network timeout error')).toBeInTheDocument()
    })

    it('should show "Try Again" button on error', () => {
      // Arrange
      const error = new Error('API error')
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      const retryButton = screen.getByRole('button', { name: 'Try Again' })
      expect(retryButton).toBeInTheDocument()
    })

    it('should call refetch when "Try Again" clicked', async () => {
      // Arrange
      const user = userEvent.setup()
      const mockRefetch = vi.fn()
      const error = new Error('API error')

      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error,
        refetch: mockRefetch,
      })

      // Act
      render(<DashboardPage />)

      const retryButton = screen.getByRole('button', { name: 'Try Again' })
      await user.click(retryButton)

      // Assert
      expect(mockRefetch).toHaveBeenCalledTimes(1)
    })

    it('should not render dashboard content on error', () => {
      // Arrange
      const error = new Error('API error')
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.queryByTestId('balance-overview')).not.toBeInTheDocument()
      expect(screen.queryByTestId('balance-history-chart')).not.toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show empty state when summary is null', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('No Data Available')).toBeInTheDocument()
      expect(screen.getByText('Connect your accounts to start tracking your balance history.')).toBeInTheDocument()
    })

    it('should show empty state when history is null', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('No Data Available')).toBeInTheDocument()
    })

    it('should show empty state when accountTypes is null', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('No Data Available')).toBeInTheDocument()
    })

    it('should display "No Data Available" message', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('No Data Available')).toBeInTheDocument()
    })

    it('should show connect accounts prompt', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('Connect your accounts to start tracking your balance history.')).toBeInTheDocument()
    })

    it('should not render dashboard content when empty', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.queryByTestId('balance-overview')).not.toBeInTheDocument()
      expect(screen.queryByTestId('balance-history-chart')).not.toBeInTheDocument()
    })
  })

  describe('Successful Render', () => {
    beforeEach(() => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
    })

    it('should render dashboard header with title', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should display "Track your balance across all accounts" subtitle', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('Track your balance across all accounts')).toBeInTheDocument()
    })

    it('should render BalanceOverview component', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByTestId('balance-overview')).toBeInTheDocument()
      expect(screen.getByText(/Balance Overview: \$15000.5/)).toBeInTheDocument()
    })

    it('should render BalanceHistoryChart component', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByTestId('balance-history-chart')).toBeInTheDocument()
      expect(screen.getByText(/Chart: 2 snapshots/)).toBeInTheDocument()
    })

    it('should render AccountTypeFilter component', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByTestId('account-type-filter')).toBeInTheDocument()
    })

    it('should render TimeframeSelector component', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByTestId('timeframe-selector')).toBeInTheDocument()
    })

    it('should render AccountBalanceCards component', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByTestId('account-balance-cards')).toBeInTheDocument()
      expect(screen.getByText(/Accounts: 2 types/)).toBeInTheDocument()
    })

    it('should render "Accounts" section heading', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText('Accounts')).toBeInTheDocument()
    })
  })

  describe('Filter Management', () => {
    beforeEach(() => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
    })

    it('should initialize with default filters (30 days ago to today)', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      // useDashboardData should be called with filters
      const calls = vi.mocked(useDashboardData).mock.calls
      expect(calls.length).toBeGreaterThan(0)

      const filters = calls[0][0]
      expect(filters).toBeDefined()
      expect(filters?.startDate).toBeDefined()
      expect(filters?.endDate).toBeDefined()
      expect(filters?.accountTypes).toEqual([])
    })

    it('should update filters when account types change', async () => {
      // Arrange
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Act - Click filter button (which calls onChange with ['depository'])
      const filterButton = screen.getByText(/Filter: 2 types/)
      await user.click(filterButton)

      // Assert
      await waitFor(() => {
        const filterElement = screen.getByText(/Selected: 1/)
        expect(filterElement).toBeInTheDocument()
      })
    })

    it('should update filters when timeframe changes', async () => {
      // Arrange
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Act - Click timeframe button
      const timeframeButton = screen.getByText('Change Timeframe')
      await user.click(timeframeButton)

      // Assert - Hook should be called with updated filters
      await waitFor(() => {
        const calls = vi.mocked(useDashboardData).mock.calls
        const lastCall = calls[calls.length - 1]
        expect(lastCall[0]?.startDate).toBe('2025-10-01')
        expect(lastCall[0]?.endDate).toBe('2025-11-15')
      })
    })

    it('should pass filters to useDashboardData hook', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(useDashboardData).toHaveBeenCalled()
      const filters = vi.mocked(useDashboardData).mock.calls[0][0]
      expect(filters).toHaveProperty('startDate')
      expect(filters).toHaveProperty('endDate')
      expect(filters).toHaveProperty('accountTypes')
    })
  })

  describe('Chart Breakdown Toggle', () => {
    beforeEach(() => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
    })

    it('should initialize with showTypeBreakdown=false', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText(/Chart: 2 snapshots \(total\)/)).toBeInTheDocument()
    })

    it('should show "Show Breakdown" button when breakdown disabled', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      const button = screen.getByRole('button', { name: 'Show Breakdown' })
      expect(button).toBeInTheDocument()
    })

    it('should toggle showTypeBreakdown when button clicked', async () => {
      // Arrange
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Initial state - total only
      expect(screen.getByText(/\(total\)/)).toBeInTheDocument()

      // Act - Click toggle
      const toggleButton = screen.getByRole('button', { name: 'Show Breakdown' })
      await user.click(toggleButton)

      // Assert - Now showing breakdown
      await waitFor(() => {
        expect(screen.getByText(/\(breakdown\)/)).toBeInTheDocument()
      })
    })

    it('should show "Total Only" button when breakdown enabled', async () => {
      // Arrange
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Act - Enable breakdown
      const toggleButton = screen.getByRole('button', { name: 'Show Breakdown' })
      await user.click(toggleButton)

      // Assert - Button text changes
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Total Only' })).toBeInTheDocument()
      })
    })

    it('should pass showTypeBreakdown to BalanceHistoryChart', async () => {
      // Arrange
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Initial - false
      expect(screen.getByText(/\(total\)/)).toBeInTheDocument()

      // Act - Toggle to true
      const toggleButton = screen.getByRole('button', { name: 'Show Breakdown' })
      await user.click(toggleButton)

      // Assert
      await waitFor(() => {
        expect(screen.getByText(/\(breakdown\)/)).toBeInTheDocument()
      })

      // Act - Toggle back to false
      const totalButton = screen.getByRole('button', { name: 'Total Only' })
      await user.click(totalButton)

      // Assert
      await waitFor(() => {
        expect(screen.getByText(/\(total\)/)).toBeInTheDocument()
      })
    })
  })

  describe('Layout', () => {
    beforeEach(() => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
    })

    it('should use responsive grid layout (1 col mobile, 3 col desktop)', () => {
      // Act
      render(<DashboardPage />)

      // Assert - Check for grid container
      const gridContainer = document.querySelector('.grid.grid-cols-1.lg\\:grid-cols-3')
      expect(gridContainer).toBeInTheDocument()
    })

    it('should place overview and chart in left column (2 cols)', () => {
      // Act
      render(<DashboardPage />)

      // Assert - Check for 2-column span
      const leftColumn = document.querySelector('.lg\\:col-span-2')
      expect(leftColumn).toBeInTheDocument()

      // Overview and chart should be in this column
      const overview = screen.getByTestId('balance-overview')
      const chart = screen.getByTestId('balance-history-chart')
      expect(leftColumn).toContainElement(overview)
      expect(leftColumn).toContainElement(chart)
    })

    it('should render account cards below chart', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      const accountsHeading = screen.getByText('Accounts')
      const accountCards = screen.getByTestId('account-balance-cards')

      // Both should exist
      expect(accountsHeading).toBeInTheDocument()
      expect(accountCards).toBeInTheDocument()

      // Cards should be in left column
      const leftColumn = document.querySelector('.lg\\:col-span-2')
      expect(leftColumn).toContainElement(accountCards)
    })
  })

  describe('Data Passing', () => {
    beforeEach(() => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
    })

    it('should pass summary to BalanceOverview', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      const overview = screen.getByTestId('balance-overview')
      expect(overview).toHaveTextContent('15000.5')
    })

    it('should pass history to BalanceHistoryChart', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      const chart = screen.getByTestId('balance-history-chart')
      expect(chart).toHaveTextContent('2 snapshots')
    })

    it('should pass accountTypes to AccountTypeFilter', () => {
      // Act
      render(<DashboardPage />)

      // Assert
      const filter = screen.getByTestId('account-type-filter')
      expect(filter).toHaveTextContent('2 types')
    })

    it('should pass selected account types to filter', () => {
      // Act
      render(<DashboardPage />)

      // Assert - Initially empty
      const filter = screen.getByTestId('account-type-filter')
      expect(filter).toHaveTextContent('Selected: 0')
    })

    it('should pass onRangeChange callback to TimeframeSelector', async () => {
      // Arrange
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Act - Trigger callback
      const button = screen.getByText('Change Timeframe')
      await user.click(button)

      // Assert - Hook called with new dates
      await waitFor(() => {
        const calls = vi.mocked(useDashboardData).mock.calls
        const lastCall = calls[calls.length - 1]
        expect(lastCall[0]?.startDate).toBe('2025-10-01')
      })
    })
  })

  describe('Edge Cases', () => {
    it('should handle null summary gracefully', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert - Shows empty state
      expect(screen.getByText('No Data Available')).toBeInTheDocument()
    })

    it('should handle null history gracefully', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert - Shows empty state
      expect(screen.getByText('No Data Available')).toBeInTheDocument()
    })

    it('should handle null accountTypes gracefully', () => {
      // Arrange
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert - Shows empty state
      expect(screen.getByText('No Data Available')).toBeInTheDocument()
    })
  })
})
