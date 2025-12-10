/**
 * useSavingsGoals Hook
 *
 * React hook for savings goal CRUD operations.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { useState, useEffect, useCallback, useMemo } from 'react';
import {
  fetchSavingsGoals,
  fetchActiveSavingsGoals,
  createSavingsGoal as apiCreateSavingsGoal,
  updateSavingsGoal as apiUpdateSavingsGoal,
  deleteSavingsGoal as apiDeleteSavingsGoal,
} from '../api/budgetsApi';
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
 *
 * @example
 * ```tsx
 * function SavingsGoalsList() {
 *   const {
 *     goals,
 *     goalsWithProgress,
 *     loading,
 *     error,
 *     totalTarget,
 *     totalSaved,
 *     createGoal,
 *     updateGoal,
 *     deleteGoal,
 *     refetch
 *   } = useSavingsGoals({ activeOnly: true });
 *
 *   if (loading) return <LoadingSpinner />;
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />;
 *
 *   return (
 *     <>
 *       <TotalProgress current={totalSaved} target={totalTarget} />
 *       {goalsWithProgress.map(goal => (
 *         <SavingsGoalCard key={goal.id} goal={goal} progress={goal.progress} />
 *       ))}
 *     </>
 *   );
 * }
 * ```
 */
export function useSavingsGoals(options: UseSavingsGoalsOptions = {}): UseSavingsGoalsResult {
  const { activeOnly = false } = options;

  const [goals, setGoals] = useState<SavingsGoal[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);

  // Load goals function
  const loadGoals = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const result = activeOnly ? await fetchActiveSavingsGoals() : await fetchSavingsGoals();
      setGoals(result);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch savings goals'));
      setGoals([]);
    } finally {
      setLoading(false);
    }
  }, [activeOnly]);

  // Initial load
  useEffect(() => {
    loadGoals();
  }, [loadGoals]);

  // Refetch function
  const refetch = useCallback(async () => {
    await loadGoals();
  }, [loadGoals]);

  // Create goal
  const createGoal = useCallback(async (req: CreateSavingsGoalRequest): Promise<SavingsGoal> => {
    const newGoal = await apiCreateSavingsGoal(req);
    setGoals((prev) => [...prev, newGoal]);
    return newGoal;
  }, []);

  // Update goal
  const updateGoal = useCallback(
    async (id: string, req: UpdateSavingsGoalRequest): Promise<SavingsGoal> => {
      const updatedGoal = await apiUpdateSavingsGoal(id, req);
      setGoals((prev) => prev.map((g) => (g.id === id ? updatedGoal : g)));
      return updatedGoal;
    },
    []
  );

  // Delete goal
  const deleteGoal = useCallback(async (id: string): Promise<void> => {
    await apiDeleteSavingsGoal(id);
    setGoals((prev) => prev.filter((g) => g.id !== id));
  }, []);

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
    loading,
    error,
    totalTarget,
    totalSaved,
    refetch,
    createGoal,
    updateGoal,
    deleteGoal,
  };
}
