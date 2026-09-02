/**
 * useSavingsGoals Hook
 * Issue #954: React hook for savings goal CRUD operations
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
  getGetDeletedSavingsGoalsQueryKey,
} from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals';
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

export function useSavingsGoals(options: UseSavingsGoalsOptions = {}): UseSavingsGoalsResult {
  const { activeOnly = false } = options;
  const queryClient = useQueryClient();

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

  const query = activeOnly ? activeGoalsQuery : allGoalsQuery;

  const error = query.error
    ? query.error instanceof Error
      ? query.error
      : new Error('Failed to fetch savings goals')
    : null;

  const goals: SavingsGoal[] = useMemo(() => {
    return (query.data || []) as unknown as SavingsGoal[];
  }, [query.data]);

  const createMutation = useCreateSavingsGoal();
  const updateMutation = useUpdateSavingsGoal();
  const deleteMutation = useDeleteSavingsGoal();

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

  const createGoal = useCallback(
    async (req: CreateSavingsGoalRequest): Promise<SavingsGoal> => {
      const result = await createMutation.mutateAsync({ data: req as never });
      await refetch();
      return result as unknown as SavingsGoal;
    },
    [createMutation, refetch]
  );

  const updateGoal = useCallback(
    async (id: string, req: UpdateSavingsGoalRequest): Promise<SavingsGoal> => {
      const result = await updateMutation.mutateAsync({ id, data: req as never });
      await refetch();
      return result as unknown as SavingsGoal;
    },
    [updateMutation, refetch]
  );

  const deleteGoal = useCallback(
    async (id: string): Promise<void> => {
      await deleteMutation.mutateAsync({ id });
      // The deleted-goals listing is now wrong, and NOTHING else will notice (#1569, Codex
      // finding 4). main.tsx sets a global five-minute staleTime, so React Query happily serves
      // the cached listing — and in the case that matters most, deleting the LAST goal,
      // SavingsPage switches to its empty branch and mounts <DeletedGoalsPanel> for the FIRST
      // time against exactly that stale entry. The panel self-hides on an empty listing, so the
      // user was shown "No savings goals yet", the purge affordance was unreachable, and the
      // confirm dialog's promise that their contributions were kept had nothing behind it.
      // Invalidated BEFORE the live-goals refetch below, so the panel's first mount already finds
      // the entry stale. Pinned at the cache level by useSavingsGoalsDeletedCache.test.tsx — a
      // test that mocks this hook cannot see this defect, because the defect IS the cache.
      await queryClient.invalidateQueries({
        queryKey: getGetDeletedSavingsGoalsQueryKey(),
      });
      await refetch();
    },
    [deleteMutation, queryClient, refetch]
  );

  const goalsWithProgress = useMemo<SavingsGoalWithProgress[]>(() => {
    return goals.map((goal) => ({
      ...goal,
      progress: computeSavingsProgress(goal),
    }));
  }, [goals]);

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
