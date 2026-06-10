/**
 * useBoostsSummary Hook Tests
 * Issue #1267: Boosts page — savings impact + active rules summary
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { mockUseGetSavingsImpactMe } from '@/test/mocks/savings-impact-api'
import { mockUseGetBoosts } from '@/test/mocks/boosts-api'
import { createQueryWrapper } from '@/test/utils/queryTestUtils'
import { useBoostsSummary } from './useBoostsSummary'

describe('useBoostsSummary', () => {
  beforeEach(() => {
    mockUseGetSavingsImpactMe.mockClear()
    mockUseGetBoosts.mockClear()
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: null, refetch: mock(() => {}) })
    mockUseGetBoosts.mockReturnValue({ data: [], isLoading: false, error: null, refetch: mock(() => {}) })
  })

  it('returns totalMoved from savings impact total_amount', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({
      data: { total_amount: 2500, month_amount: 300, prev_month_amount: 250, completed_count: 10, recent_items: null },
      isLoading: false,
      error: null,
    })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.totalMoved).toBe(2500)
  })

  it('computes momChange as month_amount minus prev_month_amount', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({
      data: { total_amount: 2000, month_amount: 400, prev_month_amount: 300, completed_count: 8, recent_items: null },
      isLoading: false,
      error: null,
    })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.momChange).toBe(100)
  })

  it('returns null momChange when prev_month_amount is zero (guard against divide-by-zero / misleading ∞)', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({
      data: { total_amount: 500, month_amount: 200, prev_month_amount: 0, completed_count: 2, recent_items: null },
      isLoading: false,
      error: null,
    })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.momChange).toBeNull()
  })

  it('returns null summary fields when savings impact data is null', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: null })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.totalMoved).toBeNull()
    expect(result.current.momChange).toBeNull()
    expect(result.current.completedCount).toBeNull()
  })

  it('returns completedCount from savings impact completed_count', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({
      data: { total_amount: 1000, month_amount: 100, prev_month_amount: 80, completed_count: 15, recent_items: null },
      isLoading: false,
      error: null,
    })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.completedCount).toBe(15)
  })

  it('counts only enabled transfer-generating rules for activeRulesCount (AC1: type ∈ {transfer_suggestion, savings_goal})', () => {
    mockUseGetBoosts.mockReturnValue({
      data: [
        { id: 'r1', enabled: true, name: 'Rule 1', boost_type: 'transfer_suggestion', priority: 1, created_at: '', updated_at: '' },
        { id: 'r2', enabled: true, name: 'Rule 2', boost_type: 'savings_goal', priority: 2, created_at: '', updated_at: '' },
        { id: 'r3', enabled: true, name: 'Rule 3', boost_type: 'budget_alert', priority: 3, created_at: '', updated_at: '' },
        { id: 'r4', enabled: true, name: 'Rule 4', boost_type: 'category_rollover', priority: 4, created_at: '', updated_at: '' },
        { id: 'r5', enabled: false, name: 'Rule 5', boost_type: 'transfer_suggestion', priority: 5, created_at: '', updated_at: '' },
      ],
      isLoading: false,
      error: null,
    })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    // Only r1 (transfer_suggestion, enabled) + r2 (savings_goal, enabled) count.
    // r3 (budget_alert), r4 (category_rollover), r5 (disabled transfer_suggestion) do not.
    expect(result.current.activeRulesCount).toBe(2)
  })

  it('returns activeRulesCount of 0 when rules list is empty', () => {
    mockUseGetBoosts.mockReturnValue({ data: [], isLoading: false, error: null })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.activeRulesCount).toBe(0)
  })

  it('returns activeRulesCount of 0 when rules data is null/undefined', () => {
    mockUseGetBoosts.mockReturnValue({ data: undefined, isLoading: false, error: null })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.activeRulesCount).toBe(0)
  })

  it('propagates isLoading as true when either query is loading', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: true, error: null })
    mockUseGetBoosts.mockReturnValue({ data: [], isLoading: false, error: null })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.isLoading).toBe(true)
  })

  it('propagates isLoading as false when both queries are settled', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: null, refetch: mock(() => {}) })
    mockUseGetBoosts.mockReturnValue({ data: [], isLoading: false, error: null, refetch: mock(() => {}) })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.isLoading).toBe(false)
  })

  // R7-2: error + refetch
  it('surfaces error from the savings impact query', () => {
    const err = new Error('impact failed')
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: err, refetch: mock(() => {}) })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.error).toBe(err)
  })

  it('surfaces error from the boosts query when savings impact is fine', () => {
    const err = new Error('rules failed')
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: null, refetch: mock(() => {}) })
    mockUseGetBoosts.mockReturnValue({ data: [], isLoading: false, error: err, refetch: mock(() => {}) })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.error).toBe(err)
  })

  it('returns null error when both queries succeed', () => {
    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    expect(result.current.error).toBeNull()
  })

  it('exposes impactQuery error when both queries fail simultaneously', () => {
    const impactErr = new Error('impact failed')
    const rulesErr = new Error('rules failed')
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: impactErr, refetch: mock(() => {}) })
    mockUseGetBoosts.mockReturnValue({ data: [], isLoading: false, error: rulesErr, refetch: mock(() => {}) })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })

    // impactQuery error takes precedence (?? short-circuits on first non-nullish)
    expect(result.current.error).toBe(impactErr)
  })

  it('provides a refetch that invokes both underlying queries', () => {
    const impactRefetch = mock(() => Promise.resolve({ data: null }))
    const rulesRefetch = mock(() => Promise.resolve({ data: [] }))
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: null, refetch: impactRefetch })
    mockUseGetBoosts.mockReturnValue({ data: [], isLoading: false, error: null, refetch: rulesRefetch })

    const { result } = renderHook(() => useBoostsSummary(), { wrapper: createQueryWrapper() })
    result.current.refetch()

    expect(impactRefetch).toHaveBeenCalledTimes(1)
    expect(rulesRefetch).toHaveBeenCalledTimes(1)
  })
})
