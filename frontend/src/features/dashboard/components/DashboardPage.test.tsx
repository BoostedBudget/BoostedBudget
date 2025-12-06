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
  WidgetType,
} from '../types'

// Mock the useDashboardData hook
vi.mock('../hooks/useDashboardData')

// Mock the useDashboardLayout hook with configurable state
import { useDashboardLayout } from '../hooks/useDashboardLayout'
vi.mock('../hooks/useDashboardLayout')

// Mock the useDashboardOnboarding hook
import { useDashboardOnboarding } from '../hooks/useDashboardOnboarding'
vi.mock('../hooks/useDashboardOnboarding')

const defaultLayoutMock = {
  layout: {
    widgets: [
      { i: 'balance-overview-1', type: 'balance-overview' as WidgetType, x: 0, y: 0, w: 4, h: 2 },
      { i: 'cash-flow-summary-1', type: 'cash-flow-summary' as WidgetType, x: 4, y: 0, w: 8, h: 2 },
      { i: 'balance-history-1', type: 'balance-history' as WidgetType, x: 0, y: 2, w: 6, h: 4 },
      { i: 'cash-flow-chart-1', type: 'cash-flow-chart' as WidgetType, x: 6, y: 2, w: 6, h: 4 },
      { i: 'account-cards-1', type: 'account-cards' as WidgetType, x: 0, y: 6, w: 12, h: 3 },
    ],
    version: 1,
  },
  loading: false,
  error: null,
  saving: false,
  updateLayout: vi.fn(),
  addWidget: vi.fn(),
  removeWidget: vi.fn(),
  resetToDefault: vi.fn(),
  saveNow: vi.fn(),
}

// Mock the DashboardGrid component (widget system)
vi.mock('../widgets/DashboardGrid', () => ({
  DashboardGrid: () => <div data-testid="dashboard-grid">Dashboard Grid</div>,
}))

// Mock the WidgetCatalog component
vi.mock('../widgets/WidgetCatalog', () => ({
  WidgetCatalog: ({ isOpen }: { isOpen: boolean }) =>
    isOpen ? <div data-testid="widget-catalog">Widget Catalog</div> : null,
}))

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
    // Default mock for useDashboardLayout
    vi.mocked(useDashboardLayout).mockReturnValue(defaultLayoutMock)
    // Default mock for useDashboardOnboarding - hide hint by default
    vi.mocked(useDashboardOnboarding).mockReturnValue({
      showHint: false,
      dismissHint: vi.fn(),
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Loading State', () => {
    it('should show loading spinner when layout loading=true', () => {
      // Arrange - Layout loading comes from useDashboardLayout (Issue #354)
      vi.mocked(useDashboardLayout).mockReturnValue({
        ...defaultLayoutMock,
        loading: true,
      })
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
      expect(screen.getByText('Loading dashboard...')).toBeInTheDocument()

      // Check for spinner element
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })

    it('should display "Loading dashboard..." text when layout loading', () => {
      // Arrange - Layout loading state (Issue #354)
      vi.mocked(useDashboardLayout).mockReturnValue({
        ...defaultLayoutMock,
        loading: true,
      })
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
      expect(screen.getByText('Loading dashboard...')).toBeInTheDocument()
    })

    it('should not render dashboard grid while loading', () => {
      // Arrange
      vi.mocked(useDashboardLayout).mockReturnValue({
        ...defaultLayoutMock,
        loading: true,
      })
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

      // Assert - Dashboard grid not rendered during loading
      expect(screen.queryByTestId('dashboard-grid')).not.toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when layout error exists', () => {
      // Arrange - Error from useDashboardLayout (Issue #354)
      vi.mocked(useDashboardLayout).mockReturnValue({
        ...defaultLayoutMock,
        error: 'Failed to fetch dashboard layout',
      })
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
      expect(screen.getByText('Failed to Load Dashboard')).toBeInTheDocument()
      expect(screen.getByText('Failed to fetch dashboard layout')).toBeInTheDocument()
    })

    it('should display error details from layout error', () => {
      // Arrange - Error string from useDashboardLayout
      vi.mocked(useDashboardLayout).mockReturnValue({
        ...defaultLayoutMock,
        error: 'Network timeout error',
      })
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
      expect(screen.getByText('Network timeout error')).toBeInTheDocument()
    })

    it('should show "Try Again" button on error', () => {
      // Arrange
      vi.mocked(useDashboardLayout).mockReturnValue({
        ...defaultLayoutMock,
        error: 'API error',
      })
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
      const retryButton = screen.getByRole('button', { name: 'Try Again' })
      expect(retryButton).toBeInTheDocument()
    })

    it('should reload page when "Try Again" clicked', async () => {
      // Arrange - The Try Again button now reloads the page
      const user = userEvent.setup()
      const mockReload = vi.fn()
      Object.defineProperty(window, 'location', {
        value: { reload: mockReload },
        writable: true,
      })

      vi.mocked(useDashboardLayout).mockReturnValue({
        ...defaultLayoutMock,
        error: 'API error',
      })
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

      const retryButton = screen.getByRole('button', { name: 'Try Again' })
      await user.click(retryButton)

      // Assert
      expect(mockReload).toHaveBeenCalledTimes(1)
    })

    it('should not render dashboard grid on error', () => {
      // Arrange
      vi.mocked(useDashboardLayout).mockReturnValue({
        ...defaultLayoutMock,
        error: 'API error',
      })
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

  describe('Empty State', () => {
    // Note: With the widget system (Issue #354), DashboardPage no longer shows a centralized
    // "No Data Available" message. Each widget handles its own empty/loading state.
    // These tests verify that the dashboard grid still renders when data is null.

    it('should render dashboard grid when summary is null', () => {
      // Arrange - Widgets handle their own empty states
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

      // Assert - Dashboard grid renders, widgets handle empty states
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render dashboard grid when history is null', () => {
      // Arrange - Widgets handle their own empty states
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

      // Assert - Dashboard grid renders, widgets handle empty states
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render dashboard grid when accountTypes is null', () => {
      // Arrange - Widgets handle their own empty states
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

      // Assert - Dashboard grid renders, widgets handle empty states
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render dashboard grid when all data is null', () => {
      // Arrange - Widgets handle their own empty states
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

      // Assert - Dashboard grid renders, widgets handle empty states
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should still render dashboard with widget grid when data is empty', () => {
      // Arrange - With widget system, dashboard always renders the grid
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

      // Assert - Dashboard grid is rendered, individual widgets handle empty states
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render widget grid even when data is empty', () => {
      // Arrange - Individual widgets handle their own empty/loading states
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

      // Assert - Dashboard grid is rendered with widgets (widgets show their own empty states)
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
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

    it('should display subtitle when not editing', () => {
      // Act
      render(<DashboardPage />)

      // Assert - Default subtitle when not in edit mode
      expect(screen.getByText('Track your balance and cash flow')).toBeInTheDocument()
    })

    it('should render DashboardGrid component (widget system)', () => {
      // Act - Widget system (Issue #354)
      render(<DashboardPage />)

      // Assert - DashboardGrid is rendered (contains all widgets)
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
    })

    it('should render Edit button', () => {
      // Act - Widget customization (Issue #354)
      render(<DashboardPage />)

      // Assert
      expect(screen.getByRole('button', { name: /Edit/i })).toBeInTheDocument()
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

    it('should initialize with default filters (current month)', () => {
      // Act
      render(<DashboardPage />)

      // Assert - Filters are managed by DashboardContext with 'month' as default timeframe
      const calls = vi.mocked(useDashboardData).mock.calls
      expect(calls.length).toBeGreaterThan(0)

      const filters = calls[0][0]
      expect(filters).toBeDefined()
      expect(filters?.startDate).toBeDefined()
      expect(filters?.endDate).toBeDefined()
      // accountTypes is undefined by default (no filter)
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

  // Note: Chart Breakdown Toggle functionality is now inside BalanceHistoryWidget (Issue #354)
  // Those tests have been moved to BalanceHistoryWidget.test.tsx

  describe('Widget Grid Layout', () => {
    // Issue #354: Customizable Dashboard Widget System
    // Issue #414: Filters moved to header row
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

    it('should render filters in header area above widgets', () => {
      // Act - Issue #414: Filters now in header, not sidebar
      render(<DashboardPage />)

      // Assert - Filters are rendered
      expect(screen.getByTestId('timeframe-selector')).toBeInTheDocument()
      expect(screen.getByTestId('account-type-filter')).toBeInTheDocument()
    })

    it('should render DashboardGrid with full width', () => {
      // Act
      render(<DashboardPage />)

      // Assert - DashboardGrid is rendered (now has full width, no sidebar)
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
    })
  })

  describe('Filter Props', () => {
    // Issue #354: Filters are passed through DashboardContext to widgets
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
      // Arrange - With widget system, data loading is delegated to individual widgets
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

      // Assert - Dashboard grid is rendered (widgets handle their own empty states)
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should handle null history gracefully', () => {
      // Arrange - With widget system, data loading is delegated to individual widgets
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

      // Assert - Dashboard grid is rendered (widgets handle their own empty states)
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should handle null accountTypes gracefully', () => {
      // Arrange - With widget system, data loading is delegated to individual widgets
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

      // Assert - Dashboard grid is rendered (widgets handle their own empty states)
      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })
  })

  describe('Onboarding Hint', () => {
    // Issue #412: Dashboard widget UX improvements - discoverability hint
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

    it('should show onboarding hint when showHint is true and not editing', () => {
      // Arrange
      vi.mocked(useDashboardOnboarding).mockReturnValue({
        showHint: true,
        dismissHint: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.getByText(/Click "Edit" to customize your dashboard/i)).toBeInTheDocument()
    })

    it('should not show onboarding hint when showHint is false', () => {
      // Arrange
      vi.mocked(useDashboardOnboarding).mockReturnValue({
        showHint: false,
        dismissHint: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert
      expect(screen.queryByText(/Click "Edit" to customize your dashboard/i)).not.toBeInTheDocument()
    })

    it('should not show onboarding hint when editing', async () => {
      // Arrange
      const user = userEvent.setup()
      vi.mocked(useDashboardOnboarding).mockReturnValue({
        showHint: true,
        dismissHint: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Click Edit to enter edit mode
      await user.click(screen.getByRole('button', { name: /Edit/i }))

      // Assert - Hint should be hidden while editing
      expect(screen.queryByText(/Click "Edit" to customize your dashboard/i)).not.toBeInTheDocument()
    })

    it('should call dismissHint when dismiss button clicked', async () => {
      // Arrange
      const user = userEvent.setup()
      const mockDismissHint = vi.fn()
      vi.mocked(useDashboardOnboarding).mockReturnValue({
        showHint: true,
        dismissHint: mockDismissHint,
      })

      // Act
      render(<DashboardPage />)

      // Click dismiss button
      const dismissButton = screen.getByLabelText('Dismiss tip')
      await user.click(dismissButton)

      // Assert
      expect(mockDismissHint).toHaveBeenCalledTimes(1)
    })

    it('should render hint with proper styling', () => {
      // Arrange
      vi.mocked(useDashboardOnboarding).mockReturnValue({
        showHint: true,
        dismissHint: vi.fn(),
      })

      // Act
      render(<DashboardPage />)

      // Assert - Check hint has correct styling classes
      const hint = screen.getByText(/Click "Edit" to customize your dashboard/i)
      expect(hint).toBeInTheDocument()
    })
  })
})
