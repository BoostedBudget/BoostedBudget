/**
 * useBudgetTransactions Hook Tests
 * Issue #499: Transaction-Budget Linking UI
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useBudgetTransactions } from './useBudgetTransactions';
import * as budgetsApi from '../api/budgetsApi';
import type { Transaction } from '../../transactions/types';

// Mock the API
vi.mock('../api/budgetsApi', () => ({
  fetchCategoryTransactions: vi.fn(),
}));

const mockFetchCategoryTransactions = vi.mocked(budgetsApi.fetchCategoryTransactions);

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
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.resetAllMocks();
  });

  describe('initial state', () => {
    it('should have empty transactions initially', () => {
      mockFetchCategoryTransactions.mockResolvedValue({
        transactions: [],
        total_count: 0,
        total_amount: 0,
        has_more: false,
      });

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      expect(result.current.transactions).toEqual([]);
      expect(result.current.totalCount).toBe(0);
      expect(result.current.totalAmount).toBe(0);
      expect(result.current.hasMore).toBe(false);
    });

    it('should start loading when hook mounts', () => {
      mockFetchCategoryTransactions.mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      expect(result.current.loading).toBe(true);
      expect(result.current.error).toBeNull();
    });
  });

  describe('data fetching', () => {
    it('should fetch transactions on mount', async () => {
      const mockResponse = {
        transactions: [mockTransaction],
        total_count: 1,
        total_amount: 50.0,
        has_more: false,
      };
      mockFetchCategoryTransactions.mockResolvedValue(mockResponse);

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(mockFetchCategoryTransactions).toHaveBeenCalledWith(
        'budget-1',
        'category-1',
        { limit: 20, offset: 0 }
      );
      expect(result.current.transactions).toEqual([mockTransaction]);
      expect(result.current.totalCount).toBe(1);
      expect(result.current.totalAmount).toBe(50.0);
      expect(result.current.hasMore).toBe(false);
    });

    it('should handle has_more flag correctly', async () => {
      mockFetchCategoryTransactions.mockResolvedValue({
        transactions: [mockTransaction],
        total_count: 50,
        total_amount: 500.0,
        has_more: true,
      });

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.hasMore).toBe(true);
    });

    it('should respect custom limit parameter', async () => {
      mockFetchCategoryTransactions.mockResolvedValue({
        transactions: [],
        total_count: 0,
        total_amount: 0,
        has_more: false,
      });

      renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1', { pageSize: 50 })
      );

      await waitFor(() => {
        expect(mockFetchCategoryTransactions).toHaveBeenCalledWith(
          'budget-1',
          'category-1',
          { limit: 50, offset: 0 }
        );
      });
    });
  });

  describe('load more', () => {
    it('should load more transactions when loadMore is called', async () => {
      // First call returns first page
      mockFetchCategoryTransactions
        .mockResolvedValueOnce({
          transactions: [mockTransaction],
          total_count: 2,
          total_amount: 100.0,
          has_more: true,
        })
        .mockResolvedValueOnce({
          transactions: [{ ...mockTransaction, id: 'tx-2' }],
          total_count: 2,
          total_amount: 100.0,
          has_more: false,
        });

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.transactions).toHaveLength(1);

      // Load more
      await act(async () => {
        await result.current.loadMore();
      });

      expect(mockFetchCategoryTransactions).toHaveBeenCalledTimes(2);
      expect(mockFetchCategoryTransactions).toHaveBeenLastCalledWith(
        'budget-1',
        'category-1',
        { limit: 20, offset: 20 }
      );
      expect(result.current.transactions).toHaveLength(2);
    });

    it('should set loadingMore while fetching more', async () => {
      mockFetchCategoryTransactions
        .mockResolvedValueOnce({
          transactions: [mockTransaction],
          total_count: 2,
          total_amount: 100.0,
          has_more: true,
        })
        .mockImplementationOnce(
          () => new Promise(() => {}) // Never resolves
        );

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

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
      mockFetchCategoryTransactions.mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      // Try to load more while initial loading
      await act(async () => {
        await result.current.loadMore();
      });

      // Should only be called once (initial load)
      expect(mockFetchCategoryTransactions).toHaveBeenCalledTimes(1);
    });
  });

  describe('refetch', () => {
    it('should refetch transactions from start', async () => {
      mockFetchCategoryTransactions
        .mockResolvedValueOnce({
          transactions: [mockTransaction],
          total_count: 1,
          total_amount: 50.0,
          has_more: false,
        })
        .mockResolvedValueOnce({
          transactions: [{ ...mockTransaction, id: 'tx-updated' }],
          total_count: 1,
          total_amount: 75.0,
          has_more: false,
        });

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.refetch();
      });

      expect(mockFetchCategoryTransactions).toHaveBeenCalledTimes(2);
      expect(result.current.transactions[0].id).toBe('tx-updated');
      expect(result.current.totalAmount).toBe(75.0);
    });
  });

  describe('error handling', () => {
    it('should set error when fetch fails', async () => {
      mockFetchCategoryTransactions.mockRejectedValue(new Error('API Error'));

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.error).toBeInstanceOf(Error);
      expect(result.current.error?.message).toBe('API Error');
      expect(result.current.transactions).toEqual([]);
    });

    it('should clear error on successful refetch', async () => {
      mockFetchCategoryTransactions
        .mockRejectedValueOnce(new Error('API Error'))
        .mockResolvedValueOnce({
          transactions: [mockTransaction],
          total_count: 1,
          total_amount: 50.0,
          has_more: false,
        });

      const { result } = renderHook(() =>
        useBudgetTransactions('budget-1', 'category-1')
      );

      await waitFor(() => {
        expect(result.current.error).not.toBeNull();
      });

      await act(async () => {
        await result.current.refetch();
      });

      expect(result.current.error).toBeNull();
      expect(result.current.transactions).toHaveLength(1);
    });
  });

  describe('parameter changes', () => {
    it('should refetch when budgetId changes', async () => {
      mockFetchCategoryTransactions.mockResolvedValue({
        transactions: [],
        total_count: 0,
        total_amount: 0,
        has_more: false,
      });

      const { result, rerender } = renderHook(
        ({ budgetId, categoryId }) => useBudgetTransactions(budgetId, categoryId),
        { initialProps: { budgetId: 'budget-1', categoryId: 'category-1' } }
      );

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      rerender({ budgetId: 'budget-2', categoryId: 'category-1' });

      await waitFor(() => {
        expect(mockFetchCategoryTransactions).toHaveBeenCalledTimes(2);
      });

      expect(mockFetchCategoryTransactions).toHaveBeenLastCalledWith(
        'budget-2',
        'category-1',
        { limit: 20, offset: 0 }
      );
    });

    it('should refetch when categoryId changes', async () => {
      mockFetchCategoryTransactions.mockResolvedValue({
        transactions: [],
        total_count: 0,
        total_amount: 0,
        has_more: false,
      });

      const { result, rerender } = renderHook(
        ({ budgetId, categoryId }) => useBudgetTransactions(budgetId, categoryId),
        { initialProps: { budgetId: 'budget-1', categoryId: 'category-1' } }
      );

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      rerender({ budgetId: 'budget-1', categoryId: 'category-2' });

      await waitFor(() => {
        expect(mockFetchCategoryTransactions).toHaveBeenCalledTimes(2);
      });

      expect(mockFetchCategoryTransactions).toHaveBeenLastCalledWith(
        'budget-1',
        'category-2',
        { limit: 20, offset: 0 }
      );
    });
  });
});
