/**
 * useGettingStarted Hook Tests
 * Issue #848: Onboarding - Add first-time user experience and guided setup
 *
 * Tests the getting-started checklist hook with localStorage-based activation/dismissal
 * and step completion derived from live API data.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useGettingStarted } from './useGettingStarted'
import { mockUseDashboardContext } from '@/test/mocks/dashboard-context'
import { mockUseGetBudgets } from '@/test/mocks/budgets-api'
import { mockUseGetSavingsGoals } from '@/test/mocks/savings-goals-api'
import { mockUseGetAutomationRules } from '@/test/mocks/automation-rules-api'
import { mockUseGetUserRules } from '@/test/mocks/auto-categorization-api'

const ACTIVATED_KEY = 'getting-started-activated'
const DISMISSED_KEY = 'getting-started-dismissed'

/** Helper: configure all mocks to represent a brand-new user with no data */
function setupNewUser() {
  mockUseDashboardContext.mockReturnValue({
    filters: { startDate: '2025-01-01', endDate: '2025-01-31' },
    setFilters: () => {},
    timeframe: 'month' as const,
    setTimeframe: () => {},
    isEditing: false,
    setIsEditing: () => {},
    hasAccounts: false,
    accountsLoading: false,
  })
  mockUseGetBudgets.mockReturnValue({ data: undefined, isLoading: false, error: null })
  mockUseGetSavingsGoals.mockReturnValue({ data: undefined, isLoading: false, error: null })
  mockUseGetAutomationRules.mockReturnValue({ data: [], isLoading: false, error: null })
  mockUseGetUserRules.mockReturnValue({ data: undefined, isLoading: false, error: null })
}

/** Helper: configure mocks to represent a fully set-up user */
function setupFullyOnboardedUser() {
  mockUseDashboardContext.mockReturnValue({
    filters: { startDate: '2025-01-01', endDate: '2025-01-31' },
    setFilters: () => {},
    timeframe: 'month' as const,
    setTimeframe: () => {},
    isEditing: false,
    setIsEditing: () => {},
    hasAccounts: true,
    accountsLoading: false,
  })
  mockUseGetBudgets.mockReturnValue({ data: [{ id: 'b1', name: 'Monthly' }], isLoading: false, error: null })
  mockUseGetSavingsGoals.mockReturnValue({ data: [{ id: 'g1', name: 'Emergency' }], isLoading: false, error: null })
  mockUseGetAutomationRules.mockReturnValue({ data: [{ id: 'r1', name: 'Budget Alert' }], isLoading: false, error: null })
  mockUseGetUserRules.mockReturnValue({ data: [{ id: 'c1' }], isLoading: false, error: null })
}

/** Helper: configure mocks to represent an existing user with accounts but no budget/rules */
function setupExistingUserWithAccounts() {
  mockUseDashboardContext.mockReturnValue({
    filters: { startDate: '2025-01-01', endDate: '2025-01-31' },
    setFilters: () => {},
    timeframe: 'month' as const,
    setTimeframe: () => {},
    isEditing: false,
    setIsEditing: () => {},
    hasAccounts: true,
    accountsLoading: false,
  })
  mockUseGetBudgets.mockReturnValue({ data: undefined, isLoading: false, error: null })
  mockUseGetSavingsGoals.mockReturnValue({ data: undefined, isLoading: false, error: null })
  mockUseGetAutomationRules.mockReturnValue({ data: [], isLoading: false, error: null })
  mockUseGetUserRules.mockReturnValue({ data: undefined, isLoading: false, error: null })
}

describe('useGettingStarted', () => {
  beforeEach(() => {
    localStorage.clear()
    setupNewUser()
  })

  afterEach(() => {
    localStorage.clear()
  })

  describe('Initial state — new user (0 accounts)', () => {
    it('returns shouldShow: true when no accounts and not dismissed', () => {
      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.shouldShow).toBe(true)
    })

    it('sets getting-started-activated in localStorage on first render', () => {
      renderHook(() => useGettingStarted())
      expect(localStorage.getItem(ACTIVATED_KEY)).toBe('true')
    })

    it('returns 5 steps', () => {
      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.steps).toHaveLength(5)
      expect(result.current.totalSteps).toBe(5)
    })

    it('returns all steps as incomplete for a new user', () => {
      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.steps.every(s => !s.complete)).toBe(true)
      expect(result.current.completedCount).toBe(0)
    })

    it('returns correct step ids and hrefs', () => {
      const { result } = renderHook(() => useGettingStarted())
      const steps = result.current.steps
      expect(steps[0].id).toBe('connect-bank')
      expect(steps[0].href).toBe('/accounts')
      expect(steps[1].id).toBe('set-up-budget')
      expect(steps[1].href).toBe('/budgets')
      expect(steps[2].id).toBe('create-savings-goal')
      expect(steps[2].href).toBe('/budgets')
      expect(steps[3].id).toBe('create-automation-rule')
      expect(steps[3].href).toBe('/rules')
      expect(steps[4].id).toBe('set-up-auto-categorization')
      expect(steps[4].href).toBe('/auto-categorization')
    })
  })

  describe('Step completion — derived from API data', () => {
    it('marks connect-bank complete when hasAccounts is true', () => {
      mockUseDashboardContext.mockReturnValue({
        filters: { startDate: '2025-01-01', endDate: '2025-01-31' },
        setFilters: () => {},
        timeframe: 'month' as const,
        setTimeframe: () => {},
        isEditing: false,
        setIsEditing: () => {},
        hasAccounts: true,
        accountsLoading: false,
      })
      localStorage.setItem(ACTIVATED_KEY, 'true')

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.steps[0].complete).toBe(true)
    })

    it('marks set-up-budget complete when budgets data is non-empty', () => {
      mockUseGetBudgets.mockReturnValue({ data: [{ id: 'b1' }], isLoading: false, error: null })

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.steps[1].complete).toBe(true)
    })

    it('marks create-savings-goal complete when savings goals data is non-empty', () => {
      mockUseGetSavingsGoals.mockReturnValue({ data: [{ id: 'g1' }], isLoading: false, error: null })

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.steps[2].complete).toBe(true)
    })

    it('marks create-automation-rule complete when automation rules data is non-empty', () => {
      mockUseGetAutomationRules.mockReturnValue({ data: [{ id: 'r1' }], isLoading: false, error: null })

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.steps[3].complete).toBe(true)
    })

    it('marks set-up-auto-categorization complete when categorization rules data is non-empty', () => {
      mockUseGetUserRules.mockReturnValue({ data: [{ id: 'c1' }], isLoading: false, error: null })

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.steps[4].complete).toBe(true)
    })

    it('correctly counts partial completion', () => {
      mockUseDashboardContext.mockReturnValue({
        filters: { startDate: '2025-01-01', endDate: '2025-01-31' },
        setFilters: () => {},
        timeframe: 'month' as const,
        setTimeframe: () => {},
        isEditing: false,
        setIsEditing: () => {},
        hasAccounts: true,
        accountsLoading: false,
      })
      mockUseGetBudgets.mockReturnValue({ data: [{ id: 'b1' }], isLoading: false, error: null })
      localStorage.setItem(ACTIVATED_KEY, 'true')

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.completedCount).toBe(2)
      expect(result.current.allComplete).toBe(false)
    })
  })

  describe('Loading state', () => {
    it('returns isLoading: true while accounts are loading', () => {
      mockUseDashboardContext.mockReturnValue({
        filters: { startDate: '2025-01-01', endDate: '2025-01-31' },
        setFilters: () => {},
        timeframe: 'month' as const,
        setTimeframe: () => {},
        isEditing: false,
        setIsEditing: () => {},
        hasAccounts: false,
        accountsLoading: true,
      })

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.isLoading).toBe(true)
    })

    it('returns isLoading: true while any query is loading for an activated user', () => {
      localStorage.setItem(ACTIVATED_KEY, 'true')
      mockUseGetBudgets.mockReturnValue({ data: undefined, isLoading: true, error: null })

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.isLoading).toBe(true)
    })

    it('returns isLoading: false when all queries are settled', () => {
      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.isLoading).toBe(false)
    })
  })

  describe('Dismissal', () => {
    it('returns shouldShow: false after dismiss() is called', () => {
      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.shouldShow).toBe(true)

      act(() => {
        result.current.dismiss()
      })

      expect(result.current.shouldShow).toBe(false)
    })

    it('persists dismissal to localStorage', () => {
      const { result } = renderHook(() => useGettingStarted())
      act(() => {
        result.current.dismiss()
      })
      expect(localStorage.getItem(DISMISSED_KEY)).toBe('true')
    })

    it('returns shouldShow: false if already dismissed in localStorage', () => {
      localStorage.setItem(DISMISSED_KEY, 'true')

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.shouldShow).toBe(false)
    })

    it('stays dismissed after re-render', () => {
      const { result, rerender } = renderHook(() => useGettingStarted())
      act(() => {
        result.current.dismiss()
      })
      rerender()
      expect(result.current.shouldShow).toBe(false)
    })
  })

  describe('Existing user protection', () => {
    it('returns shouldShow: false for existing user with accounts and no activation flag', () => {
      setupExistingUserWithAccounts()
      // No ACTIVATED_KEY in localStorage

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.shouldShow).toBe(false)
    })

    it('does NOT set getting-started-activated for existing user with accounts', () => {
      setupExistingUserWithAccounts()

      renderHook(() => useGettingStarted())
      expect(localStorage.getItem(ACTIVATED_KEY)).toBeNull()
    })
  })

  describe('Persistence across instances', () => {
    it('retains activation state for returning new user', () => {
      // First mount: activate
      const { unmount } = renderHook(() => useGettingStarted())
      expect(localStorage.getItem(ACTIVATED_KEY)).toBe('true')
      unmount()

      // Now simulate user connected bank (hasAccounts = true)
      mockUseDashboardContext.mockReturnValue({
        filters: { startDate: '2025-01-01', endDate: '2025-01-31' },
        setFilters: () => {},
        timeframe: 'month' as const,
        setTimeframe: () => {},
        isEditing: false,
        setIsEditing: () => {},
        hasAccounts: true,
        accountsLoading: false,
      })

      // Second mount: should still show (activated flag persists)
      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.shouldShow).toBe(true)
    })
  })

  describe('All-complete state', () => {
    it('returns allComplete: true when all 5 steps are done', () => {
      setupFullyOnboardedUser()
      localStorage.setItem(ACTIVATED_KEY, 'true')

      const { result } = renderHook(() => useGettingStarted())
      expect(result.current.allComplete).toBe(true)
      expect(result.current.completedCount).toBe(5)
    })
  })
})
