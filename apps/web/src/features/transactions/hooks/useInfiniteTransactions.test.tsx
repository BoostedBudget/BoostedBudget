/**
 * useInfiniteTransactions Hook Tests
 *
 * Issue #1128: Cursor-based infinite scroll for transactions
 * Tests the infinite query hook with cursor pagination, filter resets, and cache surgery.
 * NOTE: Runs with API-level mocks (getTransactions raw fetcher)
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useInfiniteTransactions } from './useInfiniteTransactions'
import type { TransactionFiltersState } from './useInfiniteTransactions'
import { mockGetTransactions } from '@/test/mocks/transactions-api'
import type { GetTransactionsOutputBody } from '@boostedbudget/api-client/models/getTransactionsOutputBody'
import type { Transaction } from '../types'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const makeTxn = (id: string): Transaction => ({
  id,
  user_id: 'user-123',
  account_id: 'account-456',
  linked_item_id: 'item-789',
  source: 'plaid' as const,
  external_transaction_id: `ext-${id}`,
  amount: -25.5,
  date: '2025-10-25',
  name: `Transaction ${id}`,
  merchant_name: 'Test Merchant',
  pending: false,
  created_at: '2025-10-25T10:00:00Z',
  updated_at: '2025-10-25T10:00:00Z',
})

const page1Response: GetTransactionsOutputBody = {
  transactions: [makeTxn('txn-1'), makeTxn('txn-2')],
  limit: 50,
  next_cursor: 'cursor1',
}

const page2Response: GetTransactionsOutputBody = {
  transactions: [makeTxn('txn-3'), makeTxn('txn-4')],
  limit: 50,
  next_cursor: 'cursor2',
}

const lastPageResponse: GetTransactionsOutputBody = {
  transactions: [makeTxn('txn-5')],
  limit: 50,
  next_cursor: undefined,
}

const defaultFilters: TransactionFiltersState = {}
const defaultOpts = {
  filters: defaultFilters,
  sortBy: 'date' as const,
  sortDir: 'desc' as const,
}

describe('useInfiniteTransactions', () => {
  beforeEach(() => {
    mockGetTransactions.mockClear()
  })

  describe('First page (no cursor)', () => {
    it('returns transactions from page 1 and hasNextPage: true when next_cursor is present', async () => {
      mockGetTransactions.mockResolvedValue(page1Response)

      const { result } = renderHook(() => useInfiniteTransactions(defaultOpts), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(result.current.transactions).toHaveLength(2)
      expect(result.current.transactions[0].id).toBe('txn-1')
      expect(result.current.transactions[1].id).toBe('txn-2')
      expect(result.current.hasNextPage).toBe(true)
      // First fetch should have no cursor (initialPageParam is undefined)
      expect(mockGetTransactions).toHaveBeenCalled()
      const firstCallParams = mockGetTransactions.mock.calls[0][0] as Record<string, unknown>
      expect(firstCallParams.cursor).toBeUndefined()
    })
  })

  describe('Second page (fetchNextPage)', () => {
    it('calls getTransactions with cursor from first page and combines rows', async () => {
      mockGetTransactions
        .mockResolvedValueOnce(page1Response)
        .mockResolvedValueOnce(page2Response)

      const { result } = renderHook(() => useInfiniteTransactions(defaultOpts), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))
      expect(result.current.transactions).toHaveLength(2)

      act(() => {
        result.current.fetchNextPage()
      })

      await waitFor(() => expect(result.current.transactions).toHaveLength(4))

      expect(result.current.transactions.map((t) => t.id)).toEqual([
        'txn-1',
        'txn-2',
        'txn-3',
        'txn-4',
      ])
      // Second call should use the cursor from page 1
      const calls = mockGetTransactions.mock.calls
      expect(calls.length).toBeGreaterThanOrEqual(2)
      const secondCallParams = calls[calls.length - 1][0]
      expect(secondCallParams.cursor).toBe('cursor1')
    })
  })

  describe('Last page', () => {
    it('sets hasNextPage: false when next_cursor is absent', async () => {
      mockGetTransactions.mockResolvedValue(lastPageResponse)

      const { result } = renderHook(() => useInfiniteTransactions(defaultOpts), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(result.current.hasNextPage).toBe(false)
      expect(result.current.transactions).toHaveLength(1)
      expect(result.current.transactions[0].id).toBe('txn-5')
    })
  })

  describe('Filter change resets', () => {
    it('discards prior pages when filters change', async () => {
      mockGetTransactions.mockResolvedValue(page1Response)

      const { result, rerender } = renderHook(
        (props: { filters: TransactionFiltersState }) =>
          useInfiniteTransactions({ ...defaultOpts, filters: props.filters }),
        {
          wrapper: createWrapper(),
          initialProps: { filters: {} },
        },
      )

      await waitFor(() => expect(result.current.isSuccess).toBe(true))
      expect(result.current.transactions).toHaveLength(2)

      // Change filters — this must change the queryKey and reset pages
      mockGetTransactions.mockResolvedValue(lastPageResponse)
      rerender({ filters: { search: 'starbucks' } })

      await waitFor(() => expect(result.current.transactions).toHaveLength(1))
      // After filter change, only the new page's transactions should be present
      expect(result.current.transactions[0].id).toBe('txn-5')
    })
  })

  describe('fetchNextPage on error', () => {
    it('sets isError: true and preserves prior pages when fetch fails', async () => {
      mockGetTransactions
        .mockResolvedValueOnce(page1Response)
        .mockRejectedValueOnce(new Error('Network error'))

      const { result } = renderHook(() => useInfiniteTransactions(defaultOpts), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))
      expect(result.current.transactions).toHaveLength(2)

      act(() => {
        result.current.fetchNextPage()
      })

      await waitFor(() => expect(result.current.isError).toBe(true))

      // Prior pages should still be accessible
      expect(result.current.transactions).toHaveLength(2)
      expect(result.current.transactions[0].id).toBe('txn-1')
    })
  })
})
