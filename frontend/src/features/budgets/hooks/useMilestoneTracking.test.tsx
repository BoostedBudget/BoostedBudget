/**
 * useMilestoneTracking Tests
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * TDD tests for milestone tracking hook.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useMilestoneTracking } from './useMilestoneTracking';

describe('useMilestoneTracking', () => {
  // Mock localStorage
  const localStorageMock: Record<string, string> = {};

  beforeEach(() => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation((key) => localStorageMock[key] || null);
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation((key, value) => {
      localStorageMock[key] = value;
    });
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation((key) => {
      delete localStorageMock[key];
    });
    // Clear mock storage before each test
    Object.keys(localStorageMock).forEach((key) => delete localStorageMock[key]);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ==========================================================================
  // Basic Tracking Tests
  // ==========================================================================

  describe('Basic Tracking', () => {
    it('returns empty set for new goal', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      expect(result.current.seenMilestones).toEqual(new Set());
      expect(result.current.hasSeenMilestone(25)).toBe(false);
    });

    it('marks milestone as seen', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      act(() => {
        result.current.markMilestoneSeen(25);
      });

      expect(result.current.seenMilestones.has(25)).toBe(true);
      expect(result.current.hasSeenMilestone(25)).toBe(true);
    });

    it('persists seen milestones to localStorage', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      act(() => {
        result.current.markMilestoneSeen(25);
        result.current.markMilestoneSeen(50);
      });

      // Check localStorage was updated
      expect(localStorage.setItem).toHaveBeenCalled();
      const stored = JSON.parse(localStorageMock['milestone_tracking_goal-1']);
      expect(stored).toContain(25);
      expect(stored).toContain(50);
    });

    it('loads seen milestones from localStorage on mount', () => {
      // Pre-populate localStorage
      localStorageMock['milestone_tracking_goal-2'] = JSON.stringify([25, 50]);

      const { result } = renderHook(() => useMilestoneTracking('goal-2'));

      expect(result.current.seenMilestones.has(25)).toBe(true);
      expect(result.current.seenMilestones.has(50)).toBe(true);
      expect(result.current.hasSeenMilestone(75)).toBe(false);
    });
  });

  // ==========================================================================
  // New Milestone Detection Tests
  // ==========================================================================

  describe('New Milestone Detection', () => {
    it('detects newly reached milestones', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      // First check at 30% progress
      const newMilestones = result.current.getNewlyReachedMilestones(30, [25, 50, 75, 100]);

      // 25% is newly reached (not seen before)
      expect(newMilestones).toEqual([25]);
    });

    it('does not include already seen milestones', () => {
      // Pre-populate localStorage with 25% already seen
      localStorageMock['milestone_tracking_goal-3'] = JSON.stringify([25]);

      const { result } = renderHook(() => useMilestoneTracking('goal-3'));

      // Check at 60% progress
      const newMilestones = result.current.getNewlyReachedMilestones(60, [25, 50, 75, 100]);

      // Only 50% should be new (25% was already seen)
      expect(newMilestones).toEqual([50]);
    });

    it('returns multiple new milestones if progress jumps', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      // Jump from 0 to 80%
      const newMilestones = result.current.getNewlyReachedMilestones(80, [25, 50, 75, 100]);

      expect(newMilestones).toEqual([25, 50, 75]);
    });

    it('returns empty array if no new milestones reached', () => {
      // Pre-populate with 25% and 50% seen
      localStorageMock['milestone_tracking_goal-4'] = JSON.stringify([25, 50]);

      const { result } = renderHook(() => useMilestoneTracking('goal-4'));

      // Check at 45% progress (between seen milestones)
      const newMilestones = result.current.getNewlyReachedMilestones(45, [25, 50, 75, 100]);

      expect(newMilestones).toEqual([]);
    });

    it('handles 100% completion', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      const newMilestones = result.current.getNewlyReachedMilestones(100, [25, 50, 75, 100]);

      expect(newMilestones).toEqual([25, 50, 75, 100]);
    });
  });

  // ==========================================================================
  // Clear/Reset Tests
  // ==========================================================================

  describe('Clear and Reset', () => {
    it('clears all seen milestones', () => {
      // Pre-populate localStorage
      localStorageMock['milestone_tracking_goal-5'] = JSON.stringify([25, 50, 75]);

      const { result } = renderHook(() => useMilestoneTracking('goal-5'));

      // Verify initial state
      expect(result.current.seenMilestones.size).toBe(3);

      // Clear
      act(() => {
        result.current.clearSeenMilestones();
      });

      expect(result.current.seenMilestones.size).toBe(0);
      expect(localStorage.removeItem).toHaveBeenCalledWith('milestone_tracking_goal-5');
    });
  });

  // ==========================================================================
  // Edge Cases Tests
  // ==========================================================================

  describe('Edge Cases', () => {
    it('handles corrupted localStorage data', () => {
      // Invalid JSON in localStorage
      localStorageMock['milestone_tracking_goal-6'] = 'not-valid-json';

      const { result } = renderHook(() => useMilestoneTracking('goal-6'));

      // Should default to empty set
      expect(result.current.seenMilestones.size).toBe(0);
    });

    it('handles empty milestones array', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      const newMilestones = result.current.getNewlyReachedMilestones(50, []);

      expect(newMilestones).toEqual([]);
    });

    it('handles progress of 0', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      const newMilestones = result.current.getNewlyReachedMilestones(0, [25, 50, 75, 100]);

      expect(newMilestones).toEqual([]);
    });

    it('handles negative progress', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      const newMilestones = result.current.getNewlyReachedMilestones(-10, [25, 50, 75, 100]);

      expect(newMilestones).toEqual([]);
    });

    it('handles progress > 100', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'));

      const newMilestones = result.current.getNewlyReachedMilestones(150, [25, 50, 75, 100]);

      expect(newMilestones).toEqual([25, 50, 75, 100]);
    });

    it('isolates tracking per goal ID', () => {
      const { result: result1 } = renderHook(() => useMilestoneTracking('goal-a'));
      const { result: result2 } = renderHook(() => useMilestoneTracking('goal-b'));

      act(() => {
        result1.current.markMilestoneSeen(25);
      });

      expect(result1.current.hasSeenMilestone(25)).toBe(true);
      expect(result2.current.hasSeenMilestone(25)).toBe(false);
    });
  });
});
