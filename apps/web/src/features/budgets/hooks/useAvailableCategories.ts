/**
 * useAvailableCategories Hook
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #564: Migrated to generated React Query hooks
 *
 * React hook for fetching available Plaid categories and transforming
 * them from flat strings to nested string arrays.
 */

import { useMemo, useCallback } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetAvailableCategories,
  getGetAvailableCategoriesQueryKey,
} from '@boostedbudget/api-client/endpoints/budgets/budgets';

export interface UseAvailableCategoriesResult {
  /** List of available categories as string[][] */
  categories: string[][];
  /** Whether the categories are currently loading */
  loading: boolean;
  /** Any error that occurred during fetching */
  error: Error | null;
  /** Function to refetch the categories */
  refetch: () => Promise<void>;
}

/**
 * Transforms a flat category string like "Food and Drink > Restaurants"
 * into a string array like ["Food and Drink", "Restaurants"]
 */
function transformCategory(flatCategory: string): string[] {
  return flatCategory
    .split(' > ')
    .map((part) => part.trim())
    .filter((part) => part.length > 0);
}

/**
 * Hook for fetching and managing available Plaid categories
 *
 * The API returns categories as flat strings (e.g., "Food and Drink > Restaurants")
 * and this hook transforms them into nested arrays (e.g., ["Food and Drink", "Restaurants"])
 * for use in the BudgetCategoryForm.
 *
 * @returns Object containing categories, loading state, error, and refetch function
 *
 * @example
 * ```tsx
 * function CategorySelector() {
 *   const { categories, loading, error, refetch } = useAvailableCategories();
 *
 *   if (loading) return <LoadingSpinner />;
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />;
 *
 *   return (
 *     <select>
 *       {categories.map(cat => (
 *         <option key={cat.join(' > ')} value={cat.join(' > ')}>
 *           {cat.join(' > ')}
 *         </option>
 *       ))}
 *     </select>
 *   );
 * }
 * ```
 */
export function useAvailableCategories(): UseAvailableCategoriesResult {
  const queryClient = useQueryClient();

  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetAvailableCategories({
    query: {
      staleTime: 5 * 60 * 1000, // 5 minutes - categories don't change often
    },
  });

  // Convert error to Error instance
  const error = queryError
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch categories')
    : null;

  // Transform flat strings to nested arrays
  // Generated type is string[] | null (not { categories: string[] })
  const categories = useMemo(() => {
    if (!data) return [];
    return data
      .filter((cat) => cat.length > 0)
      .map(transformCategory);
  }, [data]);

  // Refetch function that invalidates the cache
  const refetch = useCallback(async () => {
    await queryClient.invalidateQueries({
      queryKey: getGetAvailableCategoriesQueryKey(),
    });
    await queryRefetch();
  }, [queryClient, queryRefetch]);

  return {
    categories,
    loading: isLoading,
    error,
    refetch,
  };
}
