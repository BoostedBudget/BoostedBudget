/**
 * useMilestoneTracking Tests
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * TDD tests for milestone tracking hook.
 */

import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useMilestoneTracking } from './useMilestoneTracking'

describe('useMilestoneTracking', () => {
  const localStorageMock: Record<string, string> = {}

  // Save the entire original localStorage object
  const originalLocalStorage = window.localStorage

  const mockGetItem = mock((key: string) => localStorageMock[key] || null)
  const mockSetItem = mock((key: string, value: string) => {
    localStorageMock[key] = value
  })
  const mockRemoveItem = mock((key: string) => {
    delete localStorageMock[key]
  })

  beforeEach(() => {
    Object.keys(localStorageMock).forEach((key) => delete localStorageMock[key])
    mockGetItem.mockClear()
    mockSetItem.mockClear()
    mockRemoveItem.mockClear()

    Object.defineProperty(window, 'localStorage', {
      value: {
        getItem: mockGetItem,
        setItem: mockSetItem,
        removeItem: mockRemoveItem,
        clear: mock(() => {}),
        key: mock(() => null),
        length: 0,
      },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    // Restore the original localStorage object completely
    Object.defineProperty(window, 'localStorage', {
      value: originalLocalStorage,
      writable: true,
      configurable: true,
    })
  })

  describe('Basic Tracking', () => {
    it('returns empty set for new goal', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      expect(result.current.seenMilestones).toEqual(new Set())
      expect(result.current.hasSeenMilestone(25)).toBe(false)
    })

    it('marks milestone as seen', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      act(() => {
        result.current.markMilestoneSeen(25)
      })

      expect(result.current.seenMilestones.has(25)).toBe(true)
      expect(result.current.hasSeenMilestone(25)).toBe(true)
    })

    it('persists seen milestones to localStorage', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      act(() => {
        result.current.markMilestoneSeen(25)
        result.current.markMilestoneSeen(50)
      })

      expect(mockSetItem).toHaveBeenCalled()
      const stored = JSON.parse(localStorageMock['milestone_tracking_goal-1'])
      expect(stored).toContain(25)
      expect(stored).toContain(50)
    })

    it('loads seen milestones from localStorage on mount', () => {
      localStorageMock['milestone_tracking_goal-2'] = JSON.stringify([25, 50])

      const { result } = renderHook(() => useMilestoneTracking('goal-2'))

      expect(result.current.seenMilestones.has(25)).toBe(true)
      expect(result.current.seenMilestones.has(50)).toBe(true)
      expect(result.current.hasSeenMilestone(75)).toBe(false)
    })
  })

  describe('New Milestone Detection', () => {
    it('detects newly reached milestones', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      const newMilestones = result.current.getNewlyReachedMilestones(30, [25, 50, 75, 100])

      expect(newMilestones).toEqual([25])
    })

    it('does not include already seen milestones', () => {
      localStorageMock['milestone_tracking_goal-3'] = JSON.stringify([25])

      const { result } = renderHook(() => useMilestoneTracking('goal-3'))

      const newMilestones = result.current.getNewlyReachedMilestones(60, [25, 50, 75, 100])

      expect(newMilestones).toEqual([50])
    })

    it('returns multiple new milestones if progress jumps', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      const newMilestones = result.current.getNewlyReachedMilestones(80, [25, 50, 75, 100])

      expect(newMilestones).toEqual([25, 50, 75])
    })

    it('returns empty array if no new milestones reached', () => {
      localStorageMock['milestone_tracking_goal-4'] = JSON.stringify([25, 50])

      const { result } = renderHook(() => useMilestoneTracking('goal-4'))

      const newMilestones = result.current.getNewlyReachedMilestones(45, [25, 50, 75, 100])

      expect(newMilestones).toEqual([])
    })

    it('handles 100% completion', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      const newMilestones = result.current.getNewlyReachedMilestones(100, [25, 50, 75, 100])

      expect(newMilestones).toEqual([25, 50, 75, 100])
    })
  })

  describe('Clear and Reset', () => {
    it('clears all seen milestones', () => {
      localStorageMock['milestone_tracking_goal-5'] = JSON.stringify([25, 50, 75])

      const { result } = renderHook(() => useMilestoneTracking('goal-5'))

      expect(result.current.seenMilestones.size).toBe(3)

      act(() => {
        result.current.clearSeenMilestones()
      })

      expect(result.current.seenMilestones.size).toBe(0)
      expect(mockRemoveItem).toHaveBeenCalledWith('milestone_tracking_goal-5')
    })
  })

  describe('Edge Cases', () => {
    it('handles corrupted localStorage data', () => {
      localStorageMock['milestone_tracking_goal-6'] = 'not-valid-json'

      const { result } = renderHook(() => useMilestoneTracking('goal-6'))

      expect(result.current.seenMilestones.size).toBe(0)
    })

    it('handles empty milestones array', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      const newMilestones = result.current.getNewlyReachedMilestones(50, [])

      expect(newMilestones).toEqual([])
    })

    it('handles progress of 0', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      const newMilestones = result.current.getNewlyReachedMilestones(0, [25, 50, 75, 100])

      expect(newMilestones).toEqual([])
    })

    it('handles negative progress', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      const newMilestones = result.current.getNewlyReachedMilestones(-10, [25, 50, 75, 100])

      expect(newMilestones).toEqual([])
    })

    it('handles progress > 100', () => {
      const { result } = renderHook(() => useMilestoneTracking('goal-1'))

      const newMilestones = result.current.getNewlyReachedMilestones(150, [25, 50, 75, 100])

      expect(newMilestones).toEqual([25, 50, 75, 100])
    })

    it('isolates tracking per goal ID', () => {
      const { result: result1 } = renderHook(() => useMilestoneTracking('goal-a'))
      const { result: result2 } = renderHook(() => useMilestoneTracking('goal-b'))

      act(() => {
        result1.current.markMilestoneSeen(25)
      })

      expect(result1.current.hasSeenMilestone(25)).toBe(true)
      expect(result2.current.hasSeenMilestone(25)).toBe(false)
    })
  })
})
