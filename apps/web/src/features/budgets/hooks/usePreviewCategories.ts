/**
 * usePreviewCategories Hook
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #564: Migrated to generated React Query hooks
 * Issue #1073: Updated for PFC v2 canonical schema
 *
 * Hook for fetching category preview (transaction count and total amount)
 * with debouncing and request cancellation support.
 */

import { useState, useEffect, useRef, useMemo } from 'react';
import type { MatchLogic } from '@boostedbudget/shared-types/matching';
import {
  usePreviewCategoryMatches,
} from '@boostedbudget/api-client/endpoints/budget/budget';
import type { CategoryPreviewRequest, CategoryPreviewResponse } from '@boostedbudget/api-client/models';

export type BudgetPreviewContext = Pick<CategoryPreviewRequest, 'budget_id' | 'replace_category_id' | 'draft_categories' | 'group_name' | 'members'>;

export interface UsePreviewCategoriesResult {
  preview: CategoryPreviewResponse | null;
  loading: boolean;
  error: Error | null;
}

const DEBOUNCE_MS = 300;

const EMPTY_STATE: UsePreviewCategoriesResult = {
  preview: null,
  loading: false,
  error: null,
};

function hasPreviewCategory(
  categoryKind: CategoryPreviewRequest['category_kind'] | undefined,
  pfcPrimary: string | undefined,
  customCategoryId: string | undefined,
  context: BudgetPreviewContext | undefined
): boolean {
  switch (categoryKind) {
    case 'remainder':
      return true;
    case 'group':
      return !!context?.group_name && !!context.members?.length;
    case 'pfc':
      return !!pfcPrimary;
    case 'custom':
      return !!customCategoryId;
    default:
      return false;
  }
}

/**
 * Hook to fetch category preview with debouncing.
 *
 * @param categoryKind - 'pfc' or 'custom'
 * @param pfcPrimary - PFC primary enum (required when kind=pfc)
 * @param pfcDetailed - PFC detailed enum (optional when kind=pfc)
 * @param customCategoryId - Custom category UUID (required when kind=custom)
 * @param tags - Array of tag names to match
 * @param matchLogic - How to combine category and tag matching
 * @param startDate - Start of date range (YYYY-MM-DD)
 * @param endDate - End of date range (YYYY-MM-DD)
 * @param enabled - Whether to enable preview fetching
 */
export function usePreviewCategories(
  categoryKind: CategoryPreviewRequest['category_kind'] | undefined,
  pfcPrimary: string | undefined,
  pfcDetailed: string | undefined,
  customCategoryId: string | undefined,
  tags: string[],
  matchLogic: MatchLogic,
  startDate: string,
  endDate: string,
  enabled: boolean,
  context?: BudgetPreviewContext
): UsePreviewCategoriesResult {
  const [preview, setPreview] = useState<CategoryPreviewResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const mutation = usePreviewCategoryMatches();

  const requestIdRef = useRef(0);
  const debounceTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const hasCategory = hasPreviewCategory(categoryKind, pfcPrimary, customCategoryId, context);
  const contextKey = JSON.stringify(context);

  const shouldSkip = useMemo(
    () => !enabled || !categoryKind || !hasCategory,
    [enabled, categoryKind, hasCategory]
  );

  useEffect(() => {
    if (debounceTimeoutRef.current) {
      clearTimeout(debounceTimeoutRef.current);
      debounceTimeoutRef.current = null;
    }

    if (shouldSkip || !categoryKind) {
      return;
    }

    requestIdRef.current += 1;
    const currentRequestId = requestIdRef.current;

    const capturedKind = categoryKind;
    const capturedPfcPrimary = pfcPrimary;
    const capturedPfcDetailed = pfcDetailed;
    const capturedCustomId = customCategoryId;
    const capturedTags = tags;
    const capturedMatchLogic = matchLogic;
    const capturedStartDate = startDate;
    const capturedEndDate = endDate;

    debounceTimeoutRef.current = setTimeout(async () => {
      setLoading(true);
      setError(null);

      try {
        const result = await mutation.mutateAsync({
          data: {
            ...context,
            category_kind: capturedKind,
            pfc_primary: capturedPfcPrimary,
            pfc_detailed: capturedPfcDetailed,
            custom_category_id: capturedCustomId,
            tags: capturedTags.length > 0 ? capturedTags : undefined,
            match_logic: capturedMatchLogic,
            start_date: capturedStartDate,
            end_date: capturedEndDate,
          },
        });

        if (currentRequestId === requestIdRef.current) {
          setPreview(result);
          setError(null);
          setLoading(false);
        }
      } catch (err) {
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
  }, [categoryKind, pfcPrimary, pfcDetailed, customCategoryId, tags, matchLogic, startDate, endDate, shouldSkip, contextKey]);

  if (shouldSkip) {
    return EMPTY_STATE;
  }

  return { preview, loading, error };
}
