/**
 * useBudgets Hook
 *
 * React hook for budget CRUD operations.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { useState, useEffect, useCallback } from 'react';
import {
  fetchBudgets,
  createBudget as apiCreateBudget,
  updateBudget as apiUpdateBudget,
  deleteBudget as apiDeleteBudget,
  fetchAvailableCategories,
  addBudgetCategory,
  updateBudgetCategory,
  deleteBudgetCategory,
} from '../api/budgetsApi';
import type {
  Budget,
  BudgetCategory,
  CreateBudgetRequest,
  UpdateBudgetRequest,
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
} from '../types';

export interface UseBudgetsResult {
  budgets: Budget[];
  loading: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
  createBudget: (req: CreateBudgetRequest) => Promise<Budget>;
  updateBudget: (id: string, req: UpdateBudgetRequest) => Promise<Budget>;
  deleteBudget: (id: string) => Promise<void>;
  getAvailableCategories: () => Promise<string[]>;
  addCategory: (budgetId: string, req: CreateBudgetCategoryRequest) => Promise<BudgetCategory>;
  updateCategory: (
    budgetId: string,
    categoryId: string,
    req: UpdateBudgetCategoryRequest
  ) => Promise<BudgetCategory>;
  deleteCategory: (budgetId: string, categoryId: string) => Promise<void>;
}

/**
 * Hook for managing budgets with full CRUD operations
 *
 * @returns Object containing budgets data, loading state, error, and CRUD functions
 *
 * @example
 * ```tsx
 * function BudgetsList() {
 *   const {
 *     budgets,
 *     loading,
 *     error,
 *     createBudget,
 *     updateBudget,
 *     deleteBudget,
 *     refetch
 *   } = useBudgets();
 *
 *   if (loading) return <LoadingSpinner />;
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />;
 *
 *   return (
 *     <>
 *       {budgets.map(budget => <BudgetCard key={budget.id} data={budget} />)}
 *     </>
 *   );
 * }
 * ```
 */
export function useBudgets(): UseBudgetsResult {
  const [budgets, setBudgets] = useState<Budget[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);

  // Load budgets function
  const loadBudgets = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await fetchBudgets();
      setBudgets(result);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch budgets'));
      setBudgets([]);
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    loadBudgets();
  }, [loadBudgets]);

  // Refetch function
  const refetch = useCallback(async () => {
    await loadBudgets();
  }, [loadBudgets]);

  // Create budget
  const createBudget = useCallback(async (req: CreateBudgetRequest): Promise<Budget> => {
    const newBudget = await apiCreateBudget(req);
    setBudgets((prev) => [...prev, newBudget]);
    return newBudget;
  }, []);

  // Update budget
  const updateBudget = useCallback(
    async (id: string, req: UpdateBudgetRequest): Promise<Budget> => {
      const updatedBudget = await apiUpdateBudget(id, req);
      setBudgets((prev) => prev.map((b) => (b.id === id ? updatedBudget : b)));
      return updatedBudget;
    },
    []
  );

  // Delete budget
  const deleteBudget = useCallback(async (id: string): Promise<void> => {
    await apiDeleteBudget(id);
    setBudgets((prev) => prev.filter((b) => b.id !== id));
  }, []);

  // Get available categories
  const getAvailableCategories = useCallback(async (): Promise<string[]> => {
    return fetchAvailableCategories();
  }, []);

  // Add category to budget
  const addCategory = useCallback(
    async (budgetId: string, req: CreateBudgetCategoryRequest): Promise<BudgetCategory> => {
      const newCategory = await addBudgetCategory(budgetId, req);
      setBudgets((prev) =>
        prev.map((b) => {
          if (b.id === budgetId) {
            return {
              ...b,
              categories: [...(b.categories || []), newCategory],
            };
          }
          return b;
        })
      );
      return newCategory;
    },
    []
  );

  // Update category in budget
  const updateCategory = useCallback(
    async (
      budgetId: string,
      categoryId: string,
      req: UpdateBudgetCategoryRequest
    ): Promise<BudgetCategory> => {
      const updatedCategory = await updateBudgetCategory(budgetId, categoryId, req);
      setBudgets((prev) =>
        prev.map((b) => {
          if (b.id === budgetId) {
            return {
              ...b,
              categories: (b.categories || []).map((c) =>
                c.id === categoryId ? updatedCategory : c
              ),
            };
          }
          return b;
        })
      );
      return updatedCategory;
    },
    []
  );

  // Delete category from budget
  const deleteCategory = useCallback(
    async (budgetId: string, categoryId: string): Promise<void> => {
      await deleteBudgetCategory(budgetId, categoryId);
      setBudgets((prev) =>
        prev.map((b) => {
          if (b.id === budgetId) {
            return {
              ...b,
              categories: (b.categories || []).filter((c) => c.id !== categoryId),
            };
          }
          return b;
        })
      );
    },
    []
  );

  return {
    budgets,
    loading,
    error,
    refetch,
    createBudget,
    updateBudget,
    deleteBudget,
    getAvailableCategories,
    addCategory,
    updateCategory,
    deleteCategory,
  };
}
