/**
 * AccountCardsWidget Tests
 * Issue #575: Refactored to use centralized mocks
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import type { BalanceSummary } from '../types'

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
mock.module('../components/AccountBalanceCards', () => ({
  AccountBalanceCards: ({ summary }: { summary: unknown }) => (
    <div data-testid="account-balance-cards">
      Account Cards: {JSON.stringify(summary)}
    </div>
  ),
}))

import { AccountCardsWidget } from './AccountCardsWidget'

describe('AccountCardsWidget', () => {
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
        accounts: [{ id: '3', name: 'Credit Card', balance: -2000 }],
      },
    },
  }

  const defaultProps = {
    widgetId: 'test-1',
    onRemove: mock(() => {}),
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

      render(<AccountCardsWidget {...defaultProps} />)

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      setMockDataState({ error: new Error('Failed to load accounts') })

      render(<AccountCardsWidget {...defaultProps} />)

      expect(screen.getByText('Failed to load accounts')).toBeInTheDocument()
    })

    it('should show error in red', () => {
      setMockDataState({ error: new Error('API Error') })

      render(<AccountCardsWidget {...defaultProps} />)

      const errorText = screen.getByText('API Error')
      expect(errorText).toHaveClass('text-red-500')
    })
  })

  describe('Empty State', () => {
    it('should show no data message when summary is null', () => {
      setMockDataState({})

      render(<AccountCardsWidget {...defaultProps} />)

      expect(screen.getByText('No account data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      setMockDataState({ summary: mockSummary })
    })

    it('should render AccountBalanceCards component with data', () => {
      render(<AccountCardsWidget {...defaultProps} />)
      expect(screen.getByTestId('account-balance-cards')).toBeInTheDocument()
    })

    it('should pass summary to AccountBalanceCards component', () => {
      render(<AccountCardsWidget {...defaultProps} />)
      expect(screen.getByTestId('account-balance-cards')).toHaveTextContent('18000')
    })
  })

  describe('Context Integration', () => {
    it('should use filters from dashboard context', () => {
      setMockDataState({ summary: mockSummary })

      render(<AccountCardsWidget {...defaultProps} />)

      expect(mockUseDashboardData).toHaveBeenCalledWith(
        expect.objectContaining({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      )
    })

    it('should pass accountTypes filter when specified', () => {
      mockUseDashboardContext.mockReturnValue({
        ...defaultContextState,
        filters: {
          ...defaultContextState.filters,
          accountTypes: ['depository'],
        },
      })
      setMockDataState({ summary: mockSummary })

      render(<AccountCardsWidget {...defaultProps} />)

      expect(mockUseDashboardData).toHaveBeenCalledWith(
        expect.objectContaining({
          accountTypes: ['depository'],
        })
      )
    })
  })

  describe('Widget Wrapper Styling - Issue #420', () => {
    beforeEach(() => {
      setMockDataState({ summary: mockSummary })
    })

    it('should NOT have overflow-auto on success wrapper', () => {
      const { container } = render(<AccountCardsWidget {...defaultProps} />)

      const wrapper = container.firstChild as HTMLElement
      expect(wrapper).toHaveClass('h-full')
      expect(wrapper).not.toHaveClass('overflow-auto')
    })

    it('should have h-full for height propagation', () => {
      const { container } = render(<AccountCardsWidget {...defaultProps} />)

      const wrapper = container.firstChild as HTMLElement
      expect(wrapper).toHaveClass('h-full')
    })
  })
})
