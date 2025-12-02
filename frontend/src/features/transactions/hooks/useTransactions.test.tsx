/**
 * useTransactions Hook Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the React hook with filtering, pagination, and debounced search.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useTransactions } from './useTransactions'
import * as transactionsApi from '../api/transactionsApi'
import type { GetTransactionsResponse, TransactionFilters } from '../types'

// Mock the API module
vi.mock('../api/transactionsApi')

describe('useTransactions', () => {
  const mockTransactionsResponse: GetTransactionsResponse = {
    transactions: [
      {
        id: 'txn-1',
        user_id: 'user-123',
        account_id: 'account-456',
        linked_item_id: 'item-789',
        source: 'plaid' as const,
        external_transaction_id: 'plaid-txn-abc',
        amount: -25.50,
        date: '2025-10-25',
        name: 'Starbucks',
        merchant_name: 'Starbucks Coffee',
        category: '["Food and Drink", "Coffee"]',
        pending: false,
        created_at: '2025-10-25T10:00:00Z',
        updated_at: '2025-10-25T10:00:00Z',
      },
      {
        id: 'txn-2',
        user_id: 'user-123',
        account_id: 'account-456',
        linked_item_id: 'item-789',
        source: 'plaid' as const,
        amount: -50.00,
        date: '2025-10-24',
        name: 'Amazon',
        merchant_name: 'Amazon.com',
        category: '["Shopping", "General Merchandise"]',
        pending: false,
        created_at: '2025-10-24T15:00:00Z',
        updated_at: '2025-10-24T15:00:00Z',
      },
    ],
    total_count: 150,
    limit: 50,
    offset: 0,
    has_more: true,
  }

  const mockEmptyResponse: GetTransactionsResponse = {
    transactions: [],
    total_count: 0,
    limit: 50,
    offset: 0,
    has_more: false,
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Basic Functionality', () => {
    it('should return loading state initially', () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      // Act
      const { result } = renderHook(() => useTransactions())

      // Assert
      expect(result.current.loading).toBe(true)
      expect(result.current.data).toBeNull()
      expect(result.current.error).toBeNull()
    })

    it('should fetch and return transactions on mount', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      // Assert - Initial loading state
      expect(result.current.loading).toBe(true)

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert - Data loaded successfully
      expect(result.current.data).toEqual(mockTransactionsResponse)
      expect(result.current.error).toBeNull()
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(1)
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledWith({
        limit: 50,
        offset: 0,
        filters: {},
      })
    })

    it('should handle fetch errors gracefully', async () => {
      // Arrange
      const error = new Error('Failed to fetch transactions')
      vi.mocked(transactionsApi.fetchTransactions).mockRejectedValue(error)

      // Act
      const { result } = renderHook(() => useTransactions())

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.error).toEqual(error)
      expect(result.current.data).toBeNull()
    })

    it('should return empty transactions array when no transactions exist', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockEmptyResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.data).toEqual(mockEmptyResponse)
      expect(result.current.data?.transactions).toHaveLength(0)
      expect(result.current.data?.has_more).toBe(false)
    })
  })

  describe('Pagination', () => {
    it('should use custom limit when provided', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions({ limit: 25 }))

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledWith({
        limit: 25,
        offset: 0,
        filters: {},
      })
    })

    it('should support nextPage pagination', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Call nextPage
      act(() => {
        result.current.nextPage()
      })

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(2)
      })

      // Assert
      expect(transactionsApi.fetchTransactions).toHaveBeenLastCalledWith({
        limit: 50,
        offset: 50, // Should increment by limit
        filters: {},
      })
    })

    it('should support previousPage pagination', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions({ initialOffset: 50 }))

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Call previousPage
      act(() => {
        result.current.previousPage()
      })

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(2)
      })

      // Assert
      expect(transactionsApi.fetchTransactions).toHaveBeenLastCalledWith({
        limit: 50,
        offset: 0, // Should decrement by limit
        filters: {},
      })
    })

    it('should not go below offset 0 on previousPage', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Call previousPage when already at offset 0
      act(() => {
        result.current.previousPage()
      })

      // Assert - Should still be at offset 0, no additional fetch
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(1)
    })

    it('should expose pagination metadata', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(result.current.hasMore).toBe(true)
      expect(result.current.totalCount).toBe(150)
      expect(result.current.currentPage).toBe(0)
    })
  })

  describe('Filtering', () => {
    it('should filter by account_id', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      const filters: TransactionFilters = {
        account_id: 'account-123',
      }

      // Act
      const { result } = renderHook(() => useTransactions({ filters }))

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledWith({
        limit: 50,
        offset: 0,
        filters: {
          account_id: 'account-123',
        },
      })
    })

    it('should filter by date range', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      const filters: TransactionFilters = {
        start_date: '2025-01-01',
        end_date: '2025-12-31',
      }

      // Act
      const { result } = renderHook(() => useTransactions({ filters }))

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledWith({
        limit: 50,
        offset: 0,
        filters: {
          start_date: '2025-01-01',
          end_date: '2025-12-31',
        },
      })
    })

    it('should filter by category', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      const filters: TransactionFilters = {
        category: 'food',
      }

      // Act
      const { result } = renderHook(() => useTransactions({ filters }))

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledWith({
        limit: 50,
        offset: 0,
        filters: {
          category: 'food',
        },
      })
    })

    it('should filter by pending status', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      const filters: TransactionFilters = {
        pending: true,
      }

      // Act
      const { result } = renderHook(() => useTransactions({ filters }))

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledWith({
        limit: 50,
        offset: 0,
        filters: {
          pending: true,
        },
      })
    })

    it('should support multiple filters combined', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      const filters: TransactionFilters = {
        account_id: 'account-123',
        start_date: '2025-01-01',
        end_date: '2025-12-31',
        category: 'food',
        pending: false,
      }

      // Act
      const { result } = renderHook(() => useTransactions({ filters }))

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Assert
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledWith({
        limit: 50,
        offset: 0,
        filters: {
          account_id: 'account-123',
          start_date: '2025-01-01',
          end_date: '2025-12-31',
          category: 'food',
          pending: false,
        },
      })
    })

    it('should refetch when filters change', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act - Initial render with no filters
      const { result, rerender } = renderHook(
        ({ filters }) => useTransactions({ filters }),
        {
          initialProps: { filters: {} as TransactionFilters },
        }
      )

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(1)

      // Re-render with new filters
      rerender({ filters: { account_id: 'account-456' } })

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(2)
      })

      // Assert - Should fetch with new filters
      expect(transactionsApi.fetchTransactions).toHaveBeenLastCalledWith({
        limit: 50,
        offset: 0, // Reset to 0 on filter change
        filters: {
          account_id: 'account-456',
        },
      })
    })
  })

  describe('Debounced Search', () => {
    it('should debounce search input with 300ms delay', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act - Initial render
      const { result, rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          initialProps: { search: '' },
        }
      )

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Initial fetch without search
      const initialCallCount = vi.mocked(transactionsApi.fetchTransactions).mock.calls.length

      // Update search rapidly - should NOT fetch immediately
      rerender({ search: 'star' })

      // Immediately after rerender, should not have fetched yet (debounced)
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(initialCallCount)

      // Wait for debounce delay
      await new Promise((resolve) => setTimeout(resolve, 350))

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(initialCallCount + 1)
      })

      // Assert - Should fetch with debounced search
      expect(transactionsApi.fetchTransactions).toHaveBeenLastCalledWith({
        limit: 50,
        offset: 0,
        filters: {
          search: 'star',
        },
      })
    })

    it('should cancel previous debounced search if new input comes in', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result, rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          initialProps: { search: '' },
        }
      )

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const initialCallCount = vi.mocked(transactionsApi.fetchTransactions).mock.calls.length

      // Type quickly - each should cancel the previous debounce
      rerender({ search: 's' })
      await new Promise((resolve) => setTimeout(resolve, 100))

      rerender({ search: 'st' })
      await new Promise((resolve) => setTimeout(resolve, 100))

      rerender({ search: 'sta' })

      // Should NOT have fetched yet (still debouncing)
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(initialCallCount)

      // Wait for final debounce
      await new Promise((resolve) => setTimeout(resolve, 350))

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(initialCallCount + 1)
      })

      // Assert - Should only fetch once with final search term
      expect(transactionsApi.fetchTransactions).toHaveBeenLastCalledWith({
        limit: 50,
        offset: 0,
        filters: {
          search: 'sta',
        },
      })
    })

    it('should clear search results when search is empty', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act - Start with search
      const { result, rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          initialProps: { search: 'starbucks' },
        }
      )

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const callCountAfterFirst = vi.mocked(transactionsApi.fetchTransactions).mock.calls.length

      // Clear search
      rerender({ search: '' })

      // Wait for debounce
      await new Promise((resolve) => setTimeout(resolve, 350))

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(callCountAfterFirst + 1)
      })

      // Assert - Should fetch without search filter
      expect(transactionsApi.fetchTransactions).toHaveBeenLastCalledWith({
        limit: 50,
        offset: 0,
        filters: {},
      })
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function to manually reload data', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(1)

      // Call refetch
      act(() => {
        result.current.refetch()
      })

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(2)
      })

      // Assert - Should fetch with same parameters
      expect(transactionsApi.fetchTransactions).toHaveBeenLastCalledWith({
        limit: 50,
        offset: 0,
        filters: {},
      })
    })

    it('should preserve current filters and pagination on refetch', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      const filters: TransactionFilters = {
        account_id: 'account-123',
        category: 'food',
      }

      // Act
      const { result } = renderHook(() => useTransactions({ filters, limit: 25 }))

      await waitFor(
        () => {
          expect(result.current.loading).toBe(false)
        },
        { timeout: 5000 }
      )

      // Go to next page
      act(() => {
        result.current.nextPage()
      })

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(2)
      })

      // Refetch
      act(() => {
        result.current.refetch()
      })

      await waitFor(() => {
        expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(3)
      })

      // Assert - Should maintain current offset and filters
      expect(transactionsApi.fetchTransactions).toHaveBeenLastCalledWith({
        limit: 25,
        offset: 25, // Should preserve current offset
        filters: {
          account_id: 'account-123',
          category: 'food',
        },
      })
    })
  })

  describe('updateTransaction', () => {
    it('should update a single transaction in the current data', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Update first transaction with new tags
      const updatedTransaction = {
        ...mockTransactionsResponse.transactions[0],
        tags: ['New Tag'],
        category_override: '["Custom Category"]',
      }

      act(() => {
        result.current.updateTransaction(updatedTransaction)
      })

      // Assert - Transaction should be updated in place
      expect(result.current.data?.transactions[0].tags).toEqual(['New Tag'])
      expect(result.current.data?.transactions[0].category_override).toBe('["Custom Category"]')
      // Second transaction should remain unchanged
      expect(result.current.data?.transactions[1]).toEqual(mockTransactionsResponse.transactions[1])
    })

    it('should not trigger a refetch when updating transaction', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const callCountAfterLoad = vi.mocked(transactionsApi.fetchTransactions).mock.calls.length

      // Update transaction
      const updatedTransaction = {
        ...mockTransactionsResponse.transactions[0],
        tags: ['Updated'],
      }

      act(() => {
        result.current.updateTransaction(updatedTransaction)
      })

      // Assert - No additional fetch should be made
      expect(transactionsApi.fetchTransactions).toHaveBeenCalledTimes(callCountAfterLoad)
    })

    it('should preserve metadata when updating transaction', async () => {
      // Arrange
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      // Act
      const { result } = renderHook(() => useTransactions())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Update transaction
      act(() => {
        result.current.updateTransaction({
          ...mockTransactionsResponse.transactions[0],
          tags: ['Updated'],
        })
      })

      // Assert - Metadata should be preserved
      expect(result.current.data?.total_count).toBe(150)
      expect(result.current.data?.has_more).toBe(true)
      expect(result.current.hasMore).toBe(true)
      expect(result.current.totalCount).toBe(150)
    })

    it('should handle update when data is null', async () => {
      // Arrange - Never resolve to keep data null
      vi.mocked(transactionsApi.fetchTransactions).mockImplementation(
        () => new Promise(() => {})
      )

      // Act
      const { result } = renderHook(() => useTransactions())

      // Data should be null
      expect(result.current.data).toBeNull()

      // Try to update - should not throw
      act(() => {
        result.current.updateTransaction({
          id: 'txn-1',
          user_id: 'user-123',
          account_id: 'account-456',
          linked_item_id: 'item-789',
          source: 'plaid' as const,
          amount: -25.50,
          date: '2025-10-25',
          name: 'Test',
          pending: false,
          created_at: '2025-10-25T10:00:00Z',
          updated_at: '2025-10-25T10:00:00Z',
        })
      })

      // Data should still be null
      expect(result.current.data).toBeNull()
    })
  })
})
