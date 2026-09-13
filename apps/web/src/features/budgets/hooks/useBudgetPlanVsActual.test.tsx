import { describe, expect, it } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { mockUseGetBudgetPlanVsActual } from '@/test/mocks/budgets-api'
import { useBudgetPlanVsActual } from './useBudgetPlanVsActual'

describe('useBudgetPlanVsActual', () => {
  it('requests fresh activity for the selected budget and refreshes on focus', () => {
    renderHook(() => useBudgetPlanVsActual('budget-1'))
    expect(mockUseGetBudgetPlanVsActual).toHaveBeenLastCalledWith('budget-1', { query: { enabled: true, staleTime: 0, refetchOnWindowFocus: true, refetchInterval: 30_000, refetchIntervalInBackground: false } })
  })
  it('does not request an empty budget id', () => {
    renderHook(() => useBudgetPlanVsActual(''))
    expect(mockUseGetBudgetPlanVsActual).toHaveBeenLastCalledWith('', { query: { enabled: false, staleTime: 0, refetchOnWindowFocus: true, refetchInterval: 30_000, refetchIntervalInBackground: false } })
  })
})
