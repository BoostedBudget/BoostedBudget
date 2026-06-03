/**
 * NetWorthWidget Tests
 * Issue #575: Refactored to use centralized mocks (no inline mock.module)
 * Issue #955: Updated for semantic tokens + View details link + MemoryRouter
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { NetWorthSummary, NetWorthHistory } from '../types'

// Issue #575: Use centralized mocks to prevent mock.module() pollution
// No inline mock.module() calls for DashboardContext or useNetWorthData
import '@/test/mocks/registry'
import {
  mockUseDashboardContext,
  mockSetFilters,
  mockSetTimeframe,
  mockSetIsEditing,
} from '@/test/mocks/dashboard-context'
import {
  mockUseNetWorthData,
  mockNetWorthRefetch,
} from '@/test/mocks/net-worth-hooks'

import { NetWorthWidget } from './NetWorthWidget'

function renderWidget() {
  return render(
    <MemoryRouter>
      <NetWorthWidget />
    </MemoryRouter>
  )
}

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

  const defaultContextState = {
    filters: {
      startDate: '2024-01-01',
      endDate: '2024-01-31',
      accountTypes: undefined,
    },
    setFilters: mockSetFilters,
    timeframe: 'this_month' as const,
    setTimeframe: mockSetTimeframe,
    isEditing: false,
    setIsEditing: mockSetIsEditing,
  }

  const defaultDataState = {
    summary: null,
    history: null,
    loading: false,
    error: null,
    refetch: mockNetWorthRefetch,
  }

  const setMockDataState = (state: Partial<typeof defaultDataState>) => {
    mockUseNetWorthData.mockReturnValue({ ...defaultDataState, ...state })
  }

  beforeEach(() => {
    mockUseDashboardContext.mockClear()
    mockUseNetWorthData.mockClear()
    mockNetWorthRefetch.mockClear()
    mockSetFilters.mockClear()
    mockSetTimeframe.mockClear()
    mockSetIsEditing.mockClear()
    mockUseDashboardContext.mockReturnValue(defaultContextState)
    mockUseNetWorthData.mockReturnValue(defaultDataState)
  })

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      setMockDataState({ loading: true })

      renderWidget()

      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      setMockDataState({ error: new Error('Network error') })

      renderWidget()

      expect(screen.getByText('Network error')).toBeInTheDocument()
    })

    it('should show error text in destructive color', () => {
      setMockDataState({ error: new Error('API Error') })

      renderWidget()

      const errorText = screen.getByText('API Error')
      expect(errorText).toHaveClass('text-destructive')
    })
  })

  describe('Empty State', () => {
    it('should show no data message when summary is null', () => {
      setMockDataState({})

      renderWidget()

      expect(screen.getByText('No data available')).toBeInTheDocument()
    })

    it('should show empty state when net worth data is all zero with no accounts', () => {
      const zeroSummary: NetWorthSummary = {
        as_of_date: '2024-01-31',
        net_worth: 0,
        total_assets: 0,
        total_liabilities: 0,
        by_category: {},
      }
      setMockDataState({ summary: zeroSummary })

      renderWidget()

      expect(screen.getByRole('status')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      setMockDataState({ summary: mockSummary, history: mockHistory })
    })

    it('should render net worth value', () => {
      renderWidget()
      expect(screen.getByText(/\$50,000/)).toBeInTheDocument()
    })

    it('should display positive net worth with success color', () => {
      renderWidget()
      const netWorthElement = screen.getByText(/\$50,000/)
      expect(netWorthElement).toHaveClass('text-success')
    })

    it('should display negative net worth with destructive color', () => {
      const negativeNetWorthSummary: NetWorthSummary = {
        ...mockSummary,
        net_worth: -10000,
        total_assets: 15000,
        total_liabilities: 25000,
      }
      setMockDataState({ summary: negativeNetWorthSummary, history: mockHistory })

      renderWidget()

      const netWorthElement = screen.getByText(/-\$10,000/)
      expect(netWorthElement).toHaveClass('text-destructive')
    })

    it('should display assets and liabilities breakdown', () => {
      renderWidget()

      expect(screen.getByText('Assets')).toBeInTheDocument()
      expect(screen.getByText('Liabilities')).toBeInTheDocument()
      expect(screen.getByText(/\$75,000/)).toBeInTheDocument()
      expect(screen.getByText(/\$25,000/)).toBeInTheDocument()
    })

    it('should display period comparison when available', () => {
      renderWidget()

      expect(screen.getByText(/\$5,000/)).toBeInTheDocument()
      expect(screen.getByText(/11\.11%/)).toBeInTheDocument()
    })

    it('renders "View details →" link to /net-worth', () => {
      renderWidget()

      const link = screen.getByRole('link', { name: /view details/i })
      expect(link).toHaveAttribute('href', '/net-worth')
    })
  })

  describe('Sparkline Toggle', () => {
    beforeEach(() => {
      setMockDataState({ summary: mockSummary, history: mockHistory })
    })

    it('should render sparkline toggle button', () => {
      renderWidget()
      expect(screen.getByText('Show Trend')).toBeInTheDocument()
    })

    it('should hide sparkline by default', () => {
      renderWidget()
      expect(screen.queryByTestId('net-worth-sparkline')).not.toBeInTheDocument()
    })

    it('should show sparkline when toggle is clicked', () => {
      renderWidget()

      const toggleButton = screen.getByText('Show Trend')
      fireEvent.click(toggleButton)

      expect(screen.getByText('Hide Trend')).toBeInTheDocument()
      expect(screen.getByTestId('net-worth-sparkline')).toBeInTheDocument()
    })

    it('should have user-friendly tooltip on toggle', () => {
      renderWidget()

      const toggleButton = screen.getByText('Show Trend')
      expect(toggleButton.closest('button')).toHaveAttribute('title', expect.stringContaining('trend'))
    })
  })

  describe('Context Integration', () => {
    it('should use filters from dashboard context', () => {
      setMockDataState({ summary: mockSummary, history: mockHistory })

      renderWidget()

      expect(mockUseNetWorthData).toHaveBeenCalledWith(
        expect.objectContaining({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      )
    })
  })
})
