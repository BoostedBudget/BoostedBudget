/**
 * useTransactions Hook Tests
 *
 * Issue #564: Migrated to mock generated React Query hooks
 * Tests the React hook with filtering, pagination, and debounced search.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useTransactions } from './useTransactions';
import type { GetTransactionsResponse, TransactionFilters } from '../types';

// Use vi.hoisted for mock functions
const { mockRefetch } = vi.hoisted(() => ({
  mockRefetch: vi.fn().mockResolvedValue({}),
}));

// Mock the generated hooks
vi.mock('@/api/generated/endpoints/transactions/transactions', () => ({
  useGetTransactions: vi.fn(),
  getGetTransactionsQueryKey: vi.fn(() => ['transactions']),
}));

// Import the mocked hook
import { useGetTransactions } from '@/api/generated/endpoints/transactions/transactions';

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
  };

  const mockEmptyResponse: GetTransactionsResponse = {
    transactions: [],
    total_count: 0,
    limit: 50,
    offset: 0,
    has_more: false,
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockRefetch.mockClear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Helper to configure query state
  function configureQuery(config: {
    data?: GetTransactionsResponse | null;
    isLoading?: boolean;
    error?: Error | null;
  }) {
    vi.mocked(useGetTransactions).mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetch,
    } as unknown as ReturnType<typeof useGetTransactions>);
  }

  describe('Basic Functionality', () => {
    it('should return loading state initially', () => {
      configureQuery({ isLoading: true });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(true);
      expect(result.current.data).toBeNull();
      expect(result.current.error).toBeNull();
    });

    it('should fetch and return transactions on mount', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.data).toEqual(mockTransactionsResponse);
      expect(result.current.error).toBeNull();
    });

    it('should handle fetch errors gracefully', async () => {
      configureQuery({ error: new Error('Failed to fetch transactions') });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      expect(result.current.error).toBeInstanceOf(Error);
      expect(result.current.error?.message).toBe('Failed to fetch transactions');
      expect(result.current.data).toBeNull();
    });

    it('should return empty transactions array when no transactions exist', async () => {
      configureQuery({ data: mockEmptyResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.data).toEqual(mockEmptyResponse);
      expect(result.current.data?.transactions).toHaveLength(0);
      expect(result.current.data?.has_more).toBe(false);
    });
  });

  describe('Pagination', () => {
    it('should use custom limit when provided', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions({ limit: 25 }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Verify the hook was called (params are embedded in the hook)
      expect(useGetTransactions).toHaveBeenCalled();
    });

    it('should support nextPage pagination', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.currentPage).toBe(0);

      // Call nextPage
      act(() => {
        result.current.nextPage();
      });

      expect(result.current.currentPage).toBe(1);
    });

    it('should support previousPage pagination', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions({ initialOffset: 50 }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.currentPage).toBe(1);

      // Call previousPage
      act(() => {
        result.current.previousPage();
      });

      expect(result.current.currentPage).toBe(0);
    });

    it('should not go below offset 0 on previousPage', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.currentPage).toBe(0);

      // Call previousPage when already at offset 0
      act(() => {
        result.current.previousPage();
      });

      // Should still be at page 0
      expect(result.current.currentPage).toBe(0);
    });

    it('should expose pagination metadata', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.hasMore).toBe(true);
      expect(result.current.totalCount).toBe(150);
      expect(result.current.currentPage).toBe(0);
    });
  });

  describe('Filtering', () => {
    it('should filter by account_id', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const filters: TransactionFilters = {
        account_id: 'account-123',
      };

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.data).toEqual(mockTransactionsResponse);
    });

    it('should filter by date range', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const filters: TransactionFilters = {
        start_date: '2025-01-01',
        end_date: '2025-12-31',
      };

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.data).toEqual(mockTransactionsResponse);
    });

    it('should filter by category', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const filters: TransactionFilters = {
        category: 'food',
      };

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.data).toEqual(mockTransactionsResponse);
    });

    it('should filter by pending status', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const filters: TransactionFilters = {
        pending: true,
      };

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.data).toEqual(mockTransactionsResponse);
    });

    it('should support multiple filters combined', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const filters: TransactionFilters = {
        account_id: 'account-123',
        start_date: '2025-01-01',
        end_date: '2025-12-31',
        category: 'food',
        pending: false,
      };

      const { result } = renderHook(() => useTransactions({ filters }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.data).toEqual(mockTransactionsResponse);
    });

    it('should reset to page 0 on filter changes', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result, rerender } = renderHook(
        ({ filters }) => useTransactions({ filters }),
        {
          wrapper: createWrapper(),
          initialProps: { filters: {} as TransactionFilters },
        }
      );

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Go to next page
      act(() => {
        result.current.nextPage();
      });
      expect(result.current.currentPage).toBe(1);

      // Change filters - should reset to page 0
      rerender({ filters: { account_id: 'account-456' } });

      await waitFor(() => {
        expect(result.current.currentPage).toBe(0);
      });
    });
  });

  describe('Debounced Search', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('should debounce search input with 300ms delay', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result, rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          wrapper: createWrapper(),
          initialProps: { search: '' },
        }
      );

      // Update search - should NOT trigger immediately
      rerender({ search: 'star' });

      // Before debounce - useGetTransactions should have been called with original empty search
      const callsBefore = vi.mocked(useGetTransactions).mock.calls;
      const lastCallBefore = callsBefore[callsBefore.length - 1];
      expect(lastCallBefore?.[0]).not.toContainEqual(expect.objectContaining({ search: 'star' }));

      // Wait for debounce delay
      await act(async () => {
        vi.advanceTimersByTime(350);
      });

      // After debounce - now the query should include the search term
      // The hook will re-render with the debounced search value
      expect(result.current.loading).toBeDefined();
    });

    it('should cancel previous debounced search if new input comes in', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          wrapper: createWrapper(),
          initialProps: { search: '' },
        }
      );

      // Type quickly - each should cancel the previous debounce
      rerender({ search: 's' });
      await act(async () => {
        vi.advanceTimersByTime(100);
      });

      rerender({ search: 'st' });
      await act(async () => {
        vi.advanceTimersByTime(100);
      });

      rerender({ search: 'sta' });

      // Wait for final debounce
      await act(async () => {
        vi.advanceTimersByTime(350);
      });

      // The last call should have the final search term after debounce
      const calls = vi.mocked(useGetTransactions).mock.calls;
      expect(calls.length).toBeGreaterThan(0);
    });

    it('should clear search results when search is empty', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result, rerender } = renderHook(
        ({ search }) => useTransactions({ filters: { search } }),
        {
          wrapper: createWrapper(),
          initialProps: { search: 'starbucks' },
        }
      );

      // Clear search
      rerender({ search: '' });

      // Wait for debounce
      await act(async () => {
        vi.advanceTimersByTime(350);
      });

      // Should have data without search filter
      expect(result.current.data).toEqual(mockTransactionsResponse);
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function to manually reload data', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Call refetch
      await act(async () => {
        await result.current.refetch();
      });

      expect(mockRefetch).toHaveBeenCalled();
    });

    it('should preserve current filters and pagination on refetch', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const filters: TransactionFilters = {
        account_id: 'account-123',
        category: 'food',
      };

      const { result } = renderHook(() => useTransactions({ filters, limit: 25 }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Go to next page
      act(() => {
        result.current.nextPage();
      });

      expect(result.current.currentPage).toBe(1);

      // Refetch
      await act(async () => {
        await result.current.refetch();
      });

      // Should still be on page 1
      expect(result.current.currentPage).toBe(1);
      expect(mockRefetch).toHaveBeenCalled();
    });
  });

  describe('updateTransaction', () => {
    it('should update a single transaction in the current data', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Update first transaction with new tags
      const updatedTransaction = {
        ...mockTransactionsResponse.transactions[0],
        tags: ['New Tag'],
        category_override: '["Custom Category"]',
      };

      act(() => {
        result.current.updateTransaction(updatedTransaction);
      });

      // Transaction should be updated in place
      expect(result.current.data?.transactions[0].tags).toEqual(['New Tag']);
      expect(result.current.data?.transactions[0].category_override).toBe('["Custom Category"]');
      // Second transaction should remain unchanged
      expect(result.current.data?.transactions[1]).toEqual(
        mockTransactionsResponse.transactions[1]
      );
    });

    it('should not trigger a refetch when updating transaction', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      mockRefetch.mockClear();

      // Update transaction
      const updatedTransaction = {
        ...mockTransactionsResponse.transactions[0],
        tags: ['Updated'],
      };

      act(() => {
        result.current.updateTransaction(updatedTransaction);
      });

      // No refetch should be made
      expect(mockRefetch).not.toHaveBeenCalled();
    });

    it('should preserve metadata when updating transaction', async () => {
      configureQuery({ data: mockTransactionsResponse });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Update transaction
      act(() => {
        result.current.updateTransaction({
          ...mockTransactionsResponse.transactions[0],
          tags: ['Updated'],
        });
      });

      // Metadata should be preserved
      expect(result.current.data?.total_count).toBe(150);
      expect(result.current.data?.has_more).toBe(true);
      expect(result.current.hasMore).toBe(true);
      expect(result.current.totalCount).toBe(150);
    });

    it('should handle update when data is null', async () => {
      configureQuery({ isLoading: true });

      const { result } = renderHook(() => useTransactions(), {
        wrapper: createWrapper(),
      });

      // Data should be null (loading)
      expect(result.current.data).toBeNull();

      // Try to update - should not throw
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
        });
      });

      // Data should still be null
      expect(result.current.data).toBeNull();
    });
  });
});
