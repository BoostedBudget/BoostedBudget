/**
 * usePreviewCategories Hook
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #564: Migrated to generated React Query hooks
 *
 * Hook for fetching category preview (transaction count and total amount)
 * with debouncing and request cancellation support.
 */

import { useState, useEffect, useRef, useMemo } from 'react';
import type { MatchLogic } from '@/shared/types/matching';
import {
  usePreviewCategoryMatches,
} from '@/api/generated/endpoints/budget/budget';
import type { CategoryPreviewResponse } from '@/api/generated/models';

export interface UsePreviewCategoriesResult {
  preview: CategoryPreviewResponse | null;
  loading: boolean;
  error: Error | null;
}

const DEBOUNCE_MS = 300;

// Empty state to return when disabled or no categories
const EMPTY_STATE: UsePreviewCategoriesResult = {
  preview: null,
  loading: false,
  error: null,
};

/**
 * Hook to fetch category preview with debouncing.
 *
 * @param categories - Array of category paths to match
 * @param tags - Array of tag names to match
 * @param matchLogic - How to combine category and tag matching
 * @param startDate - Start of date range (YYYY-MM-DD)
 * @param endDate - End of date range (YYYY-MM-DD)
 * @param enabled - Whether to enable preview fetching
 * @returns Preview result, loading state, and error
 */
export function usePreviewCategories(
  categories: string[][],
  tags: string[],
  matchLogic: MatchLogic,
  startDate: string,
  endDate: string,
  enabled: boolean
): UsePreviewCategoriesResult {
  const [preview, setPreview] = useState<CategoryPreviewResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // Use the generated mutation hook
  const mutation = usePreviewCategoryMatches();

  // Track the current request to handle cancellation
  const requestIdRef = useRef(0);
  const debounceTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Determine if we should skip fetching (used to return early state without effect)
  const shouldSkip = useMemo(
    () => !enabled || categories.length === 0,
    [enabled, categories.length]
  );

  useEffect(() => {
    // Clear any pending debounce
    if (debounceTimeoutRef.current) {
      clearTimeout(debounceTimeoutRef.current);
      debounceTimeoutRef.current = null;
    }

    // Skip if disabled or no categories - early return without setting state
    if (shouldSkip) {
      return;
    }

    // Increment request ID to invalidate any in-flight requests
    requestIdRef.current += 1;
    const currentRequestId = requestIdRef.current;

    // Capture current values for the async function
    const capturedCategories = categories;
    const capturedTags = tags;
    const capturedMatchLogic = matchLogic;
    const capturedStartDate = startDate;
    const capturedEndDate = endDate;

    // Debounce the API call
    debounceTimeoutRef.current = setTimeout(async () => {
      setLoading(true);
      setError(null);

      try {
        const result = await mutation.mutateAsync({
          data: {
            categories: capturedCategories,
            tags: capturedTags.length > 0 ? capturedTags : undefined,
            match_logic: capturedMatchLogic,
            start_date: capturedStartDate,
            end_date: capturedEndDate,
          },
        });

        // Only update state if this is still the current request
        if (currentRequestId === requestIdRef.current) {
          setPreview(result);
          setError(null);
          setLoading(false);
        }
      } catch (err) {
        // Only update state if this is still the current request
        if (currentRequestId === requestIdRef.current) {
          setError(err instanceof Error ? err : new Error('Failed to fetch preview'));
          setPreview(null);
          setLoading(false);
        }
      }
    }, DEBOUNCE_MS);

    return () => {
      if (debounceTimeoutRef.current) {
        clearTimeout(debounceTimeoutRef.current);
        debounceTimeoutRef.current = null;
      }
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [categories, tags, matchLogic, startDate, endDate, shouldSkip]);

  // Return empty state when skipping (avoids setState in effect)
  if (shouldSkip) {
    return EMPTY_STATE;
  }

  return { preview, loading, error };
}
