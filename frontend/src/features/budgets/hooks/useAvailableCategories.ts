/**
 * useAvailableCategories Hook
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * React hook for fetching available Plaid categories and transforming
 * them from flat strings to nested string arrays.
 */

import { useState, useEffect, useCallback } from 'react';
import { fetchAvailableCategories } from '../api/budgetsApi';

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
  const [categories, setCategories] = useState<string[][]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);

  // Load categories function
  const loadCategories = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const flatCategories = await fetchAvailableCategories();
      // Transform flat strings to nested arrays, filtering out empty strings
      const transformedCategories = flatCategories
        .filter((cat) => cat.length > 0)
        .map(transformCategory);
      setCategories(transformedCategories);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch categories'));
      setCategories([]);
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    loadCategories();
  }, [loadCategories]);

  // Refetch function
  const refetch = useCallback(async () => {
    await loadCategories();
  }, [loadCategories]);

  return {
    categories,
    loading,
    error,
    refetch,
  };
}
