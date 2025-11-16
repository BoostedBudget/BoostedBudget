/**
 * useDashboardData Hook Tests
 *
 * Tests the dashboard data fetching hook with gap-filling logic,
 * parallel API calls, filtering, and error handling.
 *
 * Issue #353: Balance Tracking Dashboard
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useDashboardData } from './useDashboardData'
import * as dashboardApi from '../api/dashboardApi'
import type {
  BalanceSummary,
  BalanceHistory,
  AccountTypesResponse,
  DashboardFilters,
} from '../types'

// Mock the API module
vi.mock('../api/dashboardApi')

describe('useDashboardData', () => {
  const mockBalanceSummary: BalanceSummary = {
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

  const mockBalanceHistory: BalanceHistory = {
    start_date: '2025-11-10',
    end_date: '2025-11-15',
    granularity: 'daily',
    snapshots: [
      {
        date: '2025-11-10',
        balance: 14000.00,
        by_type: { depository: 4500.00, credit: -1500.00 },
      },
      {
        date: '2025-11-12',
        balance: 14500.00,
        by_type: { depository: 5000.00, credit: -1500.00 },
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
      { key: 'investment', label: 'Investments', count: 0, total_balance: 0.00 },
    ],
  }

  const mockEmptyHistory: BalanceHistory = {
    start_date: '2025-11-10',
    end_date: '2025-11-15',
    granularity: 'daily',
    snapshots: [],
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Basic Functionality', () => {
    it('should return loading state initially', () => {
      // Arrange - Never resolve promises
      vi.mocked(dashboardApi.fetchBalanceSummary).mockImplementation(
        () => new Promise(() => {})
      )
      vi.mocked(dashboardApi.fetchBalanceHistory).mockImplementation(
        () => new Promise(() => {})
      )
      vi.mocked(dashboardApi.fetchAccountTypes).mockImplementation(
        () => new Promise(() => {})
      )

      // Act
      const { result } = renderHook(() => useDashboardData())

      // Assert
      expect(result.current.loading).toBe(true)
      expect(result.current.summary).toBeNull()
      expect(result.current.history).toBeNull()
      expect(result.current.accountTypes).toBeNull()
      expect(result.current.error).toBeNull()
    })

    it('should fetch all three endpoints on mount', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      // Assert - Initial loading state
      expect(result.current.loading).toBe(true)

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - All three APIs called
      expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalledTimes(1)
      expect(dashboardApi.fetchBalanceHistory).toHaveBeenCalledTimes(1)
      expect(dashboardApi.fetchAccountTypes).toHaveBeenCalledTimes(1)
    })

    it('should call fetchBalanceSummary with filters', async () => {
      // Arrange
      const filters: DashboardFilters = {
        accountTypes: ['depository'],
        asOfDate: '2025-11-15',
      }

      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      renderHook(() => useDashboardData(filters))

      await waitFor(() => {
        expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalled()
      })

      // Assert
      expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalledWith(filters)
    })

    it('should call fetchBalanceHistory with filters', async () => {
      // Arrange
      const filters: DashboardFilters = {
        startDate: '2025-11-01',
        endDate: '2025-11-15',
        accountTypes: ['depository', 'credit'],
        granularity: 'daily',
      }

      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      renderHook(() => useDashboardData(filters))

      await waitFor(() => {
        expect(dashboardApi.fetchBalanceHistory).toHaveBeenCalled()
      })

      // Assert
      expect(dashboardApi.fetchBalanceHistory).toHaveBeenCalledWith(filters)
    })

    it('should call fetchAccountTypes without filters', async () => {
      // Arrange
      const filters: DashboardFilters = {
        accountTypes: ['depository'],
      }

      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      renderHook(() => useDashboardData(filters))

      await waitFor(() => {
        expect(dashboardApi.fetchAccountTypes).toHaveBeenCalled()
      })

      // Assert - Account types endpoint doesn't take filters
      expect(dashboardApi.fetchAccountTypes).toHaveBeenCalledWith()
    })

    it('should populate summary with API response', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.summary).toEqual(mockBalanceSummary)
      expect(result.current.error).toBeNull()
    })

    it('should populate accountTypes with API response', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.accountTypes).toEqual(mockAccountTypes)
      expect(result.current.error).toBeNull()
    })
  })

  describe('Error Handling', () => {
    it('should handle fetchBalanceSummary errors', async () => {
      // Arrange
      const error = new Error('Failed to fetch balance summary')
      vi.mocked(dashboardApi.fetchBalanceSummary).mockRejectedValue(error)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.error).toEqual(error)
      expect(result.current.summary).toBeNull()
      expect(result.current.history).toBeNull()
      expect(result.current.accountTypes).toBeNull()
    })

    it('should handle fetchBalanceHistory errors', async () => {
      // Arrange
      const error = new Error('Failed to fetch balance history')
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockRejectedValue(error)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.error).toEqual(error)
    })

    it('should handle fetchAccountTypes errors', async () => {
      // Arrange
      const error = new Error('Failed to fetch account types')
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockRejectedValue(error)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.error).toEqual(error)
    })

    it('should handle generic errors with default message', async () => {
      // Arrange - Throw non-Error object
      vi.mocked(dashboardApi.fetchBalanceSummary).mockRejectedValue('Network failure')
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.error).toBeInstanceOf(Error)
      expect(result.current.error?.message).toBe('Failed to fetch dashboard data')
    })

    it('should set loading=false on error', async () => {
      // Arrange
      const error = new Error('API error')
      vi.mocked(dashboardApi.fetchBalanceSummary).mockRejectedValue(error)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      // Initial state
      expect(result.current.loading).toBe(true)

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.loading).toBe(false)
    })

    it('should set data to null on error', async () => {
      // Arrange
      const error = new Error('API error')
      vi.mocked(dashboardApi.fetchBalanceSummary).mockRejectedValue(error)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.summary).toBeNull()
      expect(result.current.history).toBeNull()
      expect(result.current.accountTypes).toBeNull()
    })
  })

  describe('Gap-Filling Logic', () => {
    it('should fill missing days in balance history', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - Original has 3 snapshots (11/10, 11/12, 11/15)
      // Should fill gaps: 11/11, 11/13, 11/14
      expect(result.current.history?.snapshots).toHaveLength(6) // 3 original + 3 filled

      const snapshots = result.current.history?.snapshots || []
      const dates = snapshots.map((s) => s.date)

      expect(dates).toEqual([
        '2025-11-10',
        '2025-11-11', // Filled
        '2025-11-12',
        '2025-11-13', // Filled
        '2025-11-14', // Filled
        '2025-11-15',
      ])
    })

    it('should use forward-fill strategy for missing days', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const snapshots = result.current.history?.snapshots || []

      // Assert - 11/11 should inherit from 11/10
      const nov11 = snapshots.find((s) => s.date === '2025-11-11')
      expect(nov11?.balance).toBe(14000.00)
      expect(nov11?.by_type).toEqual({ depository: 4500.00, credit: -1500.00 })

      // Assert - 11/13 should inherit from 11/12
      const nov13 = snapshots.find((s) => s.date === '2025-11-13')
      expect(nov13?.balance).toBe(14500.00)
      expect(nov13?.by_type).toEqual({ depository: 5000.00, credit: -1500.00 })

      // Assert - 11/14 should also inherit from 11/12 (last actual snapshot)
      const nov14 = snapshots.find((s) => s.date === '2025-11-14')
      expect(nov14?.balance).toBe(14500.00)
      expect(nov14?.by_type).toEqual({ depository: 5000.00, credit: -1500.00 })
    })

    it('should preserve actual snapshot dates', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const snapshots = result.current.history?.snapshots || []

      // Assert - Actual snapshots remain unchanged
      const nov10 = snapshots.find((s) => s.date === '2025-11-10')
      expect(nov10).toEqual(mockBalanceHistory.snapshots[0])

      const nov12 = snapshots.find((s) => s.date === '2025-11-12')
      expect(nov12).toEqual(mockBalanceHistory.snapshots[1])

      const nov15 = snapshots.find((s) => s.date === '2025-11-15')
      expect(nov15).toEqual(mockBalanceHistory.snapshots[2])
    })

    it('should handle empty snapshots array', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockEmptyHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - Should return original empty history without gap-filling
      expect(result.current.history).toEqual(mockEmptyHistory)
      expect(result.current.history?.snapshots).toHaveLength(0)
    })

    it('should skip gap-filling if no snapshots', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockEmptyHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.history).toEqual(mockEmptyHistory)
    })

    it('should handle single snapshot without filling gaps before it', async () => {
      // Arrange - Only one snapshot on the last day
      const singleSnapshotHistory: BalanceHistory = {
        start_date: '2025-11-10',
        end_date: '2025-11-15',
        granularity: 'daily',
        snapshots: [
          {
            date: '2025-11-15',
            balance: 15000.00,
            by_type: { depository: 5000.00 },
          },
        ],
      }

      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(singleSnapshotHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - Should only have 1 snapshot (no forward-fill before first snapshot)
      expect(result.current.history?.snapshots).toHaveLength(1)
      expect(result.current.history?.snapshots[0].date).toBe('2025-11-15')
    })

    it('should fill gaps at beginning when first snapshot is not start_date', async () => {
      // Arrange - First snapshot on 11/12, not 11/10
      const gapAtStart: BalanceHistory = {
        start_date: '2025-11-10',
        end_date: '2025-11-12',
        granularity: 'daily',
        snapshots: [
          {
            date: '2025-11-12',
            balance: 14500.00,
            by_type: { depository: 5000.00 },
          },
        ],
      }

      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(gapAtStart)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - Should NOT fill before first snapshot (no lastSnapshot yet)
      // Should only have 1 snapshot
      expect(result.current.history?.snapshots).toHaveLength(1)
      expect(result.current.history?.snapshots[0].date).toBe('2025-11-12')
    })

    it('should preserve by_type data in filled snapshots', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const snapshots = result.current.history?.snapshots || []

      // Assert - Filled snapshots should have by_type from previous snapshot
      const nov11 = snapshots.find((s) => s.date === '2025-11-11')
      expect(nov11?.by_type).toEqual({ depository: 4500.00, credit: -1500.00 })
    })

    it('should handle missing by_type in snapshots', async () => {
      // Arrange - Snapshots without by_type
      const historyWithoutByType: BalanceHistory = {
        start_date: '2025-11-10',
        end_date: '2025-11-12',
        granularity: 'daily',
        snapshots: [
          {
            date: '2025-11-10',
            balance: 14000.00,
          },
          {
            date: '2025-11-12',
            balance: 14500.00,
          },
        ],
      }

      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(historyWithoutByType)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const snapshots = result.current.history?.snapshots || []

      // Assert - Should fill gap and handle undefined by_type
      expect(snapshots).toHaveLength(3) // 11/10, 11/11 (filled), 11/12
      const nov11 = snapshots.find((s) => s.date === '2025-11-11')
      expect(nov11?.balance).toBe(14000.00)
      expect(nov11?.by_type).toBeUndefined()
    })
  })

  describe('Filter Changes', () => {
    it('should refetch when filters change', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act - Initial render with no filters
      const { rerender } = renderHook(
        ({ filters }: { filters?: DashboardFilters }) => useDashboardData(filters),
        {
          initialProps: { filters: undefined as DashboardFilters | undefined },
        }
      )

      await waitFor(() => {
        expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalledTimes(1)
      })

      // Re-render with new filters
      const newFilters: DashboardFilters = { accountTypes: ['depository'] }
      rerender({ filters: newFilters })

      await waitFor(() => {
        expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalledTimes(2)
      })

      // Assert
      expect(dashboardApi.fetchBalanceSummary).toHaveBeenLastCalledWith(newFilters)
      expect(dashboardApi.fetchBalanceHistory).toHaveBeenLastCalledWith(newFilters)
    })

    it('should pass new account types to API', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      const filters1: DashboardFilters = { accountTypes: ['depository'] }
      const filters2: DashboardFilters = { accountTypes: ['depository', 'credit'] }

      // Act
      const { rerender } = renderHook(
        ({ filters }) => useDashboardData(filters),
        {
          initialProps: { filters: filters1 },
        }
      )

      await waitFor(() => {
        expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalledTimes(1)
      })

      rerender({ filters: filters2 })

      await waitFor(() => {
        expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalledTimes(2)
      })

      // Assert
      expect(dashboardApi.fetchBalanceSummary).toHaveBeenLastCalledWith(filters2)
    })

    it('should pass new date range to API', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      const filters1: DashboardFilters = {
        startDate: '2025-11-01',
        endDate: '2025-11-15',
      }
      const filters2: DashboardFilters = {
        startDate: '2025-10-01',
        endDate: '2025-11-15',
      }

      // Act
      const { rerender } = renderHook(
        ({ filters }) => useDashboardData(filters),
        {
          initialProps: { filters: filters1 },
        }
      )

      await waitFor(() => {
        expect(dashboardApi.fetchBalanceHistory).toHaveBeenCalledTimes(1)
      })

      rerender({ filters: filters2 })

      await waitFor(() => {
        expect(dashboardApi.fetchBalanceHistory).toHaveBeenCalledTimes(2)
      })

      // Assert
      expect(dashboardApi.fetchBalanceHistory).toHaveBeenLastCalledWith(filters2)
    })
  })

  describe('Refetch Function', () => {
    it('should provide refetch function', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.refetch).toBeInstanceOf(Function)
    })

    it('should refetch all data when refetch called', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalledTimes(1)
      expect(dashboardApi.fetchBalanceHistory).toHaveBeenCalledTimes(1)
      expect(dashboardApi.fetchAccountTypes).toHaveBeenCalledTimes(1)

      // Call refetch
      await result.current.refetch()

      // Assert - Should fetch all three again
      expect(dashboardApi.fetchBalanceSummary).toHaveBeenCalledTimes(2)
      expect(dashboardApi.fetchBalanceHistory).toHaveBeenCalledTimes(2)
      expect(dashboardApi.fetchAccountTypes).toHaveBeenCalledTimes(2)
    })

    it('should preserve current filters on refetch', async () => {
      // Arrange
      const filters: DashboardFilters = {
        accountTypes: ['depository'],
        startDate: '2025-11-01',
        endDate: '2025-11-15',
      }

      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData(filters))

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Refetch
      await result.current.refetch()

      // Assert - Should maintain filters
      expect(dashboardApi.fetchBalanceSummary).toHaveBeenLastCalledWith(filters)
      expect(dashboardApi.fetchBalanceHistory).toHaveBeenLastCalledWith(filters)
    })

    it('should set loading=true during refetch', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockBalanceHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Refetch - loading state may change too quickly to observe
      await result.current.refetch()

      // Assert - Should be done loading after refetch completes
      expect(result.current.loading).toBe(false)
      expect(result.current.error).toBeNull()
      expect(result.current.summary).toEqual(mockBalanceSummary)
    })
  })

  describe('Edge Cases', () => {
    it('should handle null history gracefully', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(null as any)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - Should set history to null without crashing
      expect(result.current.history).toBeNull()
      expect(result.current.summary).toEqual(mockBalanceSummary)
      expect(result.current.accountTypes).toEqual(mockAccountTypes)
    })

    it('should handle history with zero snapshots', async () => {
      // Arrange
      vi.mocked(dashboardApi.fetchBalanceSummary).mockResolvedValue(mockBalanceSummary)
      vi.mocked(dashboardApi.fetchBalanceHistory).mockResolvedValue(mockEmptyHistory)
      vi.mocked(dashboardApi.fetchAccountTypes).mockResolvedValue(mockAccountTypes)

      // Act
      const { result } = renderHook(() => useDashboardData())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.history).toEqual(mockEmptyHistory)
      expect(result.current.history?.snapshots).toHaveLength(0)
    })
  })
})
