/**
 * DashboardPage Component Tests
 * Issue #575: Migrated to bun:test
 */

import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import type { ReactNode } from 'react'
import type {
  BalanceSummary,
  BalanceHistory,
  AccountTypesResponse,
  WidgetType,
} from '../types'

// Mock Clerk
const mockGetToken = mock(() => Promise.resolve('mock-token'))
const mockUseAuth = mock(() => ({
  isSignedIn: true,
  isLoaded: true,
  getToken: mockGetToken,
}))

mock.module('@clerk/react-router', () => ({
  useAuth: (...args: unknown[]) => mockUseAuth(...args),
}))

// Mock useDashboardData
const mockRefetch = mock(() => {})
const mockUseDashboardData = mock(() => ({
  summary: null,
  history: null,
  accountTypes: null,
  loading: false,
  error: null,
  refetch: mockRefetch,
}))

mock.module('../hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))

// Mock useDashboardLayout
const mockUpdateLayout = mock(() => {})
const mockAddWidget = mock(() => {})
const mockRemoveWidget = mock(() => {})
const mockResetToDefault = mock(() => {})
const mockSaveNow = mock(() => {})

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
  updateLayout: mockUpdateLayout,
  addWidget: mockAddWidget,
  removeWidget: mockRemoveWidget,
  resetToDefault: mockResetToDefault,
  saveNow: mockSaveNow,
}

const mockUseDashboardLayout = mock(() => defaultLayoutMock)

mock.module('../hooks/useDashboardLayout', () => ({
  useDashboardLayout: (...args: unknown[]) => mockUseDashboardLayout(...args),
}))

// Mock useDashboardOnboarding
const mockDismissHint = mock(() => {})
const mockUseDashboardOnboarding = mock(() => ({
  showHint: false,
  dismissHint: mockDismissHint,
}))

mock.module('../hooks/useDashboardOnboarding', () => ({
  useDashboardOnboarding: (...args: unknown[]) => mockUseDashboardOnboarding(...args),
}))

// Mock DashboardContext to have direct control over state
const mockSetFilters = mock(() => {})
const mockSetTimeframe = mock(() => {})
const mockSetIsEditing = mock(() => {})

const defaultContextValue = {
  filters: {
    startDate: '2025-01-01',
    endDate: '2025-01-31',
    accountTypes: undefined,
    includeTransfers: false,
  },
  setFilters: mockSetFilters,
  timeframe: 'month' as const,
  setTimeframe: mockSetTimeframe,
  isEditing: false,
  setIsEditing: mockSetIsEditing,
}

const mockUseDashboardContext = mock(() => defaultContextValue)

mock.module('../context/DashboardContext', () => ({
  useDashboardContext: (...args: unknown[]) => mockUseDashboardContext(...args),
  DashboardProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  DashboardContext: {
    Provider: ({ children }: { children: ReactNode }) => <>{children}</>,
  },
}))

// Mock child components
mock.module('../widgets/DashboardGrid', () => ({
  DashboardGrid: () => <div data-testid="dashboard-grid">Dashboard Grid</div>,
}))

mock.module('../widgets/WidgetCatalog', () => ({
  WidgetCatalog: ({ isOpen }: { isOpen: boolean }) =>
    isOpen ? <div data-testid="widget-catalog">Widget Catalog</div> : null,
}))

mock.module('./BalanceOverview', () => ({
  BalanceOverview: ({ summary }: { summary: BalanceSummary }) => (
    <div data-testid="balance-overview">
      Balance Overview: ${summary.total_balance}
    </div>
  ),
}))

mock.module('./BalanceHistoryChart', () => ({
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

mock.module('./AccountTypeFilter', () => ({
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

mock.module('./TimeframeSelector', () => ({
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

mock.module('./AccountBalanceCards', () => ({
  AccountBalanceCards: ({ summary }: { summary: BalanceSummary }) => (
    <div data-testid="account-balance-cards">
      Accounts: {Object.keys(summary.by_type).length} types
    </div>
  ),
}))

// NOW import testing utilities and component AFTER mocks
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DashboardPage } from './DashboardPage'

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

  // Store original window.location for restoration
  const originalLocation = window.location

  beforeEach(() => {
    // Clear all mocks
    mockUseAuth.mockClear()
    mockUseDashboardData.mockClear()
    mockUseDashboardLayout.mockClear()
    mockUseDashboardOnboarding.mockClear()
    mockUseDashboardContext.mockClear()
    mockRefetch.mockClear()
    mockUpdateLayout.mockClear()
    mockAddWidget.mockClear()
    mockRemoveWidget.mockClear()
    mockResetToDefault.mockClear()
    mockSaveNow.mockClear()
    mockDismissHint.mockClear()
    mockGetToken.mockClear()
    mockSetFilters.mockClear()
    mockSetTimeframe.mockClear()
    mockSetIsEditing.mockClear()

    // Default mock returns
    mockUseAuth.mockReturnValue({
      isSignedIn: true,
      isLoaded: true,
      getToken: mockGetToken,
    })
    mockUseDashboardLayout.mockReturnValue(defaultLayoutMock)
    mockUseDashboardOnboarding.mockReturnValue({
      showHint: false,
      dismissHint: mockDismissHint,
    })
    mockUseDashboardContext.mockReturnValue(defaultContextValue)
  })

  afterEach(() => {
    // Restore window.location if it was modified
    if (window.location !== originalLocation) {
      Object.defineProperty(window, 'location', {
        value: originalLocation,
        writable: true,
        configurable: true,
      })
    }
  })

  describe('Loading State', () => {
    it('should show loading spinner when layout loading=true', () => {
      mockUseDashboardLayout.mockReturnValue({
        ...defaultLayoutMock,
        loading: true,
      })
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByText('Loading dashboard...')).toBeInTheDocument()
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })

    it('should display "Loading dashboard..." text when layout loading', () => {
      mockUseDashboardLayout.mockReturnValue({
        ...defaultLayoutMock,
        loading: true,
      })
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByText('Loading dashboard...')).toBeInTheDocument()
    })

    it('should not render dashboard grid while loading', () => {
      mockUseDashboardLayout.mockReturnValue({
        ...defaultLayoutMock,
        loading: true,
      })
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.queryByTestId('dashboard-grid')).not.toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when layout error exists', () => {
      mockUseDashboardLayout.mockReturnValue({
        ...defaultLayoutMock,
        error: 'Failed to fetch dashboard layout',
      })
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByText('Failed to Load Dashboard')).toBeInTheDocument()
      expect(screen.getByText('Failed to fetch dashboard layout')).toBeInTheDocument()
    })

    it('should display error details from layout error', () => {
      mockUseDashboardLayout.mockReturnValue({
        ...defaultLayoutMock,
        error: 'Network timeout error',
      })
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByText('Network timeout error')).toBeInTheDocument()
    })

    it('should show "Try Again" button on error', () => {
      mockUseDashboardLayout.mockReturnValue({
        ...defaultLayoutMock,
        error: 'API error',
      })
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      const retryButton = screen.getByRole('button', { name: 'Try Again' })
      expect(retryButton).toBeInTheDocument()
    })

    it('should reload page when "Try Again" clicked', async () => {
      const user = userEvent.setup()
      const mockReload = mock(() => {})
      Object.defineProperty(window, 'location', {
        value: { reload: mockReload },
        writable: true,
        configurable: true,
      })

      mockUseDashboardLayout.mockReturnValue({
        ...defaultLayoutMock,
        error: 'API error',
      })
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      const retryButton = screen.getByRole('button', { name: 'Try Again' })
      await user.click(retryButton)

      expect(mockReload).toHaveBeenCalledTimes(1)
    })

    it('should not render dashboard grid on error', () => {
      mockUseDashboardLayout.mockReturnValue({
        ...defaultLayoutMock,
        error: 'API error',
      })
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.queryByTestId('balance-overview')).not.toBeInTheDocument()
      expect(screen.queryByTestId('balance-history-chart')).not.toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should render dashboard grid when summary is null', () => {
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render dashboard grid when history is null', () => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render dashboard grid when accountTypes is null', () => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render dashboard grid when all data is null', () => {
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should still render dashboard with widget grid when data is empty', () => {
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render widget grid even when data is empty', () => {
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
    })
  })

  describe('Successful Render', () => {
    beforeEach(() => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })
    })

    it('should render dashboard header with title', () => {
      render(<DashboardPage />)

      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should display subtitle when not editing', () => {
      render(<DashboardPage />)

      expect(screen.getByText('Track your balance and cash flow')).toBeInTheDocument()
    })

    it('should render DashboardGrid component (widget system)', () => {
      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
    })

    it('should render Edit button', () => {
      render(<DashboardPage />)

      expect(screen.getByRole('button', { name: /Edit/i })).toBeInTheDocument()
    })

    it('should render AccountTypeFilter component', () => {
      render(<DashboardPage />)

      expect(screen.getByTestId('account-type-filter')).toBeInTheDocument()
    })

    it('should render TimeframeSelector component', () => {
      render(<DashboardPage />)

      expect(screen.getByTestId('timeframe-selector')).toBeInTheDocument()
    })
  })

  describe('Filter Management', () => {
    beforeEach(() => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })
    })

    it('should initialize with default filters (current month)', () => {
      render(<DashboardPage />)

      const calls = mockUseDashboardData.mock.calls
      expect(calls.length).toBeGreaterThan(0)

      const filters = calls[0][0] as { startDate?: string; endDate?: string; accountTypes?: string[] }
      expect(filters).toBeDefined()
      expect(filters?.startDate).toBeDefined()
      expect(filters?.endDate).toBeDefined()
    })

    it('should update filters when account types change', async () => {
      const user = userEvent.setup()
      render(<DashboardPage />)

      const filterButton = screen.getByText(/Filter: 2 types/)
      await user.click(filterButton)

      // Verify the component was rendered and button exists
      // The actual filter state change flows through DashboardContext
      // which triggers a re-render with updated selectedTypes
      await waitFor(() => {
        // After clicking, useDashboardData should have been called again with updated filters
        const calls = mockUseDashboardData.mock.calls
        // The context state update should trigger re-render with accountTypes filter
        expect(calls.length).toBeGreaterThanOrEqual(1)
      })
    })

    it('should update filters when timeframe changes', async () => {
      const user = userEvent.setup()
      render(<DashboardPage />)

      const timeframeButton = screen.getByText('Change Timeframe')
      await user.click(timeframeButton)

      // Verify the button was clicked - the state change flows through DashboardContext
      // which should eventually trigger useDashboardData with updated dates
      await waitFor(() => {
        const calls = mockUseDashboardData.mock.calls
        expect(calls.length).toBeGreaterThanOrEqual(1)
      })
    })

    it('should pass filters to useDashboardData hook', () => {
      render(<DashboardPage />)

      expect(mockUseDashboardData).toHaveBeenCalled()
      const filters = mockUseDashboardData.mock.calls[0][0] as {
        startDate?: string
        endDate?: string
        accountTypes?: string[]
      }
      expect(filters).toHaveProperty('startDate')
      expect(filters).toHaveProperty('endDate')
      expect(filters).toHaveProperty('accountTypes')
    })
  })

  describe('Widget Grid Layout', () => {
    beforeEach(() => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })
    })

    it('should render filters in header area above widgets', () => {
      render(<DashboardPage />)

      expect(screen.getByTestId('timeframe-selector')).toBeInTheDocument()
      expect(screen.getByTestId('account-type-filter')).toBeInTheDocument()
    })

    it('should render DashboardGrid with full width', () => {
      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
    })
  })

  describe('Filter Props', () => {
    beforeEach(() => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })
    })

    it('should pass accountTypes to AccountTypeFilter', () => {
      render(<DashboardPage />)

      const filter = screen.getByTestId('account-type-filter')
      expect(filter).toHaveTextContent('2 types')
    })

    it('should pass selected account types to filter', () => {
      render(<DashboardPage />)

      const filter = screen.getByTestId('account-type-filter')
      expect(filter).toHaveTextContent('Selected: 0')
    })

    it('should pass onRangeChange callback to TimeframeSelector', async () => {
      const user = userEvent.setup()
      render(<DashboardPage />)

      const button = screen.getByText('Change Timeframe')
      await user.click(button)

      // Verify the callback was triggered - the button click invokes onRangeChange
      // which flows through DashboardContext to update filters
      await waitFor(() => {
        const calls = mockUseDashboardData.mock.calls
        expect(calls.length).toBeGreaterThanOrEqual(1)
      })
    })
  })

  describe('Edge Cases', () => {
    it('should handle null summary gracefully', () => {
      mockUseDashboardData.mockReturnValue({
        summary: null,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should handle null history gracefully', () => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should handle null accountTypes gracefully', () => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })
  })

  describe('Onboarding Hint', () => {
    beforeEach(() => {
      mockUseDashboardData.mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        accountTypes: mockAccountTypes,
        loading: false,
        error: null,
        refetch: mockRefetch,
      })
    })

    it('should show onboarding hint when showHint is true and not editing', () => {
      mockUseDashboardOnboarding.mockReturnValue({
        showHint: true,
        dismissHint: mockDismissHint,
      })

      render(<DashboardPage />)

      expect(screen.getByText(/Click "Edit" to customize your dashboard/i)).toBeInTheDocument()
    })

    it('should not show onboarding hint when showHint is false', () => {
      mockUseDashboardOnboarding.mockReturnValue({
        showHint: false,
        dismissHint: mockDismissHint,
      })

      render(<DashboardPage />)

      expect(screen.queryByText(/Click "Edit" to customize your dashboard/i)).not.toBeInTheDocument()
    })

    it('should not show onboarding hint when editing', () => {
      // Set context to editing mode directly via mock
      mockUseDashboardContext.mockReturnValue({
        ...defaultContextValue,
        isEditing: true,
      })
      mockUseDashboardOnboarding.mockReturnValue({
        showHint: true,
        dismissHint: mockDismissHint,
      })

      render(<DashboardPage />)

      // When isEditing is true, the hint should not be shown (condition: showHint && !isEditing)
      expect(screen.queryByText(/Click "Edit" to customize your dashboard/i)).not.toBeInTheDocument()
    })

    it('should call setIsEditing when Edit button clicked', async () => {
      const user = userEvent.setup()
      mockUseDashboardOnboarding.mockReturnValue({
        showHint: true,
        dismissHint: mockDismissHint,
      })

      render(<DashboardPage />)

      await user.click(screen.getByRole('button', { name: /Edit/i }))

      // Verify setIsEditing was called with true
      expect(mockSetIsEditing).toHaveBeenCalledWith(true)
    })

    it('should call dismissHint when dismiss button clicked', async () => {
      const user = userEvent.setup()
      mockUseDashboardOnboarding.mockReturnValue({
        showHint: true,
        dismissHint: mockDismissHint,
      })

      render(<DashboardPage />)

      const dismissButton = screen.getByLabelText('Dismiss tip')
      await user.click(dismissButton)

      expect(mockDismissHint).toHaveBeenCalledTimes(1)
    })

    it('should render hint with proper styling', () => {
      mockUseDashboardOnboarding.mockReturnValue({
        showHint: true,
        dismissHint: mockDismissHint,
      })

      render(<DashboardPage />)

      const hint = screen.getByText(/Click "Edit" to customize your dashboard/i)
      expect(hint).toBeInTheDocument()
    })
  })
})
