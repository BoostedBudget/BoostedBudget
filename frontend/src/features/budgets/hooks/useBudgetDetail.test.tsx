/**
 * useBudgetDetail Hook Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useBudgetDetail } from './useBudgetDetail';
import * as budgetsApi from '../api/budgetsApi';
import type { Budget } from '../types';

// Mock the API module
vi.mock('../api/budgetsApi');

describe('useBudgetDetail', () => {
  const mockBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2024-01-01T00:00:00Z',
    end_date: '2024-01-31T23:59:59Z',
    amount: 3000,
    currency_code: 'USD',
    is_active: true,
    income_amount: 5000,
    income_sources: [],
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
        spent_amount: 250,
        tags: [],
        match_logic: 'category_only',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation'],
        allocated_amount: 300,
        spent_amount: 150,
        tags: ['Essential'],
        match_logic: 'and',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      },
    ],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Initial State', () => {
    it('should return loading=true initially', () => {
      vi.mocked(budgetsApi.fetchBudget).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      expect(result.current.loading).toBe(true);
      expect(result.current.budget).toBeNull();
      expect(result.current.metrics).toBeNull();
      expect(result.current.error).toBeNull();
    });
  });

  describe('Fetching Budget', () => {
    it('should fetch budget by ID on mount', async () => {
      vi.mocked(budgetsApi.fetchBudget).mockResolvedValue(mockBudget);

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(budgetsApi.fetchBudget).toHaveBeenCalledWith('budget-1');
      expect(result.current.budget).toEqual(mockBudget);
    });

    it('should compute metrics from budget data', async () => {
      vi.mocked(budgetsApi.fetchBudget).mockResolvedValue(mockBudget);

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      await waitFor(() => {
        expect(result.current.metrics).not.toBeNull();
      });

      expect(result.current.metrics).toMatchObject({
        total_allocated: 800, // 500 + 300
        total_spent: 400, // 250 + 150
        remaining_amount: expect.any(Number),
        percent_used: expect.any(Number),
        unallocated_funds: expect.any(Number),
      });
    });

    it('should refetch when budgetId changes', async () => {
      const budget2: Budget = {
        ...mockBudget,
        id: 'budget-2',
        name: 'Second Budget',
      };

      vi.mocked(budgetsApi.fetchBudget)
        .mockResolvedValueOnce(mockBudget)
        .mockResolvedValueOnce(budget2);

      const { result, rerender } = renderHook(
        ({ budgetId }) => useBudgetDetail(budgetId),
        { initialProps: { budgetId: 'budget-1' } }
      );

      await waitFor(() => {
        expect(result.current.budget?.id).toBe('budget-1');
      });

      rerender({ budgetId: 'budget-2' });

      await waitFor(() => {
        expect(result.current.budget?.id).toBe('budget-2');
      });

      expect(budgetsApi.fetchBudget).toHaveBeenCalledTimes(2);
      expect(budgetsApi.fetchBudget).toHaveBeenLastCalledWith('budget-2');
    });
  });

  describe('Error Handling', () => {
    it('should handle fetch errors', async () => {
      const testError = new Error('Network error');
      vi.mocked(budgetsApi.fetchBudget).mockRejectedValue(testError);

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.error).toBeTruthy();
      expect(result.current.error?.message).toBe('Network error');
      expect(result.current.budget).toBeNull();
      expect(result.current.metrics).toBeNull();
    });

    it('should clear error on successful refetch', async () => {
      vi.mocked(budgetsApi.fetchBudget)
        .mockRejectedValueOnce(new Error('First error'))
        .mockResolvedValueOnce(mockBudget);

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      // Wait for error
      await waitFor(() => {
        expect(result.current.error).toBeTruthy();
      });

      // Refetch
      await act(async () => {
        await result.current.refetch();
      });

      expect(result.current.error).toBeNull();
      expect(result.current.budget).toEqual(mockBudget);
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function', async () => {
      vi.mocked(budgetsApi.fetchBudget).mockResolvedValue(mockBudget);

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(typeof result.current.refetch).toBe('function');
    });

    it('should refetch data when called', async () => {
      const updatedBudget: Budget = {
        ...mockBudget,
        name: 'Updated Budget',
      };

      vi.mocked(budgetsApi.fetchBudget)
        .mockResolvedValueOnce(mockBudget)
        .mockResolvedValueOnce(updatedBudget);

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.budget?.name).toBe('Monthly Budget');
      });

      // Call refetch
      await act(async () => {
        await result.current.refetch();
      });

      expect(result.current.budget?.name).toBe('Updated Budget');
      expect(budgetsApi.fetchBudget).toHaveBeenCalledTimes(2);
    });
  });

  describe('Edge Cases', () => {
    it('should handle budget without categories', async () => {
      const budgetNoCategories: Budget = {
        ...mockBudget,
        categories: [],
      };

      vi.mocked(budgetsApi.fetchBudget).mockResolvedValue(budgetNoCategories);

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.metrics?.total_allocated).toBe(0);
      expect(result.current.metrics?.total_spent).toBe(0);
    });

    it('should handle budget without income amount', async () => {
      const budgetNoIncome: Budget = {
        ...mockBudget,
        income_amount: 0,
      };

      vi.mocked(budgetsApi.fetchBudget).mockResolvedValue(budgetNoIncome);

      const { result } = renderHook(() => useBudgetDetail('budget-1'));

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.metrics?.unallocated_funds).toBe(-800); // 0 - 800
    });
  });
});
