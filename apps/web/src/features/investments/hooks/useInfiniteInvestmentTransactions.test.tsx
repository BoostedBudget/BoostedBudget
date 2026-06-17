/**
 * useInfiniteInvestmentTransactions Hook Tests
 *
 * Issue #1296: Cursor-based infinite scroll for investment transactions.
 * Tests the infinite query hook with cursor pagination, filter changes, and error handling.
 * NOTE: Runs with API-level mocks (getInvestmentTransactions raw fetcher)
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useInfiniteInvestmentTransactions } from './useInfiniteInvestmentTransactions'
import { mockGetInvestmentTransactions } from '@/test/mocks/investments-api'
import type { GetInvestmentTransactionsOutputBody } from '@boostedbudget/api-client/models/getInvestmentTransactionsOutputBody'
import type { InvestmentTransaction } from '@boostedbudget/api-client/models/investmentTransaction'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const makeTxn = (id: string): InvestmentTransaction => ({
  id,
  user_id: 'user-123',
  account_id: 'account-456',
  plaid_investment_transaction_id: `plaid-${id}`,
  security_id: 'sec-abc',
  date: '2025-10-25',
  name: `Investment txn ${id}`,
  amount: -100.0,
  quantity: 1.0,
  price: 100.0,
  type: 'buy',
  created_at: '2025-10-25T10:00:00Z',
})

const page1Response: GetInvestmentTransactionsOutputBody = {
  transactions: [makeTxn('itxn-1'), makeTxn('itxn-2')],
  limit: 50,
  next_cursor: 'cursor1',
}

const page2Response: GetInvestmentTransactionsOutputBody = {
  transactions: [makeTxn('itxn-3'), makeTxn('itxn-4')],
  limit: 50,
  next_cursor: 'cursor2',
}

const lastPageResponse: GetInvestmentTransactionsOutputBody = {
  transactions: [makeTxn('itxn-5')],
  limit: 50,
  next_cursor: undefined,
}

describe('useInfiniteInvestmentTransactions', () => {
  beforeEach(() => {
    // mockReset clears both call history AND stored mockResolvedValue implementations,
    // ensuring no state bleeds between tests. Each test sets its own mock return value.
    mockGetInvestmentTransactions.mockReset()
  })

  describe('First page (no cursor)', () => {
    it('returns transactions from page 1 and hasNextPage: true when next_cursor is present', async () => {
      mockGetInvestmentTransactions.mockResolvedValue(page1Response)

      const { result } = renderHook(() => useInfiniteInvestmentTransactions({}), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(result.current.transactions).toHaveLength(2)
      expect(result.current.transactions[0].id).toBe('itxn-1')
      expect(result.current.transactions[1].id).toBe('itxn-2')
      expect(result.current.hasNextPage).toBe(true)

      const firstCallParams = mockGetInvestmentTransactions.mock.calls[0][0] as Record<string, unknown>
      expect(firstCallParams.cursor).toBeUndefined()
    })
  })

  describe('Second page (fetchNextPage)', () => {
    it('calls getInvestmentTransactions with cursor from first page and combines rows', async () => {
      mockGetInvestmentTransactions
        .mockResolvedValueOnce(page1Response)
        .mockResolvedValueOnce(page2Response)

      const { result } = renderHook(() => useInfiniteInvestmentTransactions({}), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))
      expect(result.current.transactions).toHaveLength(2)

      act(() => {
        result.current.fetchNextPage()
      })

      await waitFor(() => expect(result.current.transactions).toHaveLength(4))

      expect(result.current.transactions.map((t: InvestmentTransaction) => t.id)).toEqual([
        'itxn-1',
        'itxn-2',
        'itxn-3',
        'itxn-4',
      ])

      const calls = mockGetInvestmentTransactions.mock.calls
      expect(calls.length).toBeGreaterThanOrEqual(2)
      const secondCallParams = calls[calls.length - 1][0]
      expect(secondCallParams.cursor).toBe('cursor1')
    })
  })

  describe('Last page', () => {
    it('sets hasNextPage: false when next_cursor is absent', async () => {
      mockGetInvestmentTransactions.mockResolvedValue(lastPageResponse)

      const { result } = renderHook(() => useInfiniteInvestmentTransactions({}), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(result.current.hasNextPage).toBe(false)
      expect(result.current.transactions).toHaveLength(1)
    })
  })

  describe('Null transactions array', () => {
    it('returns empty array when API returns null transactions', async () => {
      mockGetInvestmentTransactions.mockResolvedValue({
        transactions: null,
        limit: 50,
        next_cursor: undefined,
      })

      const { result } = renderHook(() => useInfiniteInvestmentTransactions({}), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))
      expect(result.current.transactions).toHaveLength(0)
      expect(result.current.hasNextPage).toBe(false)
    })
  })

  describe('Filter forwarding', () => {
    it('forwards account_id filter to the API call', async () => {
      mockGetInvestmentTransactions.mockResolvedValue(page1Response)

      const { result } = renderHook(
        () => useInfiniteInvestmentTransactions({ account_id: 'acct-xyz' }),
        { wrapper: createWrapper() },
      )

      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      const callParams = mockGetInvestmentTransactions.mock.calls[0][0] as Record<string, unknown>
      expect(callParams.account_id).toBe('acct-xyz')
    })

    it('forwards start_date and end_date filters', async () => {
      mockGetInvestmentTransactions.mockResolvedValue(lastPageResponse)

      const { result } = renderHook(
        () =>
          useInfiniteInvestmentTransactions({
            start_date: '2025-01-01',
            end_date: '2025-12-31',
          }),
        { wrapper: createWrapper() },
      )

      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      const callParams = mockGetInvestmentTransactions.mock.calls[0][0] as Record<string, unknown>
      expect(callParams.start_date).toBe('2025-01-01')
      expect(callParams.end_date).toBe('2025-12-31')
    })
  })

  describe('Filter change resets', () => {
    it('discards prior pages when filters change', async () => {
      mockGetInvestmentTransactions.mockResolvedValue(page1Response)

      const { result, rerender } = renderHook(
        (props: { account_id?: string }) => useInfiniteInvestmentTransactions(props),
        {
          wrapper: createWrapper(),
          initialProps: {},
        },
      )

      await waitFor(() => expect(result.current.isSuccess).toBe(true))
      expect(result.current.transactions).toHaveLength(2)

      mockGetInvestmentTransactions.mockResolvedValue(lastPageResponse)
      rerender({ account_id: 'different-account' })

      await waitFor(() => expect(result.current.transactions).toHaveLength(1))
      expect(result.current.transactions[0].id).toBe('itxn-5')
    })
  })

  describe('Error handling', () => {
    it('sets isError: true and preserves prior pages when fetch fails', async () => {
      // Use mockImplementationOnce (lazy) rather than mockRejectedValueOnce (eager).
      // bun:test v1.x detects pre-created rejected promises as unhandled rejections
      // immediately during test setup, before TanStack Query can catch them. A lazy
      // implementation only creates the rejected promise when the mock is actually
      // invoked by TQ, at which point TQ's internal error handling catches it first.
      mockGetInvestmentTransactions
        .mockResolvedValueOnce(page1Response)
        .mockImplementationOnce(() => Promise.reject(new Error('Network error')))

      const { result } = renderHook(() => useInfiniteInvestmentTransactions({}), {
        wrapper: createWrapper(),
      })

      await waitFor(() => expect(result.current.isSuccess).toBe(true))
      expect(result.current.transactions).toHaveLength(2)

      act(() => {
        result.current.fetchNextPage().catch(() => {})
      })

      await waitFor(() => expect(result.current.isError).toBe(true))

      expect(result.current.transactions).toHaveLength(2)
      expect(result.current.transactions[0].id).toBe('itxn-1')
    })
  })
})
