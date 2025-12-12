/**
 * useCurrentBudget Hook Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the React hook for fetching the current active budget.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useCurrentBudget } from './useCurrentBudget';
import * as budgetsApi from '../api/budgetsApi';
import type { Budget } from '../types';

// Mock the API module
vi.mock('../api/budgetsApi');

describe('useCurrentBudget', () => {
  const mockBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-123',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2025-01-01T00:00:00Z',
    end_date: '2025-01-31T23:59:59Z',
    amount: 2000,
    currency_code: 'USD',
    is_active: true,
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
        spent_amount: 150,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Initial State', () => {
    it('should return loading state initially', () => {
      // Arrange
      vi.mocked(budgetsApi.fetchCurrentBudget).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      // Act
      const { result } = renderHook(() => useCurrentBudget());

      // Assert
      expect(result.current.loading).toBe(true);
      expect(result.current.budget).toBeNull();
      expect(result.current.error).toBeNull();
    });

    it('should fetch and return current budget on mount', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(mockBudget);

      // Act
      const { result } = renderHook(() => useCurrentBudget());

      // Assert - Initial loading state
      expect(result.current.loading).toBe(true);

      // Wait for data to load - extended timeout for CI environment
      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      }, { timeout: 5000 });

      // Assert - Data loaded successfully
      expect(result.current.budget).toEqual(mockBudget);
      expect(result.current.error).toBeNull();
      expect(budgetsApi.fetchCurrentBudget).toHaveBeenCalledTimes(1);
    }, 60000); // Extended timeout for CI environment

    it('should handle 404 when no current budget exists', async () => {
      // Arrange
      const error = { response: { status: 404 } };
      vi.mocked(budgetsApi.fetchCurrentBudget).mockRejectedValue(error);

      // Act
      const { result } = renderHook(() => useCurrentBudget());

      // Wait for error handling
      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert - Should have null budget and no error (404 is expected case)
      expect(result.current.budget).toBeNull();
      expect(result.current.hasNoBudget).toBe(true);
    });

    it('should handle fetch errors gracefully', async () => {
      // Arrange
      const error = new Error('Failed to fetch current budget');
      vi.mocked(budgetsApi.fetchCurrentBudget).mockRejectedValue(error);

      // Act
      const { result } = renderHook(() => useCurrentBudget());

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.error).toEqual(error);
      expect(result.current.budget).toBeNull();
    });
  });

  describe('Computed Values', () => {
    it('should compute budget metrics correctly', async () => {
      // Arrange
      const budgetWithCategories: Budget = {
        ...mockBudget,
        amount: 1000,
        categories: [
          {
            id: 'cat-1',
            budget_id: 'budget-1',
            category: ['Food'],
            allocated_amount: 400,
            spent_amount: 200,
            created_at: '2025-01-01T00:00:00Z',
            updated_at: '2025-01-01T00:00:00Z',
          },
          {
            id: 'cat-2',
            budget_id: 'budget-1',
            category: ['Transport'],
            allocated_amount: 300,
            spent_amount: 100,
            created_at: '2025-01-01T00:00:00Z',
            updated_at: '2025-01-01T00:00:00Z',
          },
        ],
      };
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(budgetWithCategories);

      // Act
      const { result } = renderHook(() => useCurrentBudget());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert - Metrics should be computed
      expect(result.current.metrics).toBeDefined();
      expect(result.current.metrics?.total_allocated).toBe(700); // 400 + 300
      expect(result.current.metrics?.total_spent).toBe(300); // 200 + 100
      expect(result.current.metrics?.remaining_amount).toBe(700); // 1000 - 300
      expect(result.current.metrics?.percent_used).toBe(30); // (300/1000) * 100
      expect(result.current.metrics?.is_over_budget).toBe(false);
    });

    it('should detect over-budget status', async () => {
      // Arrange
      const overBudget: Budget = {
        ...mockBudget,
        amount: 500,
        categories: [
          {
            id: 'cat-1',
            budget_id: 'budget-1',
            category: ['Food'],
            allocated_amount: 600,
            spent_amount: 600,
            created_at: '2025-01-01T00:00:00Z',
            updated_at: '2025-01-01T00:00:00Z',
          },
        ],
      };
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(overBudget);

      // Act
      const { result } = renderHook(() => useCurrentBudget());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.metrics?.is_over_budget).toBe(true);
      expect(result.current.metrics?.percent_used).toBe(120); // 600/500 * 100
    });

    it('should return null metrics when no budget exists', async () => {
      // Arrange
      const error = { response: { status: 404 } };
      vi.mocked(budgetsApi.fetchCurrentBudget).mockRejectedValue(error);

      // Act
      const { result } = renderHook(() => useCurrentBudget());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.metrics).toBeNull();
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function to reload data', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(mockBudget);

      // Act
      const { result } = renderHook(() => useCurrentBudget());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(budgetsApi.fetchCurrentBudget).toHaveBeenCalledTimes(1);

      // Call refetch
      await act(async () => {
        await result.current.refetch();
      });

      // Assert
      expect(budgetsApi.fetchCurrentBudget).toHaveBeenCalledTimes(2);
    });
  });
});
