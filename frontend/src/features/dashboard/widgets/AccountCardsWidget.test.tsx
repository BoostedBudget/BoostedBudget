/**
 * Account Cards Widget Tests
 *
 * Tests the AccountCardsWidget component including loading,
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

vi.mock('../components/AccountBalanceCards', () => ({
  AccountBalanceCards: ({ summary }: { summary: unknown }) => (
    <div data-testid="account-balance-cards">
      Account Cards: {JSON.stringify(summary)}
    </div>
  ),
}))

vi.mock('./registry', () => ({
  registerWidget: vi.fn(),
}))

import { AccountCardsWidget } from './AccountCardsWidget'
import { useDashboardContext } from '../context/DashboardContext'
import { useDashboardData } from '../hooks/useDashboardData'
import type { BalanceSummary } from '../types'

describe('AccountCardsWidget', () => {
  const mockFilters = {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    accountTypes: undefined,
  }

  const mockSummary: BalanceSummary = {
    as_of_date: '2024-01-31',
    total_balance: 18000,
    by_type: {
      depository: {
        balance: 20000,
        account_count: 2,
        accounts: [
          { id: '1', name: 'Checking', balance: 5000 },
          { id: '2', name: 'Savings', balance: 15000 },
        ],
      },
      credit: {
        balance: -2000,
        account_count: 1,
        accounts: [
          { id: '3', name: 'Credit Card', balance: -2000 },
        ],
      },
    },
  }

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
        summary: null,
        history: null,
        accountTypes: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      render(<AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />)

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
        error: new Error('Failed to load accounts'),
        refetch: vi.fn(),
      })

      render(<AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByText('Failed to load accounts')).toBeInTheDocument()
    })

    it('should show error in red', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: null,
        history: null,
        accountTypes: null,
        loading: false,
        error: new Error('API Error'),
        refetch: vi.fn(),
      })

      render(<AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />)

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

      render(<AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByText('No account data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    it('should render AccountBalanceCards component with data', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(screen.getByTestId('account-balance-cards')).toBeInTheDocument()
    })

    it('should pass summary to AccountBalanceCards component', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />)

      // Verify summary data is passed
      expect(screen.getByTestId('account-balance-cards')).toHaveTextContent('18000')
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

      render(<AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(useDashboardData).toHaveBeenCalledWith(
        expect.objectContaining({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      )
    })

    it('should pass accountTypes filter when specified', () => {
      const filtersWithAccountTypes = {
        ...mockFilters,
        accountTypes: ['depository'],
      }

      vi.mocked(useDashboardContext).mockReturnValue({
        filters: filtersWithAccountTypes,
        setFilters: vi.fn(),
        timeframe: 'month',
        setTimeframe: vi.fn(),
        isEditing: false,
        setIsEditing: vi.fn(),
      })

      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      render(<AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />)

      expect(useDashboardData).toHaveBeenCalledWith(
        expect.objectContaining({
          accountTypes: ['depository'],
        })
      )
    })
  })

  /**
   * Widget Wrapper Styling Tests - Issue #420
   *
   * These tests verify that the widget wrapper does NOT have redundant
   * styling that conflicts with WidgetWrapper (which already provides overflow-auto).
   *
   * Root cause: Double overflow-auto causes scrollbar issues.
   */
  describe('Widget Wrapper Styling - Issue #420', () => {
    it('should NOT have overflow-auto on success wrapper (WidgetWrapper provides it)', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      const { container } = render(
        <AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />
      )

      const wrapper = container.firstChild as HTMLElement
      expect(wrapper).toHaveClass('h-full')
      expect(wrapper).not.toHaveClass('overflow-auto')
    })

    it('should have h-full for height propagation', () => {
      vi.mocked(useDashboardData).mockReturnValue({
        summary: mockSummary,
        history: null,
        accountTypes: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      const { container } = render(
        <AccountCardsWidget widgetId="test-1" onRemove={vi.fn()} />
      )

      const wrapper = container.firstChild as HTMLElement
      expect(wrapper).toHaveClass('h-full')
    })
  })
})
