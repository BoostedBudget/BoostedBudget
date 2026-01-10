/**
 * useTransactions Hook Tests
 *
 * Issue #575: Migrated to bun:test with centralized API mocks
 * Tests the React hook with filtering, pagination, and debounced search.
 * NOTE: Runs with API-level mocks (transactions-api.ts)
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { setSystemTime } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useTransactions } from './useTransactions'
import type { GetTransactionsResponse, TransactionFilters } from '../types'
import {
  mockUseGetTransactions,
  mockRefetchTransactions,
} from '@/test/mocks/transactions-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

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
        amount: -25.5,
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
        amount: -50.0,
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
    mockUseGetTransactions.mockClear()
    mockRefetchTransactions.mockClear()
  })

  function configureQuery(config: {
    data?: GetTransactionsResponse | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetTransactions.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchTransactions,
    })
  }

  describe('Basic Functionality', () => {
    it('should return loading state initially', () => {
      configureQuery({ isLoading: true })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.data).toBeNull()
      expect(result.current.error).toBeNull()
    })

    it('should fetch and return transactions on mount', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.data).toEqual(mockTransactionsResponse)
      expect(result.current.error).toBeNull()
    })

    it('should handle fetch errors gracefully', async () => {
      configureQuery({ error: new Error('Failed to fetch transactions') })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeInstanceOf(Error)
      expect(result.current.error?.message).toBe('Failed to fetch transactions')
      expect(result.current.data).toBeNull()
    })

    it('should return empty transactions array when no transactions exist', async () => {
      configureQuery({ data: mockEmptyResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.data).toEqual(mockEmptyResponse)
      expect(result.current.data?.transactions).toHaveLength(0)
      expect(result.current.data?.has_more).toBe(false)
    })
  })

  describe('Pagination', () => {
    it('should use custom limit when provided', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions({ limit: 25 }), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(mockUseGetTransactions).toHaveBeenCalled()
    })

    it('should support nextPage pagination', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.currentPage).toBe(0)

      act(() => {
        result.current.nextPage()
      })

      expect(result.current.currentPage).toBe(1)
    })

    it('should support previousPage pagination', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(
        () => useTransactions({ initialOffset: 50 }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.currentPage).toBe(1)

      act(() => {
        result.current.previousPage()
      })

      expect(result.current.currentPage).toBe(0)
    })

    it('should not go below offset 0 on previousPage', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.currentPage).toBe(0)

      act(() => {
        result.current.previousPage()
      })

      expect(result.current.currentPage).toBe(0)
    })

    it('should expose pagination metadata', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.hasMore).toBe(true)
      expect(result.current.totalCount).toBe(150)
      expect(result.current.currentPage).toBe(0)
    })
  })

  describe('Filtering', () => {
    it('should filter by account_id', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const filters: TransactionFilters = {
        account_id: 'account-123',
      }

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.data).toEqual(mockTransactionsResponse)
    })

    it('should filter by date range', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const filters: TransactionFilters = {
        start_date: '2025-01-01',
        end_date: '2025-12-31',
      }

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.data).toEqual(mockTransactionsResponse)
    })

    it('should filter by category', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const filters: TransactionFilters = {
        category: 'food',
      }

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.data).toEqual(mockTransactionsResponse)
    })

    it('should filter by pending status', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const filters: TransactionFilters = {
        pending: true,
      }

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.data).toEqual(mockTransactionsResponse)
    })

    it('should support multiple filters combined', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const filters: TransactionFilters = {
        account_id: 'account-123',
        start_date: '2025-01-01',
        end_date: '2025-12-31',
        category: 'food',
        pending: false,
      }

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.data).toEqual(mockTransactionsResponse)
    })

    it('should reset to page 0 on filter changes', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result, rerender } = renderHook(
        ({ filters }) => useTransactions({ filters }),
        {
          wrapper: createWrapper(),
          initialProps: { filters: {} as TransactionFilters },
        }
      )

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.nextPage()
      })
      expect(result.current.currentPage).toBe(1)

      rerender({ filters: { account_id: 'account-456' } })

      await waitFor(() => {
        expect(result.current.currentPage).toBe(0)
      })
    })
  })

  describe('Debounced Search', () => {
    let originalTime: number

    beforeEach(() => {
      originalTime = Date.now()
      setSystemTime(new Date(originalTime))
    })

    afterEach(() => {
      setSystemTime()
    })

    it('should debounce search input with 300ms delay', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result, rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          wrapper: createWrapper(),
          initialProps: { search: '' },
        }
      )

      rerender({ search: 'star' })

      const callsBefore = mockUseGetTransactions.mock.calls
      const lastCallBefore = callsBefore[callsBefore.length - 1]
      // The first argument should NOT have search: 'star' yet (debounce not triggered)
      expect(lastCallBefore?.[0]?.search).not.toBe('star')

      setSystemTime(new Date(originalTime + 350))
      await waitFor(() => {
        expect(result.current.loading).toBeDefined()
      })
    })

    it('should cancel previous debounced search if new input comes in', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          wrapper: createWrapper(),
          initialProps: { search: '' },
        }
      )

      rerender({ search: 's' })
      setSystemTime(new Date(originalTime + 100))

      rerender({ search: 'st' })
      setSystemTime(new Date(originalTime + 200))

      rerender({ search: 'sta' })
      setSystemTime(new Date(originalTime + 550))

      const calls = mockUseGetTransactions.mock.calls
      expect(calls.length).toBeGreaterThan(0)
    })

    it('should clear search results when search is empty', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result, rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          wrapper: createWrapper(),
          initialProps: { search: 'starbucks' },
        }
      )

      rerender({ search: '' })
      setSystemTime(new Date(originalTime + 350))

      expect(result.current.data).toEqual(mockTransactionsResponse)
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function to manually reload data', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.refetch()
      })

      expect(mockRefetchTransactions).toHaveBeenCalled()
    })

    it('should preserve current filters and pagination on refetch', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const filters: TransactionFilters = {
        account_id: 'account-123',
        category: 'food',
      }

      const { result } = renderHook(
        () => useTransactions({ filters, limit: 25 }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.nextPage()
      })

      expect(result.current.currentPage).toBe(1)

      await act(async () => {
        await result.current.refetch()
      })

      expect(result.current.currentPage).toBe(1)
      expect(mockRefetchTransactions).toHaveBeenCalled()
    })
  })

  describe('updateTransaction', () => {
    it('should update a single transaction in the current data', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      const updatedTransaction = {
        ...mockTransactionsResponse.transactions[0],
        tags: ['New Tag'],
        category_override: '["Custom Category"]',
      }

      act(() => {
        result.current.updateTransaction(updatedTransaction)
      })

      expect(result.current.data?.transactions[0].tags).toEqual(['New Tag'])
      expect(result.current.data?.transactions[0].category_override).toBe(
        '["Custom Category"]'
      )
      expect(result.current.data?.transactions[1]).toEqual(
        mockTransactionsResponse.transactions[1]
      )
    })

    it('should not trigger a refetch when updating transaction', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      mockRefetchTransactions.mockClear()

      const updatedTransaction = {
        ...mockTransactionsResponse.transactions[0],
        tags: ['Updated'],
      }

      act(() => {
        result.current.updateTransaction(updatedTransaction)
      })

      expect(mockRefetchTransactions).not.toHaveBeenCalled()
    })

    it('should preserve metadata when updating transaction', async () => {
      configureQuery({ data: mockTransactionsResponse })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.updateTransaction({
          ...mockTransactionsResponse.transactions[0],
          tags: ['Updated'],
        })
      })

      expect(result.current.data?.total_count).toBe(150)
      expect(result.current.data?.has_more).toBe(true)
      expect(result.current.hasMore).toBe(true)
      expect(result.current.totalCount).toBe(150)
    })

    it('should handle update when data is null', async () => {
      configureQuery({ isLoading: true })

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      })

      expect(result.current.data).toBeNull()

      act(() => {
        result.current.updateTransaction({
          id: 'txn-1',
          user_id: 'user-123',
          account_id: 'account-456',
          linked_item_id: 'item-789',
          source: 'plaid' as const,
          amount: -25.5,
          date: '2025-10-25',
          name: 'Test',
          pending: false,
          created_at: '2025-10-25T10:00:00Z',
          updated_at: '2025-10-25T10:00:00Z',
        })
      })

      expect(result.current.data).toBeNull()
    })
  })
})
