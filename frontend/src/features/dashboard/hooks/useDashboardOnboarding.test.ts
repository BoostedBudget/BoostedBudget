/**
 * useDashboardOnboarding Hook Tests
 *
 * Tests the first-visit detection hook for dashboard discoverability.
 * Issue #412: Dashboard widget UX improvements
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useDashboardOnboarding } from './useDashboardOnboarding'

describe('useDashboardOnboarding', () => {
  const STORAGE_KEY = 'dashboard-onboarding-seen'

  beforeEach(() => {
    // Clear localStorage before each test
    localStorage.clear()
    vi.clearAllMocks()
  })

  afterEach(() => {
    localStorage.clear()
  })

  describe('Initial State', () => {
    it('should show hint when localStorage key is not set', () => {
      const { result } = renderHook(() => useDashboardOnboarding())

      expect(result.current.showHint).toBe(true)
    })

    it('should not show hint when localStorage key is set', () => {
      localStorage.setItem(STORAGE_KEY, 'true')

      const { result } = renderHook(() => useDashboardOnboarding())

      expect(result.current.showHint).toBe(false)
    })
  })

  describe('dismissHint', () => {
    it('should set showHint to false when dismissed', () => {
      const { result } = renderHook(() => useDashboardOnboarding())

      expect(result.current.showHint).toBe(true)

      act(() => {
        result.current.dismissHint()
      })

      expect(result.current.showHint).toBe(false)
    })

    it('should persist dismissal to localStorage', () => {
      const { result } = renderHook(() => useDashboardOnboarding())

      act(() => {
        result.current.dismissHint()
      })

      expect(localStorage.getItem(STORAGE_KEY)).toBe('true')
    })

    it('should remain dismissed after re-render', () => {
      const { result, rerender } = renderHook(() => useDashboardOnboarding())

      act(() => {
        result.current.dismissHint()
      })

      rerender()

      expect(result.current.showHint).toBe(false)
    })
  })

  describe('Persistence', () => {
    it('should remember dismissal across hook instances', () => {
      // First instance - dismiss the hint
      const { result: result1, unmount } = renderHook(() =>
        useDashboardOnboarding()
      )

      act(() => {
        result1.current.dismissHint()
      })

      unmount()

      // New instance should not show hint
      const { result: result2 } = renderHook(() => useDashboardOnboarding())

      expect(result2.current.showHint).toBe(false)
    })
  })
})
