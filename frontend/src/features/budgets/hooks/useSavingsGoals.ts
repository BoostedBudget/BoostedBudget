/**
 * useSavingsGoals Hook
 * Issue #564: Migrated to generated React Query hooks
 *
 * React hook for savings goal CRUD operations.
 */

import { useCallback, useMemo } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetSavingsGoals,
  useGetActiveSavingsGoals,
  useCreateSavingsGoal,
  useUpdateSavingsGoal,
  useDeleteSavingsGoal,
  getGetSavingsGoalsQueryKey,
  getGetActiveSavingsGoalsQueryKey,
} from '@/api/generated/endpoints/savings-goals/savings-goals';
import { computeSavingsProgress } from '../types';
import type {
  SavingsGoal,
  CreateSavingsGoalRequest,
  UpdateSavingsGoalRequest,
} from '../types';

export interface SavingsGoalWithProgress extends SavingsGoal {
  progress: number;
}

export interface UseSavingsGoalsOptions {
  activeOnly?: boolean;
}

export interface UseSavingsGoalsResult {
  goals: SavingsGoal[];
  goalsWithProgress: SavingsGoalWithProgress[];
  loading: boolean;
  error: Error | null;
  totalTarget: number;
  totalSaved: number;
  refetch: () => Promise<void>;
  createGoal: (req: CreateSavingsGoalRequest) => Promise<SavingsGoal>;
  updateGoal: (id: string, req: UpdateSavingsGoalRequest) => Promise<SavingsGoal>;
  deleteGoal: (id: string) => Promise<void>;
}

/**
 * Hook for managing savings goals with full CRUD operations
 *
 * @param options - Configuration options including activeOnly filter
 * @returns Object containing goals data, computed values, loading state, error, and CRUD functions
 */
export function useSavingsGoals(options: UseSavingsGoalsOptions = {}): UseSavingsGoalsResult {
  const { activeOnly = false } = options;
  const queryClient = useQueryClient();

  // Query hook based on activeOnly option
  const allGoalsQuery = useGetSavingsGoals({
    query: {
      enabled: !activeOnly,
      staleTime: 30 * 1000,
    },
  });

  const activeGoalsQuery = useGetActiveSavingsGoals({
    query: {
      enabled: activeOnly,
      staleTime: 30 * 1000,
    },
  });

  // Use the appropriate query based on activeOnly
  const query = activeOnly ? activeGoalsQuery : allGoalsQuery;

  // Convert query error to Error instance
  const error = query.error
    ? query.error instanceof Error
      ? query.error
      : new Error('Failed to fetch savings goals')
    : null;

  // Convert generated type to local type (safely handle null/undefined)
  const goals: SavingsGoal[] = useMemo(() => {
    const data = query.data;
    if (!data) return [];
    // Handle both array format and potentially wrapped response
    return (Array.isArray(data) ? data : []) as unknown as SavingsGoal[];
  }, [query.data]);

  // Mutation hooks
  const createMutation = useCreateSavingsGoal();
  const updateMutation = useUpdateSavingsGoal();
  const deleteMutation = useDeleteSavingsGoal();

  // Refetch function
  const refetch = useCallback(async () => {
    if (activeOnly) {
      await queryClient.invalidateQueries({
        queryKey: getGetActiveSavingsGoalsQueryKey(),
      });
      await activeGoalsQuery.refetch();
    } else {
      await queryClient.invalidateQueries({
        queryKey: getGetSavingsGoalsQueryKey(),
      });
      await allGoalsQuery.refetch();
    }
  }, [queryClient, activeOnly, activeGoalsQuery, allGoalsQuery]);

  // Create goal
  const createGoal = useCallback(
    async (req: CreateSavingsGoalRequest): Promise<SavingsGoal> => {
      const result = await createMutation.mutateAsync({ data: req as never });
      // Invalidate queries to refresh the list
      await refetch();
      return result as unknown as SavingsGoal;
    },
    [createMutation, refetch]
  );

  // Update goal
  const updateGoal = useCallback(
    async (id: string, req: UpdateSavingsGoalRequest): Promise<SavingsGoal> => {
      const result = await updateMutation.mutateAsync({ id, data: req as never });
      // Invalidate queries to refresh the list
      await refetch();
      return result as unknown as SavingsGoal;
    },
    [updateMutation, refetch]
  );

  // Delete goal
  const deleteGoal = useCallback(
    async (id: string): Promise<void> => {
      await deleteMutation.mutateAsync({ id });
      // Invalidate queries to refresh the list
      await refetch();
    },
    [deleteMutation, refetch]
  );

  // Compute goals with progress
  const goalsWithProgress = useMemo<SavingsGoalWithProgress[]>(() => {
    return goals.map((goal) => ({
      ...goal,
      progress: computeSavingsProgress(goal),
    }));
  }, [goals]);

  // Compute totals
  const totalTarget = useMemo(() => {
    return goals.reduce((sum, goal) => sum + goal.target_amount, 0);
  }, [goals]);

  const totalSaved = useMemo(() => {
    return goals.reduce((sum, goal) => sum + goal.current_amount, 0);
  }, [goals]);

  return {
    goals,
    goalsWithProgress,
    loading: query.isLoading,
    error,
    totalTarget,
    totalSaved,
    refetch,
    createGoal,
    updateGoal,
    deleteGoal,
  };
}
