/**
 * useAutoCategorizationRules Hook
 *
 * React hook for managing auto-categorization rules - fetching, creating, updating, deleting, and toggling.
 * Issue #346: Custom Transaction Categorization Rules
 * Issue #564: Migrated to generated React Query hooks
 */

import { useMemo, useCallback } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetUserRules,
  useCreateRule,
  useUpdateRule,
  useDeleteRule,
  useToggleRule,
  getGetUserRulesQueryKey,
} from '@/api/generated/endpoints/auto-categorization/auto-categorization';
import type {
  CategorizationRule,
  CreateRuleRequest,
  UpdateRuleRequest,
  UseAutoCategorizationRulesResult,
} from '../types';

/**
 * Hook for managing auto-categorization rules
 *
 * Features:
 * - Fetches rules on mount
 * - CRUD operations with optimistic updates
 * - Toggle enabled/disabled status
 * - Manual refresh
 * - Error handling
 *
 * @returns Object containing rules, loading state, error, and CRUD methods
 *
 * @example
 * ```tsx
 * function AutoCategorizationRulesList() {
 *   const {
 *     rules,
 *     loading,
 *     error,
 *     createRule,
 *     updateRule,
 *     deleteRule,
 *     toggleRule,
 *     refresh
 *   } = useAutoCategorizationRules()
 *
 *   if (loading) return <LoadingSpinner />
 *   if (error) return <ErrorMessage error={error} onRetry={refresh} />
 *
 *   return (
 *     <>
 *       {rules.map(rule => (
 *         <AutoCategorizationRuleCard
 *           key={rule.id}
 *           rule={rule}
 *           onToggle={(enabled) => toggleRule(rule.id, enabled)}
 *           onDelete={() => deleteRule(rule.id)}
 *         />
 *       ))}
 *     </>
 *   )
 * }
 * ```
 */
export function useAutoCategorizationRules(): UseAutoCategorizationRulesResult {
  const queryClient = useQueryClient();

  // Use generated query hook for fetching rules
  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetUserRules({
    query: {
      staleTime: 30 * 1000, // 30 seconds
    },
  });

  // Use generated mutation hooks
  const createMutation = useCreateRule();
  const updateMutation = useUpdateRule();
  const deleteMutation = useDeleteRule();
  const toggleMutation = useToggleRule();

  // Sort by priority (higher first), then by created_at (newer first)
  const rules = useMemo(() => {
    if (!data) return [];
    const ruleList = data as unknown as CategorizationRule[];
    return [...ruleList].sort((a, b) => {
      if (b.priority !== a.priority) {
        return b.priority - a.priority;
      }
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    });
  }, [data]);

  // Convert error to string
  const error = useMemo<string | null>(() => {
    if (!queryError) return null;
    return queryError instanceof Error ? queryError.message : 'Failed to fetch rules';
  }, [queryError]);

  // Create a new rule
  const createRule = useCallback(
    async (request: CreateRuleRequest): Promise<CategorizationRule> => {
      const result = await createMutation.mutateAsync({
        data: request as never,
      });
      await queryClient.invalidateQueries({
        queryKey: getGetUserRulesQueryKey(),
      });
      return result as unknown as CategorizationRule;
    },
    [createMutation, queryClient]
  );

  // Update an existing rule
  const updateRule = useCallback(
    async (ruleId: string, request: UpdateRuleRequest): Promise<CategorizationRule> => {
      const result = await updateMutation.mutateAsync({
        id: ruleId,
        data: request as never,
      });
      await queryClient.invalidateQueries({
        queryKey: getGetUserRulesQueryKey(),
      });
      return result as unknown as CategorizationRule;
    },
    [updateMutation, queryClient]
  );

  // Delete a rule
  const deleteRule = useCallback(
    async (ruleId: string): Promise<void> => {
      await deleteMutation.mutateAsync({ id: ruleId });
      await queryClient.invalidateQueries({
        queryKey: getGetUserRulesQueryKey(),
      });
    },
    [deleteMutation, queryClient]
  );

  // Toggle rule enabled status
  const toggleRule = useCallback(
    async (ruleId: string, enabled: boolean): Promise<CategorizationRule> => {
      const result = await toggleMutation.mutateAsync({
        id: ruleId,
        data: { enabled },
      });
      await queryClient.invalidateQueries({
        queryKey: getGetUserRulesQueryKey(),
      });
      return result as unknown as CategorizationRule;
    },
    [toggleMutation, queryClient]
  );

  // Refresh rules
  const refresh = useCallback(async (): Promise<void> => {
    await queryClient.invalidateQueries({
      queryKey: getGetUserRulesQueryKey(),
    });
    await queryRefetch();
  }, [queryClient, queryRefetch]);

  return {
    rules,
    loading: isLoading,
    error,
    createRule,
    updateRule,
    deleteRule,
    toggleRule,
    refresh,
  };
}
