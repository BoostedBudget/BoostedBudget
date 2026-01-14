/**
 * useBudgets Hook
 * Issue #564: Migrated to generated React Query hooks
 *
 * React hook for budget CRUD operations.
 */

import { useCallback, useMemo } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetBudgets,
  useCreateBudget as useCreateBudgetMutation,
  useUpdateBudget as useUpdateBudgetMutation,
  useDeleteBudget as useDeleteBudgetMutation,
  useAddBudgetCategory,
  useUpdateBudgetCategory,
  useDeleteBudgetCategory,
  getGetBudgetsQueryKey,
  getAvailableCategories,
} from '@boostedbudget/api-client/endpoints/budgets/budgets';
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
 */
export function useBudgets(): UseBudgetsResult {
  const queryClient = useQueryClient();

  // Query for fetching budgets
  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetBudgets({
    query: {
      staleTime: 30 * 1000, // 30 seconds
    },
  });

  // Convert error to Error instance
  const error = queryError
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch budgets')
    : null;

  // Convert generated type to local type (safely handle null/undefined)
  const budgets: Budget[] = useMemo(() => {
    if (!data) return [];
    return (Array.isArray(data) ? data : []) as unknown as Budget[];
  }, [data]);

  // Mutation hooks
  const createMutation = useCreateBudgetMutation();
  const updateMutation = useUpdateBudgetMutation();
  const deleteMutation = useDeleteBudgetMutation();
  const addCategoryMutation = useAddBudgetCategory();
  const updateCategoryMutation = useUpdateBudgetCategory();
  const deleteCategoryMutation = useDeleteBudgetCategory();

  // Refetch function
  const refetch = useCallback(async () => {
    await queryClient.invalidateQueries({
      queryKey: getGetBudgetsQueryKey(),
    });
    await queryRefetch();
  }, [queryClient, queryRefetch]);

  // Create budget
  const createBudget = useCallback(
    async (req: CreateBudgetRequest): Promise<Budget> => {
      const result = await createMutation.mutateAsync({ data: req as never });
      await refetch();
      return result as unknown as Budget;
    },
    [createMutation, refetch]
  );

  // Update budget
  const updateBudget = useCallback(
    async (id: string, req: UpdateBudgetRequest): Promise<Budget> => {
      const result = await updateMutation.mutateAsync({ id, data: req as never });
      await refetch();
      return result as unknown as Budget;
    },
    [updateMutation, refetch]
  );

  // Delete budget
  const deleteBudget = useCallback(
    async (id: string): Promise<void> => {
      await deleteMutation.mutateAsync({ id });
      await refetch();
    },
    [deleteMutation, refetch]
  );

  // Get available categories (uses the generated function directly)
  // Generated type is string[] | null (not { categories: string[] })
  const getAvailableCategoriesFunc = useCallback(async (): Promise<string[]> => {
    const result = await getAvailableCategories();
    return result ?? [];
  }, []);

  // Add category to budget
  const addCategory = useCallback(
    async (budgetId: string, req: CreateBudgetCategoryRequest): Promise<BudgetCategory> => {
      const result = await addCategoryMutation.mutateAsync({
        id: budgetId,
        data: req as never,
      });
      await refetch();
      return result as unknown as BudgetCategory;
    },
    [addCategoryMutation, refetch]
  );

  // Update category in budget
  const updateCategory = useCallback(
    async (
      budgetId: string,
      categoryId: string,
      req: UpdateBudgetCategoryRequest
    ): Promise<BudgetCategory> => {
      const result = await updateCategoryMutation.mutateAsync({
        id: budgetId,
        categoryId,
        data: req as never,
      });
      await refetch();
      return result as unknown as BudgetCategory;
    },
    [updateCategoryMutation, refetch]
  );

  // Delete category from budget
  const deleteCategory = useCallback(
    async (budgetId: string, categoryId: string): Promise<void> => {
      await deleteCategoryMutation.mutateAsync({ id: budgetId, categoryId });
      await refetch();
    },
    [deleteCategoryMutation, refetch]
  );

  return {
    budgets,
    loading: isLoading,
    error,
    refetch,
    createBudget,
    updateBudget,
    deleteBudget,
    getAvailableCategories: getAvailableCategoriesFunc,
    addCategory,
    updateCategory,
    deleteCategory,
  };
}
