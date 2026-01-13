/**
 * useMilestoneTracking
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * Hook for tracking and persisting milestone progress.
 * Uses localStorage to remember which milestones have been seen/celebrated.
 */

import { useState, useCallback, useEffect, useMemo } from 'react';

const STORAGE_KEY_PREFIX = 'milestone_tracking_';

export interface UseMilestoneTrackingResult {
  /** Set of milestone percentages that have been seen */
  seenMilestones: Set<number>;
  /** Check if a specific milestone has been seen */
  hasSeenMilestone: (milestone: number) => boolean;
  /** Mark a milestone as seen (persists to localStorage) */
  markMilestoneSeen: (milestone: number) => void;
  /** Get milestones that are newly reached (not yet seen) */
  getNewlyReachedMilestones: (progress: number, milestones: number[]) => number[];
  /** Clear all seen milestones for this goal */
  clearSeenMilestones: () => void;
}

/**
 * Load seen milestones from localStorage
 */
function loadSeenMilestones(goalId: string): Set<number> {
  try {
    const stored = localStorage.getItem(`${STORAGE_KEY_PREFIX}${goalId}`);
    if (stored) {
      const parsed = JSON.parse(stored);
      if (Array.isArray(parsed)) {
        return new Set(parsed);
      }
    }
  } catch {
    // Invalid JSON or other error - return empty set
    console.warn(`Failed to load milestone tracking for goal ${goalId}`);
  }
  return new Set();
}

/**
 * Save seen milestones to localStorage
 */
function saveSeenMilestones(goalId: string, milestones: Set<number>): void {
  const key = `${STORAGE_KEY_PREFIX}${goalId}`;
  if (milestones.size === 0) {
    localStorage.removeItem(key);
  } else {
    localStorage.setItem(key, JSON.stringify([...milestones]));
  }
}

/**
 * Hook for tracking goal milestones with localStorage persistence
 */
export function useMilestoneTracking(goalId: string): UseMilestoneTrackingResult {
  const [seenMilestones, setSeenMilestones] = useState<Set<number>>(() =>
    loadSeenMilestones(goalId)
  );

  // Reload if goalId changes
  useEffect(() => {
    setSeenMilestones(loadSeenMilestones(goalId));
  }, [goalId]);

  const hasSeenMilestone = useCallback(
    (milestone: number): boolean => {
      return seenMilestones.has(milestone);
    },
    [seenMilestones]
  );

  const markMilestoneSeen = useCallback(
    (milestone: number): void => {
      setSeenMilestones((prev) => {
        const next = new Set(prev);
        next.add(milestone);
        saveSeenMilestones(goalId, next);
        return next;
      });
    },
    [goalId]
  );

  const getNewlyReachedMilestones = useCallback(
    (progress: number, milestones: number[]): number[] => {
      if (progress <= 0 || milestones.length === 0) {
        return [];
      }

      // Find milestones that:
      // 1. Are at or below the current progress
      // 2. Have not been seen before
      const sortedMilestones = [...milestones].sort((a, b) => a - b);
      const newlyReached = sortedMilestones.filter(
        (m) => progress >= m && !seenMilestones.has(m)
      );

      return newlyReached;
    },
    [seenMilestones]
  );

  const clearSeenMilestones = useCallback((): void => {
    localStorage.removeItem(`${STORAGE_KEY_PREFIX}${goalId}`);
    setSeenMilestones(new Set());
  }, [goalId]);

  // Memoize the result to prevent unnecessary re-renders
  return useMemo(
    () => ({
      seenMilestones,
      hasSeenMilestone,
      markMilestoneSeen,
      getNewlyReachedMilestones,
      clearSeenMilestones,
    }),
    [seenMilestones, hasSeenMilestone, markMilestoneSeen, getNewlyReachedMilestones, clearSeenMilestones]
  );
}
