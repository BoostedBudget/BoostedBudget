import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Transaction } from '../../transactions/types'
import {
  mockUseGetCategoryTransactions,
  mockRefetchCategoryTransactions,
  mockGetCategoryTransactions,
  mockGetCategoryTransactionsQueryKey,
} from '@/test/mocks/budgets-api'
import { useBudgetTransactions } from './useBudgetTransactions'

function createWrapper(queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

const mockTransaction: Transaction = {
  id: 'tx-1',
  user_id: 'user-1',
  account_id: 'account-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: -50.0,
  date: '2024-01-15T00:00:00Z',
  name: 'Coffee Shop',
  merchant_name: 'Starbucks',
  category: '["Food and Drink", "Coffee Shop"]',
  pending: false,
  created_at: '2024-01-15T00:00:00Z',
  updated_at: '2024-01-15T00:00:00Z',
}

describe('useBudgetTransactions', () => {
  beforeEach(() => {
    mockUseGetCategoryTransactions.mockClear()
    mockGetCategoryTransactions.mockClear()
    mockRefetchCategoryTransactions.mockClear()
    mockGetCategoryTransactionsQueryKey.mockClear()
  })

  function mockQueryState(data?: unknown, isLoading = false, error: Error | null = null) {
    mockUseGetCategoryTransactions.mockReturnValue({
      data,
      isLoading,
      error,
      refetch: mockRefetchCategoryTransactions,
    })
  }

  describe('initial state', () => {
    it('should have empty transactions initially', async () => {
      mockQueryState({
        transactions: [],
        total_count: 0,
        total_amount: 0,
        has_more: false,
      })

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.transactions).toEqual([])
        expect(result.current.totalCount).toBe(0)
        expect(result.current.totalAmount).toBe(0)
        expect(result.current.hasMore).toBe(false)
      })
    })

    it('should start loading when hook mounts', () => {
      mockQueryState(undefined, true)

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.error).toBeNull()
    })
  })

  describe('data fetching', () => {
    it('should fetch transactions on mount', async () => {
      mockQueryState({
        transactions: [mockTransaction],
        total_count: 1,
        total_amount: 50.0,
        has_more: false,
      })

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(mockUseGetCategoryTransactions).toHaveBeenCalledWith(
        'budget-1',
        'category-1',
        { limit: 20, offset: 0 },
        expect.any(Object)
      )
      expect(result.current.transactions).toEqual([mockTransaction])
      expect(result.current.totalCount).toBe(1)
      expect(result.current.totalAmount).toBe(50.0)
      expect(result.current.hasMore).toBe(false)
    })

    it('should handle has_more flag correctly', async () => {
      mockQueryState({
        transactions: [mockTransaction],
        total_count: 50,
        total_amount: 500.0,
        has_more: true,
      })

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.hasMore).toBe(true)
    })

    it('should respect custom limit parameter', async () => {
      mockQueryState({
        transactions: [],
        total_count: 0,
        total_amount: 0,
        has_more: false,
      })

      renderHook(() => useBudgetTransactions('budget-1', 'category-1', { pageSize: 50 }), {
        wrapper: createWrapper(),
      })

      expect(mockUseGetCategoryTransactions).toHaveBeenCalledWith(
        'budget-1',
        'category-1',
        { limit: 50, offset: 0 },
        expect.any(Object)
      )
    })
  })

  describe('load more', () => {
    it('discards a pending page immediately on central cache invalidation even with unchanged first-page data', async () => {
      const initial = { transactions: [mockTransaction], total_count: 2, total_amount: 100, has_more: true }
      mockQueryState(initial)
      const client = new QueryClient()
      const key = mockGetCategoryTransactionsQueryKey('budget-1', 'category-1', { limit: 20, offset: 0 })
      client.setQueryData(key, initial)
      let resolvePage!: (value: typeof initial) => void
      mockGetCategoryTransactions.mockImplementationOnce(() => new Promise((resolve) => { resolvePage = resolve }))
      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), { wrapper: createWrapper(client) })
      let pending!: Promise<void>
      act(() => { pending = result.current.loadMore() })
      await act(async () => { await client.invalidateQueries({ queryKey: key }); resolvePage(initial); await pending })
      expect(result.current.transactions).toEqual([mockTransaction])
    })
    it('discards an in-flight page when the first page is refreshed', async () => {
      const initial = { transactions: [mockTransaction], total_count: 2, total_amount: 100, has_more: true }
      mockQueryState(initial)
      let resolvePage!: (value: typeof initial) => void
      mockGetCategoryTransactions.mockImplementationOnce(() => new Promise((resolve) => { resolvePage = resolve }))
      const { result, rerender } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), { wrapper: createWrapper() })
      let pending!: Promise<void>
      act(() => { pending = result.current.loadMore() })
      mockQueryState({ ...initial, transactions: [{ ...mockTransaction, id: 'fresh' }], has_more: false })
      rerender()
      await act(async () => { resolvePage(initial); await pending })
      expect(result.current.transactions.map((transaction) => transaction.id)).toEqual(['fresh'])
      expect(result.current.hasMore).toBe(false)
    })

    it('should load more transactions when loadMore is called', async () => {
      mockQueryState({
        transactions: [mockTransaction],
        total_count: 2,
        total_amount: 100.0,
        has_more: true,
      })

      mockGetCategoryTransactions.mockResolvedValue({
        transactions: [{ ...mockTransaction, id: 'tx-2' }],
        total_count: 2,
        total_amount: 100.0,
        has_more: false,
      })

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.transactions).toHaveLength(1)

      await act(async () => {
        await result.current.loadMore()
      })

      expect(mockGetCategoryTransactions).toHaveBeenCalledTimes(1)
      expect(mockGetCategoryTransactions).toHaveBeenCalledWith('budget-1', 'category-1', {
        limit: 20,
        offset: 20,
      })
      expect(result.current.transactions).toHaveLength(2)
    })

    it('should set loadingMore while fetching more', async () => {
      mockQueryState({
        transactions: [mockTransaction],
        total_count: 2,
        total_amount: 100.0,
        has_more: true,
      })

      mockGetCategoryTransactions.mockImplementation(() => new Promise(() => {}))

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      act(() => {
        result.current.loadMore()
      })

      expect(result.current.loadingMore).toBe(true)
      expect(result.current.loading).toBe(false)
    })

    it('should not loadMore if already loading', async () => {
      mockQueryState(undefined, true)

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.loadMore()
      })

      expect(mockGetCategoryTransactions).not.toHaveBeenCalled()
    })
  })

  describe('refetch', () => {
    it('should refetch transactions from start', async () => {
      mockQueryState({
        transactions: [mockTransaction],
        total_count: 1,
        total_amount: 50.0,
        has_more: false,
      })

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.refetch()
      })

      expect(mockRefetchCategoryTransactions).toHaveBeenCalled()
    })
  })

  describe('error handling', () => {
    it('should set error when fetch fails', async () => {
      mockQueryState(undefined, false, new Error('API Error'))

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.error).toBeInstanceOf(Error)
      expect(result.current.error?.message).toBe('API Error')
    })
  })

  describe('parameter changes', () => {
    it('should refetch when budgetId changes', async () => {
      mockQueryState({
        transactions: [],
        total_count: 0,
        total_amount: 0,
        has_more: false,
      })

      const { rerender } = renderHook(
        ({ budgetId, categoryId }) => useBudgetTransactions(budgetId, categoryId),
        {
          initialProps: { budgetId: 'budget-1', categoryId: 'category-1' },
          wrapper: createWrapper(),
        }
      )

      rerender({ budgetId: 'budget-2', categoryId: 'category-1' })

      expect(mockUseGetCategoryTransactions).toHaveBeenCalledWith(
        'budget-2',
        'category-1',
        expect.any(Object),
        expect.any(Object)
      )
    })

    it('should refetch when categoryId changes', async () => {
      mockQueryState({
        transactions: [],
        total_count: 0,
        total_amount: 0,
        has_more: false,
      })

      const { rerender } = renderHook(
        ({ budgetId, categoryId }) => useBudgetTransactions(budgetId, categoryId),
        {
          initialProps: { budgetId: 'budget-1', categoryId: 'category-1' },
          wrapper: createWrapper(),
        }
      )

      rerender({ budgetId: 'budget-1', categoryId: 'category-2' })

      expect(mockUseGetCategoryTransactions).toHaveBeenCalledWith(
        'budget-1',
        'category-2',
        expect.any(Object),
        expect.any(Object)
      )
    })
  })
})
