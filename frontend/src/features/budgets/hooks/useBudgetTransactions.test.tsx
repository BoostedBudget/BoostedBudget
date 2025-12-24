/**
 * useBudgetTransactions Hook Tests
 * Issue #499: Transaction-Budget Linking UI
 * Issue #564: Migrated to mock generated React Query hooks
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useBudgetTransactions } from './useBudgetTransactions';
import type { Transaction } from '../../transactions/types';

// Mock getCategoryTransactions function for loadMore calls
const mockGetCategoryTransactions = vi.fn();

// Mock the generated hooks
vi.mock('@/api/generated/endpoints/budgets/budgets', () => ({
  useGetCategoryTransactions: vi.fn(),
  getGetCategoryTransactionsQueryKey: vi.fn(
    (budgetId, categoryId, params) => ['budgets', budgetId, 'categories', categoryId, 'transactions', params]
  ),
  getCategoryTransactions: (...args: unknown[]) => mockGetCategoryTransactions(...args),
}));

// Import the mocked hook
import { useGetCategoryTransactions } from '@/api/generated/endpoints/budgets/budgets';

// Helper to create wrapper with QueryClient
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

// Test data
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
};

describe('useBudgetTransactions', () => {
  const mockRefetch = vi.fn().mockResolvedValue({ data: null });

  beforeEach(() => {
    vi.clearAllMocks();
    mockGetCategoryTransactions.mockReset();
    mockRefetch.mockClear();
  });

  afterEach(() => {
    vi.resetAllMocks();
  });

  // Helper to configure initial query state
  function configureInitialQuery(config: {
    data?: {
      transactions: Transaction[];
      total_count: number;
      total_amount: number;
      has_more: boolean;
    } | null;
    isLoading?: boolean;
    error?: Error | null;
  }) {
    vi.mocked(useGetCategoryTransactions).mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetch,
    } as unknown as ReturnType<typeof useGetCategoryTransactions>);
  }

  describe('initial state', () => {
    it('should have empty transactions initially', async () => {
      configureInitialQuery({
        data: {
          transactions: [],
          total_count: 0,
          total_amount: 0,
          has_more: false,
        },
      });

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.transactions).toEqual([]);
        expect(result.current.totalCount).toBe(0);
        expect(result.current.totalAmount).toBe(0);
        expect(result.current.hasMore).toBe(false);
      });
    });

    it('should start loading when hook mounts', () => {
      configureInitialQuery({ isLoading: true });

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(true);
      expect(result.current.error).toBeNull();
    });
  });

  describe('data fetching', () => {
    it('should fetch transactions on mount', async () => {
      configureInitialQuery({
        data: {
          transactions: [mockTransaction],
          total_count: 1,
          total_amount: 50.0,
          has_more: false,
        },
      });

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(useGetCategoryTransactions).toHaveBeenCalledWith(
        'budget-1',
        'category-1',
        { limit: 20, offset: 0 },
        expect.any(Object)
      );
      expect(result.current.transactions).toEqual([mockTransaction]);
      expect(result.current.totalCount).toBe(1);
      expect(result.current.totalAmount).toBe(50.0);
      expect(result.current.hasMore).toBe(false);
    });

    it('should handle has_more flag correctly', async () => {
      configureInitialQuery({
        data: {
          transactions: [mockTransaction],
          total_count: 50,
          total_amount: 500.0,
          has_more: true,
        },
      });

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.hasMore).toBe(true);
    });

    it('should respect custom limit parameter', async () => {
      configureInitialQuery({
        data: {
          transactions: [],
          total_count: 0,
          total_amount: 0,
          has_more: false,
        },
      });

      renderHook(() => useBudgetTransactions('budget-1', 'category-1', { pageSize: 50 }), {
        wrapper: createWrapper(),
      });

      expect(useGetCategoryTransactions).toHaveBeenCalledWith(
        'budget-1',
        'category-1',
        { limit: 50, offset: 0 },
        expect.any(Object)
      );
    });
  });

  describe('load more', () => {
    it('should load more transactions when loadMore is called', async () => {
      configureInitialQuery({
        data: {
          transactions: [mockTransaction],
          total_count: 2,
          total_amount: 100.0,
          has_more: true,
        },
      });

      mockGetCategoryTransactions.mockResolvedValue({
        transactions: [{ ...mockTransaction, id: 'tx-2' }],
        total_count: 2,
        total_amount: 100.0,
        has_more: false,
      });

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.transactions).toHaveLength(1);

      // Load more
      await act(async () => {
        await result.current.loadMore();
      });

      expect(mockGetCategoryTransactions).toHaveBeenCalledTimes(1);
      expect(mockGetCategoryTransactions).toHaveBeenCalledWith('budget-1', 'category-1', {
        limit: 20,
        offset: 20,
      });
      expect(result.current.transactions).toHaveLength(2);
    });

    it('should set loadingMore while fetching more', async () => {
      configureInitialQuery({
        data: {
          transactions: [mockTransaction],
          total_count: 2,
          total_amount: 100.0,
          has_more: true,
        },
      });

      // Never resolving promise for loadMore
      mockGetCategoryTransactions.mockImplementation(() => new Promise(() => {}));

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      act(() => {
        result.current.loadMore();
      });

      expect(result.current.loadingMore).toBe(true);
      expect(result.current.loading).toBe(false);
    });

    it('should not loadMore if already loading', async () => {
      configureInitialQuery({ isLoading: true });

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      // Try to load more while initial loading
      await act(async () => {
        await result.current.loadMore();
      });

      // getCategoryTransactions should not have been called
      expect(mockGetCategoryTransactions).not.toHaveBeenCalled();
    });
  });

  describe('refetch', () => {
    it('should refetch transactions from start', async () => {
      configureInitialQuery({
        data: {
          transactions: [mockTransaction],
          total_count: 1,
          total_amount: 50.0,
          has_more: false,
        },
      });

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.refetch();
      });

      expect(mockRefetch).toHaveBeenCalled();
    });
  });

  describe('error handling', () => {
    it('should set error when fetch fails', async () => {
      configureInitialQuery({
        error: new Error('API Error'),
      });

      const { result } = renderHook(() => useBudgetTransactions('budget-1', 'category-1'), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(false);
      expect(result.current.error).toBeInstanceOf(Error);
      expect(result.current.error?.message).toBe('API Error');
    });
  });

  describe('parameter changes', () => {
    it('should refetch when budgetId changes', async () => {
      configureInitialQuery({
        data: {
          transactions: [],
          total_count: 0,
          total_amount: 0,
          has_more: false,
        },
      });

      const { rerender } = renderHook(
        ({ budgetId, categoryId }) => useBudgetTransactions(budgetId, categoryId),
        {
          initialProps: { budgetId: 'budget-1', categoryId: 'category-1' },
          wrapper: createWrapper(),
        }
      );

      rerender({ budgetId: 'budget-2', categoryId: 'category-1' });

      expect(useGetCategoryTransactions).toHaveBeenCalledWith(
        'budget-2',
        'category-1',
        expect.any(Object),
        expect.any(Object)
      );
    });

    it('should refetch when categoryId changes', async () => {
      configureInitialQuery({
        data: {
          transactions: [],
          total_count: 0,
          total_amount: 0,
          has_more: false,
        },
      });

      const { rerender } = renderHook(
        ({ budgetId, categoryId }) => useBudgetTransactions(budgetId, categoryId),
        {
          initialProps: { budgetId: 'budget-1', categoryId: 'category-1' },
          wrapper: createWrapper(),
        }
      );

      rerender({ budgetId: 'budget-1', categoryId: 'category-2' });

      expect(useGetCategoryTransactions).toHaveBeenCalledWith(
        'budget-1',
        'category-2',
        expect.any(Object),
        expect.any(Object)
      );
    });
  });
});
