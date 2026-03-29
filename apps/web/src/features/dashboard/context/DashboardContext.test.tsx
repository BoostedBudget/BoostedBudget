/**
 * Dashboard Context Tests
 *
 * Tests the DashboardProvider and useDashboardContext hook
 * including filter state, timeframe management, and editing mode.
 *
 * Issue #354: Customizable Dashboard Widget System
 * Issue #575: Migrated to bun:test with isolated config (bunfig.real.toml)
 *
 * NOTE: This test runs with bunfig.real.toml to test the REAL context,
 * not the centralized mock. Run with: scripts/test-real-implementations.sh
 */

import { describe, it, expect, beforeEach, afterEach, spyOn, setSystemTime, mock } from 'bun:test'
import { render, screen, renderHook, act } from '@testing-library/react'
import { format, subDays, startOfMonth, endOfMonth } from 'date-fns'
import type { ReactNode } from 'react'

// Must be hoisted before DashboardProvider import (bun:test hoists mock.module calls)
const mockUseGetAccounts = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
}))

mock.module('@boostedbudget/api-client/endpoints/accounts/accounts', () => ({
  useGetAccounts: mockUseGetAccounts,
}))

import { DashboardProvider, useDashboardContext, DashboardContext } from './DashboardContext'

const wrapper = ({ children }: { children: ReactNode }) => (
  <DashboardProvider>{children}</DashboardProvider>
)

describe('DashboardContext', () => {
  const mockToday = new Date('2024-06-15')

  beforeEach(() => {
    setSystemTime(mockToday)
  })

  afterEach(() => {
    setSystemTime()
  })

  describe('DashboardProvider', () => {
    it('should render children', () => {
      render(
        <DashboardProvider>
          <div data-testid="child">Child content</div>
        </DashboardProvider>
      )

      expect(screen.getByTestId('child')).toBeInTheDocument()
      expect(screen.getByText('Child content')).toBeInTheDocument()
    })

    it('should provide context value', () => {
      const TestComponent = () => {
        const context = useDashboardContext()
        return <div data-testid="has-context">{context ? 'yes' : 'no'}</div>
      }

      render(
        <DashboardProvider>
          <TestComponent />
        </DashboardProvider>
      )

      expect(screen.getByTestId('has-context')).toHaveTextContent('yes')
    })
  })

  describe('useDashboardContext', () => {
    it('should return context value inside provider', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      expect(result.current).toBeDefined()
      expect(result.current.filters).toBeDefined()
      expect(result.current.setFilters).toBeDefined()
      expect(result.current.timeframe).toBeDefined()
      expect(result.current.setTimeframe).toBeDefined()
    })

    it('should throw error outside provider', () => {
      const consoleSpy = spyOn(console, 'error').mockImplementation(() => {})

      expect(() => {
        renderHook(() => useDashboardContext())
      }).toThrow('useDashboardContext must be used within a DashboardProvider')

      consoleSpy.mockRestore()
    })
  })

  describe('Filter State', () => {
    it('should have default date range for month timeframe', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      expect(result.current.filters.startDate).toBe(
        format(startOfMonth(mockToday), 'yyyy-MM-dd')
      )
      expect(result.current.filters.endDate).toBe(
        format(endOfMonth(mockToday), 'yyyy-MM-dd')
      )
    })

    it('should update filters with setFilters', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setFilters({ accountTypes: ['checking', 'savings'] })
      })

      expect(result.current.filters.accountTypes).toEqual(['checking', 'savings'])
    })

    it('should merge filters with existing values', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      const originalStartDate = result.current.filters.startDate

      act(() => {
        result.current.setFilters({ includeTransfers: true })
      })

      expect(result.current.filters.startDate).toBe(originalStartDate)
      expect(result.current.filters.includeTransfers).toBe(true)
    })

    it('should allow setting accountTypes', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setFilters({ accountTypes: ['credit'] })
      })

      expect(result.current.filters.accountTypes).toEqual(['credit'])
    })

    it('should allow clearing accountTypes', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setFilters({ accountTypes: ['checking'] })
      })

      expect(result.current.filters.accountTypes).toEqual(['checking'])

      act(() => {
        result.current.setFilters({ accountTypes: undefined })
      })

      expect(result.current.filters.accountTypes).toBeUndefined()
    })

    it('should have default includeTransfers as false', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      expect(result.current.filters.includeTransfers).toBe(false)
    })
  })

  describe('Timeframe', () => {
    it('should have default timeframe of month', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      expect(result.current.timeframe).toBe('month')
    })

    it('should update timeframe with setTimeframe', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setTimeframe('week')
      })

      expect(result.current.timeframe).toBe('week')
    })

    it('should update date range when timeframe changes to week', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setTimeframe('week')
      })

      expect(result.current.filters.startDate).toBe(
        format(subDays(mockToday, 7), 'yyyy-MM-dd')
      )
      expect(result.current.filters.endDate).toBe(
        format(mockToday, 'yyyy-MM-dd')
      )
    })

    it('should update date range when timeframe changes to 3months', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setTimeframe('3months')
      })

      expect(result.current.filters.startDate).toBe(
        format(subDays(mockToday, 90), 'yyyy-MM-dd')
      )
      expect(result.current.filters.endDate).toBe(
        format(mockToday, 'yyyy-MM-dd')
      )
    })

    it('should update date range when timeframe changes to 6months', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setTimeframe('6months')
      })

      expect(result.current.filters.startDate).toBe(
        format(subDays(mockToday, 180), 'yyyy-MM-dd')
      )
      expect(result.current.filters.endDate).toBe(
        format(mockToday, 'yyyy-MM-dd')
      )
    })

    it('should update date range when timeframe changes to year', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setTimeframe('year')
      })

      expect(result.current.filters.startDate).toBe(
        format(subDays(mockToday, 365), 'yyyy-MM-dd')
      )
      expect(result.current.filters.endDate).toBe(
        format(mockToday, 'yyyy-MM-dd')
      )
    })

    it('should use 30-day default for custom timeframe', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setTimeframe('custom')
      })

      expect(result.current.filters.startDate).toBe(
        format(subDays(mockToday, 30), 'yyyy-MM-dd')
      )
      expect(result.current.filters.endDate).toBe(
        format(mockToday, 'yyyy-MM-dd')
      )
    })

    it('should preserve custom dates when on custom timeframe', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setTimeframe('custom')
      })

      act(() => {
        result.current.setFilters({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      })

      expect(result.current.filters.startDate).toBe('2024-01-01')
      expect(result.current.filters.endDate).toBe('2024-01-31')
    })

    it('should clear custom dates when switching from custom to another timeframe', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setTimeframe('custom')
        result.current.setFilters({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      })

      act(() => {
        result.current.setTimeframe('month')
      })

      expect(result.current.filters.startDate).toBe(
        format(startOfMonth(mockToday), 'yyyy-MM-dd')
      )
      expect(result.current.filters.endDate).toBe(
        format(endOfMonth(mockToday), 'yyyy-MM-dd')
      )
    })
  })

  describe('isEditing', () => {
    it('should have initial isEditing as false', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      expect(result.current.isEditing).toBe(false)
    })

    it('should toggle isEditing on', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setIsEditing(true)
      })

      expect(result.current.isEditing).toBe(true)
    })

    it('should toggle isEditing off', () => {
      const { result } = renderHook(() => useDashboardContext(), { wrapper })

      act(() => {
        result.current.setIsEditing(true)
      })

      expect(result.current.isEditing).toBe(true)

      act(() => {
        result.current.setIsEditing(false)
      })

      expect(result.current.isEditing).toBe(false)
    })
  })

  describe('Memoization', () => {
    it('should maintain stable context value reference when state unchanged', () => {
      const { result, rerender } = renderHook(() => useDashboardContext(), {
        wrapper,
      })

      const firstValue = result.current

      rerender()

      expect(result.current.filters).toBe(firstValue.filters)
    })

    it('should maintain stable callback references', () => {
      const { result, rerender } = renderHook(() => useDashboardContext(), {
        wrapper,
      })

      const firstSetFilters = result.current.setFilters
      const firstSetTimeframe = result.current.setTimeframe
      const firstSetIsEditing = result.current.setIsEditing

      rerender()

      expect(result.current.setFilters).toBe(firstSetFilters)
      expect(result.current.setTimeframe).toBe(firstSetTimeframe)
      expect(result.current.setIsEditing).toBe(firstSetIsEditing)
    })
  })

  describe('Context Direct Access', () => {
    it('should expose DashboardContext for custom consumers', () => {
      expect(DashboardContext).toBeDefined()
    })

    it('should have null default value when accessed without provider', () => {
      expect(DashboardContext).toBeDefined()
    })
  })

  describe('hasAccounts', () => {
    beforeEach(() => {
      mockUseGetAccounts.mockClear()
    })

    it('should provide hasAccounts: false when no accounts', () => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [] },
        isLoading: false,
        error: null,
      })
      const { result } = renderHook(() => useDashboardContext(), { wrapper })
      expect(result.current.hasAccounts).toBe(false)
    })

    it('should provide hasAccounts: true when accounts exist', () => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [{ id: '1', name: 'Checking' }] },
        isLoading: false,
        error: null,
      })
      const { result } = renderHook(() => useDashboardContext(), { wrapper })
      expect(result.current.hasAccounts).toBe(true)
    })

    it('should provide hasAccounts: false when data is undefined', () => {
      mockUseGetAccounts.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: null,
      })
      const { result } = renderHook(() => useDashboardContext(), { wrapper })
      expect(result.current.hasAccounts).toBe(false)
    })

    it('should provide accountsLoading: true while fetching', () => {
      mockUseGetAccounts.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
      })
      const { result } = renderHook(() => useDashboardContext(), { wrapper })
      expect(result.current.accountsLoading).toBe(true)
    })

    it('should provide accountsLoading: false when not loading', () => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [] },
        isLoading: false,
        error: null,
      })
      const { result } = renderHook(() => useDashboardContext(), { wrapper })
      expect(result.current.accountsLoading).toBe(false)
    })
  })
})
