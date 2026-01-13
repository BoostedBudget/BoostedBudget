/**
 * DashboardPage Component Tests
 * Issue #575: Migrated to bun:test with centralized mocks
 */

import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import type {
  BalanceSummary,
  BalanceHistory,
  AccountTypesResponse,
} from '../types'

// Import centralized mocks
import { mockUseAuth, createMockClerkAuth } from '@/test/mocks/clerk'
import {
  mockUseDashboardContext,
  mockSetFilters,
  mockSetTimeframe,
  mockSetIsEditing,
} from '@/test/mocks/dashboard-context'
import {
  mockUseDashboardData,
  mockDashboardDataRefetch,
  mockUseDashboardLayout,
  mockUpdateLayout,
  mockAddWidget,
  mockRemoveWidget,
  mockResetToDefault,
  mockSaveNow,
  DEFAULT_LAYOUT_MOCK,
  mockUseDashboardOnboarding,
  mockDismissHint,
} from '@/test/mocks/dashboard-wrapper-hooks'

// Issue #575: Register wrapper hook mocks inline (not globally preloaded)
// This must be BEFORE importing the component
// Note: We use spread args to capture arguments for test assertions
mock.module('@/features/dashboard/hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))
mock.module('../hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))

mock.module('@/features/dashboard/hooks/useDashboardLayout', () => ({
  useDashboardLayout: (...args: unknown[]) => mockUseDashboardLayout(...args),
}))
mock.module('../hooks/useDashboardLayout', () => ({
  useDashboardLayout: (...args: unknown[]) => mockUseDashboardLayout(...args),
}))

mock.module('@/features/dashboard/hooks/useDashboardOnboarding', () => ({
  useDashboardOnboarding: (...args: unknown[]) => mockUseDashboardOnboarding(...args),
}))
mock.module('../hooks/useDashboardOnboarding', () => ({
  useDashboardOnboarding: (...args: unknown[]) => mockUseDashboardOnboarding(...args),
}))

// Mock child components (these are isolated mocks specific to this test)
mock.module('../widgets/DashboardGrid', () => ({
  DashboardGrid: () => <div data-testid="dashboard-grid">Dashboard Grid</div>,
}))

mock.module('../widgets/WidgetCatalog', () => ({
  WidgetCatalog: ({ isOpen }: { isOpen: boolean }) =>
    isOpen ? <div data-testid="widget-catalog">Widget Catalog</div> : null,
}))

// Issue #575: Use REAL AccountTypeFilter and TimeframeSelector components
// They have data-testid attributes for test queries.
// We don't mock them to prevent pollution of their standalone tests.
// The real components work with the mocked hook data (mockUseDashboardData).

// NOW import testing utilities and component AFTER mocks
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DashboardPage } from './DashboardPage'

describe('DashboardPage', () => {
  const mockSummary: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 15000.5,
    by_type: {
      depository: {
        balance: 5000.0,
        account_count: 2,
        accounts: [
          { id: 'acc-1', name: 'Checking', balance: 3000.0 },
          { id: 'acc-2', name: 'Savings', balance: 2000.0 },
        ],
      },
      credit: {
        balance: -2000.0,
        account_count: 1,
        accounts: [{ id: 'acc-3', name: 'Credit Card', balance: -2000.0 }],
      },
    },
    period_comparison: {
      previous_balance: 14000.0,
      change_amount: 1000.5,
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
        balance: 14000.0,
        by_type: { depository: 4500.0, credit: -1500.0 },
      },
      {
        date: '2025-11-15',
        balance: 15000.5,
        by_type: { depository: 5000.0, credit: -2000.0 },
      },
    ],
  }

  const mockAccountTypes: AccountTypesResponse = {
    types: [
      { key: 'depository', label: 'Checking & Savings', count: 2, total_balance: 5000.0 },
      { key: 'credit', label: 'Credit Cards', count: 1, total_balance: -2000.0 },
    ],
  }

  // Default context value
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

  // Store original window.location for restoration
  const originalLocation = window.location

  beforeEach(() => {
    // Clear all centralized mocks
    mockUseAuth.mockClear()
    mockUseDashboardData.mockClear()
    mockUseDashboardLayout.mockClear()
    mockUseDashboardOnboarding.mockClear()
    mockUseDashboardContext.mockClear()
    mockDashboardDataRefetch.mockClear()
    mockUpdateLayout.mockClear()
    mockAddWidget.mockClear()
    mockRemoveWidget.mockClear()
    mockResetToDefault.mockClear()
    mockSaveNow.mockClear()
    mockDismissHint.mockClear()
    mockSetFilters.mockClear()
    mockSetTimeframe.mockClear()
    mockSetIsEditing.mockClear()

    // Default mock returns
    mockUseAuth.mockReturnValue(
      createMockClerkAuth({
        isSignedIn: true,
        isLoaded: true,
      })
    )
    mockUseDashboardLayout.mockReturnValue({
      layout: DEFAULT_LAYOUT_MOCK,
      loading: false,
      error: null,
      saving: false,
      updateLayout: mockUpdateLayout,
      addWidget: mockAddWidget,
      removeWidget: mockRemoveWidget,
      resetToDefault: mockResetToDefault,
      saveNow: mockSaveNow,
    })
    mockUseDashboardOnboarding.mockReturnValue({
      showHint: false,
      dismissHint: mockDismissHint,
    })
    mockUseDashboardContext.mockReturnValue(defaultContextValue)
    mockUseDashboardData.mockReturnValue({
      summary: null,
      history: null,
      accountTypes: null,
      loading: false,
      error: null,
      refetch: mockDashboardDataRefetch,
    })
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
        layout: DEFAULT_LAYOUT_MOCK,
        loading: true,
        error: null,
        saving: false,
        updateLayout: mockUpdateLayout,
        addWidget: mockAddWidget,
        removeWidget: mockRemoveWidget,
        resetToDefault: mockResetToDefault,
        saveNow: mockSaveNow,
      })

      render(<DashboardPage />)

      expect(screen.getByText('Loading dashboard...')).toBeInTheDocument()
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })

    it('should display "Loading dashboard..." text when layout loading', () => {
      mockUseDashboardLayout.mockReturnValue({
        layout: DEFAULT_LAYOUT_MOCK,
        loading: true,
        error: null,
        saving: false,
        updateLayout: mockUpdateLayout,
        addWidget: mockAddWidget,
        removeWidget: mockRemoveWidget,
        resetToDefault: mockResetToDefault,
        saveNow: mockSaveNow,
      })

      render(<DashboardPage />)

      expect(screen.getByText('Loading dashboard...')).toBeInTheDocument()
    })

    it('should not render dashboard grid while loading', () => {
      mockUseDashboardLayout.mockReturnValue({
        layout: DEFAULT_LAYOUT_MOCK,
        loading: true,
        error: null,
        saving: false,
        updateLayout: mockUpdateLayout,
        addWidget: mockAddWidget,
        removeWidget: mockRemoveWidget,
        resetToDefault: mockResetToDefault,
        saveNow: mockSaveNow,
      })

      render(<DashboardPage />)

      expect(screen.queryByTestId('dashboard-grid')).not.toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when layout error exists', () => {
      mockUseDashboardLayout.mockReturnValue({
        layout: DEFAULT_LAYOUT_MOCK,
        loading: false,
        error: 'Failed to fetch dashboard layout',
        saving: false,
        updateLayout: mockUpdateLayout,
        addWidget: mockAddWidget,
        removeWidget: mockRemoveWidget,
        resetToDefault: mockResetToDefault,
        saveNow: mockSaveNow,
      })

      render(<DashboardPage />)

      expect(screen.getByText('Failed to Load Dashboard')).toBeInTheDocument()
      expect(screen.getByText('Failed to fetch dashboard layout')).toBeInTheDocument()
    })

    it('should display error details from layout error', () => {
      mockUseDashboardLayout.mockReturnValue({
        layout: DEFAULT_LAYOUT_MOCK,
        loading: false,
        error: 'Network timeout error',
        saving: false,
        updateLayout: mockUpdateLayout,
        addWidget: mockAddWidget,
        removeWidget: mockRemoveWidget,
        resetToDefault: mockResetToDefault,
        saveNow: mockSaveNow,
      })

      render(<DashboardPage />)

      expect(screen.getByText('Network timeout error')).toBeInTheDocument()
    })

    it('should show "Try Again" button on error', () => {
      mockUseDashboardLayout.mockReturnValue({
        layout: DEFAULT_LAYOUT_MOCK,
        loading: false,
        error: 'API error',
        saving: false,
        updateLayout: mockUpdateLayout,
        addWidget: mockAddWidget,
        removeWidget: mockRemoveWidget,
        resetToDefault: mockResetToDefault,
        saveNow: mockSaveNow,
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
        layout: DEFAULT_LAYOUT_MOCK,
        loading: false,
        error: 'API error',
        saving: false,
        updateLayout: mockUpdateLayout,
        addWidget: mockAddWidget,
        removeWidget: mockRemoveWidget,
        resetToDefault: mockResetToDefault,
        saveNow: mockSaveNow,
      })

      render(<DashboardPage />)

      const retryButton = screen.getByRole('button', { name: 'Try Again' })
      await user.click(retryButton)

      expect(mockReload).toHaveBeenCalledTimes(1)
    })

    it('should not render dashboard grid on error', () => {
      mockUseDashboardLayout.mockReturnValue({
        layout: DEFAULT_LAYOUT_MOCK,
        loading: false,
        error: 'API error',
        saving: false,
        updateLayout: mockUpdateLayout,
        addWidget: mockAddWidget,
        removeWidget: mockRemoveWidget,
        resetToDefault: mockResetToDefault,
        saveNow: mockSaveNow,
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
        refetch: mockDashboardDataRefetch,
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
        refetch: mockDashboardDataRefetch,
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
        refetch: mockDashboardDataRefetch,
      })

      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render dashboard grid when all data is null', () => {
      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should still render dashboard with widget grid when data is empty', () => {
      render(<DashboardPage />)

      expect(screen.getByTestId('dashboard-grid')).toBeInTheDocument()
      expect(screen.getByText('Dashboard')).toBeInTheDocument()
    })

    it('should render widget grid even when data is empty', () => {
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
        refetch: mockDashboardDataRefetch,
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
        refetch: mockDashboardDataRefetch,
      })
    })

    it('should initialize with default filters (current month)', () => {
      render(<DashboardPage />)

      const calls = mockUseDashboardData.mock.calls
      expect(calls.length).toBeGreaterThan(0)

      const filters = calls[0][0] as {
        startDate?: string
        endDate?: string
        accountTypes?: string[]
      }
      expect(filters).toBeDefined()
      expect(filters?.startDate).toBeDefined()
      expect(filters?.endDate).toBeDefined()
    })

    it('should update filters when account types change', async () => {
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Click on "Checking & Savings" checkbox in real AccountTypeFilter
      const checkbox = screen.getByRole('checkbox', { name: /Checking & Savings/i })
      await user.click(checkbox)

      await waitFor(() => {
        expect(mockSetFilters).toHaveBeenCalled()
      })
    })

    it('should update filters when timeframe changes', async () => {
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Click on "7 Days" button in real TimeframeSelector
      const timeframeButton = screen.getByRole('button', { name: '7 Days' })
      await user.click(timeframeButton)

      await waitFor(() => {
        expect(mockSetFilters).toHaveBeenCalled()
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
        refetch: mockDashboardDataRefetch,
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
        refetch: mockDashboardDataRefetch,
      })
    })

    it('should pass accountTypes to AccountTypeFilter', () => {
      render(<DashboardPage />)

      // Real AccountTypeFilter shows the account type labels
      expect(screen.getByText('Checking & Savings')).toBeInTheDocument()
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
    })

    it('should pass selected account types to filter', () => {
      render(<DashboardPage />)

      // Real AccountTypeFilter shows "All Types" checkbox that is checked when no filter selected
      const allTypesCheckbox = screen.getByRole('checkbox', { name: 'All Types' })
      expect(allTypesCheckbox).toBeChecked()
    })

    it('should pass onRangeChange callback to TimeframeSelector', async () => {
      const user = userEvent.setup()
      render(<DashboardPage />)

      // Click "7 Days" in real TimeframeSelector
      const button = screen.getByRole('button', { name: '7 Days' })
      await user.click(button)

      await waitFor(() => {
        expect(mockSetFilters).toHaveBeenCalled()
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
        refetch: mockDashboardDataRefetch,
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
        refetch: mockDashboardDataRefetch,
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
        refetch: mockDashboardDataRefetch,
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
        refetch: mockDashboardDataRefetch,
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

      expect(
        screen.queryByText(/Click "Edit" to customize your dashboard/i)
      ).not.toBeInTheDocument()
    })

    it('should not show onboarding hint when editing', () => {
      mockUseDashboardContext.mockReturnValue({
        ...defaultContextValue,
        isEditing: true,
      })
      mockUseDashboardOnboarding.mockReturnValue({
        showHint: true,
        dismissHint: mockDismissHint,
      })

      render(<DashboardPage />)

      expect(
        screen.queryByText(/Click "Edit" to customize your dashboard/i)
      ).not.toBeInTheDocument()
    })

    it('should call setIsEditing when Edit button clicked', async () => {
      const user = userEvent.setup()
      mockUseDashboardOnboarding.mockReturnValue({
        showHint: true,
        dismissHint: mockDismissHint,
      })

      render(<DashboardPage />)

      await user.click(screen.getByRole('button', { name: /Edit/i }))

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
