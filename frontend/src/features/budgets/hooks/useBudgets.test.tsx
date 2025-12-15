/**
 * useBudgets Hook Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the React hook for budget CRUD operations.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useBudgets } from './useBudgets';
import * as budgetsApi from '../api/budgetsApi';
import type { Budget, CreateBudgetRequest, UpdateBudgetRequest } from '../types';

// Mock the API module
vi.mock('../api/budgetsApi');

describe('useBudgets', () => {
  const mockBudgets: Budget[] = [
    {
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
          // Issue #495: Envelope budgeting fields
          tags: [],
          match_logic: 'category_only',
        },
      ],
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
      // Issue #495: Envelope budgeting fields
      income_amount: 0,
      income_sources: [],
    },
    {
      id: 'budget-2',
      user_id: 'user-123',
      name: 'Weekly Groceries',
      period: 'weekly',
      start_date: '2025-01-06T00:00:00Z',
      amount: 200,
      currency_code: 'USD',
      is_active: true,
      categories: [],
      created_at: '2025-01-06T00:00:00Z',
      updated_at: '2025-01-06T00:00:00Z',
      // Issue #495: Envelope budgeting fields
      income_amount: 0,
      income_sources: [],
    },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Initial State', () => {
    it('should return loading state initially', () => {
      // Arrange
      vi.mocked(budgetsApi.fetchBudgets).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      // Act
      const { result } = renderHook(() => useBudgets());

      // Assert
      expect(result.current.loading).toBe(true);
      expect(result.current.budgets).toEqual([]);
      expect(result.current.error).toBeNull();
    });

    it('should fetch and return budgets on mount', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);

      // Act
      const { result } = renderHook(() => useBudgets());

      // Assert - Initial loading state
      expect(result.current.loading).toBe(true);

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert - Data loaded successfully
      expect(result.current.budgets).toEqual(mockBudgets);
      expect(result.current.error).toBeNull();
      expect(budgetsApi.fetchBudgets).toHaveBeenCalledTimes(1);
    });

    it('should handle fetch errors gracefully', async () => {
      // Arrange
      const error = new Error('Failed to fetch budgets');
      vi.mocked(budgetsApi.fetchBudgets).mockRejectedValue(error);

      // Act
      const { result } = renderHook(() => useBudgets());

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.error).toEqual(error);
      expect(result.current.budgets).toEqual([]);
    });

    it('should return empty array when no budgets exist', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue([]);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.budgets).toEqual([]);
      expect(result.current.error).toBeNull();
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function to reload data', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(budgetsApi.fetchBudgets).toHaveBeenCalledTimes(1);

      // Call refetch
      await act(async () => {
        await result.current.refetch();
      });

      // Assert
      expect(budgetsApi.fetchBudgets).toHaveBeenCalledTimes(2);
    });

    it('should update data after refetch', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchBudgets)
        .mockResolvedValueOnce([mockBudgets[0]])
        .mockResolvedValueOnce(mockBudgets);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.budgets).toHaveLength(1);

      // Refetch
      await act(async () => {
        await result.current.refetch();
      });

      // Assert
      expect(result.current.budgets).toHaveLength(2);
    });
  });

  describe('Create Budget', () => {
    it('should create a new budget and add it to the list', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue([]);
      vi.mocked(budgetsApi.createBudget).mockResolvedValue(mockBudgets[0]);

      const createRequest: CreateBudgetRequest = {
        name: 'Monthly Budget',
        period: 'monthly',
        start_date: '2025-01-01T00:00:00Z',
        amount: 2000,
      };

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.budgets).toHaveLength(0);

      // Create budget
      let createdBudget: Budget | undefined;
      await act(async () => {
        createdBudget = await result.current.createBudget(createRequest);
      });

      // Assert
      expect(budgetsApi.createBudget).toHaveBeenCalledWith(createRequest);
      expect(createdBudget).toEqual(mockBudgets[0]);
      expect(result.current.budgets).toHaveLength(1);
      expect(result.current.budgets[0]).toEqual(mockBudgets[0]);
    });

    it('should handle create errors and not add budget to list', async () => {
      // Arrange
      const error = new Error('Failed to create budget');
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue([]);
      vi.mocked(budgetsApi.createBudget).mockRejectedValue(error);

      const createRequest: CreateBudgetRequest = {
        name: 'Monthly Budget',
        period: 'monthly',
        start_date: '2025-01-01T00:00:00Z',
        amount: 2000,
      };

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Try to create budget
      await act(async () => {
        await expect(result.current.createBudget(createRequest)).rejects.toThrow(error);
      });

      // Assert - budget list should remain empty
      expect(result.current.budgets).toHaveLength(0);
    });
  });

  describe('Update Budget', () => {
    it('should update an existing budget in the list', async () => {
      // Arrange
      const updatedBudget: Budget = {
        ...mockBudgets[0],
        name: 'Updated Monthly Budget',
        amount: 2500,
      };
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);
      vi.mocked(budgetsApi.updateBudget).mockResolvedValue(updatedBudget);

      const updateRequest: UpdateBudgetRequest = {
        name: 'Updated Monthly Budget',
        amount: 2500,
      };

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Update budget
      let returnedBudget: Budget | undefined;
      await act(async () => {
        returnedBudget = await result.current.updateBudget('budget-1', updateRequest);
      });

      // Assert
      expect(budgetsApi.updateBudget).toHaveBeenCalledWith('budget-1', updateRequest);
      expect(returnedBudget).toEqual(updatedBudget);
      expect(result.current.budgets[0].name).toBe('Updated Monthly Budget');
      expect(result.current.budgets[0].amount).toBe(2500);
      // Other budgets should remain unchanged
      expect(result.current.budgets[1]).toEqual(mockBudgets[1]);
    });

    it('should handle update errors without modifying the list', async () => {
      // Arrange
      const error = new Error('Failed to update budget');
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);
      vi.mocked(budgetsApi.updateBudget).mockRejectedValue(error);

      const updateRequest: UpdateBudgetRequest = {
        name: 'Updated Monthly Budget',
      };

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Try to update budget
      await act(async () => {
        await expect(result.current.updateBudget('budget-1', updateRequest)).rejects.toThrow(error);
      });

      // Assert - budget should remain unchanged
      expect(result.current.budgets[0].name).toBe('Monthly Budget');
    });
  });

  describe('Delete Budget', () => {
    it('should delete a budget and remove it from the list', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);
      vi.mocked(budgetsApi.deleteBudget).mockResolvedValue(undefined);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.budgets).toHaveLength(2);

      // Delete budget
      await act(async () => {
        await result.current.deleteBudget('budget-1');
      });

      // Assert
      expect(budgetsApi.deleteBudget).toHaveBeenCalledWith('budget-1');
      expect(result.current.budgets).toHaveLength(1);
      expect(result.current.budgets[0].id).toBe('budget-2');
    });

    it('should handle delete errors without removing budget from list', async () => {
      // Arrange
      const error = new Error('Failed to delete budget');
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);
      vi.mocked(budgetsApi.deleteBudget).mockRejectedValue(error);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Try to delete budget
      await act(async () => {
        await expect(result.current.deleteBudget('budget-1')).rejects.toThrow(error);
      });

      // Assert - budget should still be in the list
      expect(result.current.budgets).toHaveLength(2);
    });
  });

  describe('Get Available Categories', () => {
    it('should fetch available categories', async () => {
      // Arrange
      const categories = ['Food and Drink', 'Transportation', 'Shopping'];
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue([]);
      vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue(categories);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      let availableCategories: string[] | undefined;
      await act(async () => {
        availableCategories = await result.current.getAvailableCategories();
      });

      // Assert
      expect(budgetsApi.fetchAvailableCategories).toHaveBeenCalled();
      expect(availableCategories).toEqual(categories);
    });
  });

  describe('Budget Categories', () => {
    it('should add a category to a budget', async () => {
      // Arrange
      const newCategory = {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation'],
        allocated_amount: 300,
        spent_amount: 0,
        created_at: '2025-01-15T00:00:00Z',
        updated_at: '2025-01-15T00:00:00Z',
        // Issue #495: Envelope budgeting fields
        tags: [],
        match_logic: 'category_only' as const,
      };
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);
      vi.mocked(budgetsApi.addBudgetCategory).mockResolvedValue(newCategory);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.addCategory('budget-1', {
          category: ['Transportation'],
          allocated_amount: 300,
        });
      });

      // Assert
      expect(budgetsApi.addBudgetCategory).toHaveBeenCalledWith('budget-1', {
        category: ['Transportation'],
        allocated_amount: 300,
      });
      // Budget should now have 2 categories
      expect(result.current.budgets[0].categories).toHaveLength(2);
      expect(result.current.budgets[0].categories?.[1]).toEqual(newCategory);
    });

    it('should update a category in a budget', async () => {
      // Arrange
      const updatedCategory = {
        ...mockBudgets[0].categories![0],
        allocated_amount: 600,
      };
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);
      vi.mocked(budgetsApi.updateBudgetCategory).mockResolvedValue(updatedCategory);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.updateCategory('budget-1', 'cat-1', {
          allocated_amount: 600,
        });
      });

      // Assert
      expect(budgetsApi.updateBudgetCategory).toHaveBeenCalledWith('budget-1', 'cat-1', {
        allocated_amount: 600,
      });
      expect(result.current.budgets[0].categories?.[0].allocated_amount).toBe(600);
    });

    it('should delete a category from a budget', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchBudgets).mockResolvedValue(mockBudgets);
      vi.mocked(budgetsApi.deleteBudgetCategory).mockResolvedValue(undefined);

      // Act
      const { result } = renderHook(() => useBudgets());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.budgets[0].categories).toHaveLength(1);

      await act(async () => {
        await result.current.deleteCategory('budget-1', 'cat-1');
      });

      // Assert
      expect(budgetsApi.deleteBudgetCategory).toHaveBeenCalledWith('budget-1', 'cat-1');
      expect(result.current.budgets[0].categories).toHaveLength(0);
    });
  });
});
