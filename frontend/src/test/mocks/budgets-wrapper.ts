// Issue #575: Centralized budgets wrapper hook mocks
// This file is preloaded via bunfig.toml before all tests
// Mocks wrapper hooks (useBudgets, useSavingsGoals, useBudgetFilters)
// for component tests that need to test in isolation from hook implementation.

import { mock } from 'bun:test'
import type { Budget, SavingsGoal } from '@/features/budgets/types'

// === useBudgets wrapper hook mock ===
export const mockRefetchBudgets = mock(() => Promise.resolve())
export const mockCreateBudget = mock(() => Promise.resolve())
export const mockUpdateBudget = mock(() => Promise.resolve())
export const mockDeleteBudget = mock(() => Promise.resolve())
export const mockGetAvailableCategories = mock(() => Promise.resolve([]))
export const mockAddCategory = mock(() => Promise.resolve())
export const mockUpdateCategory = mock(() => Promise.resolve())
export const mockDeleteCategory = mock(() => Promise.resolve())

export const mockUseBudgets = mock(() => ({
  budgets: [] as Budget[],
  loading: false,
  error: null as Error | null,
  refetch: mockRefetchBudgets,
  createBudget: mockCreateBudget,
  updateBudget: mockUpdateBudget,
  deleteBudget: mockDeleteBudget,
  getAvailableCategories: mockGetAvailableCategories,
  addCategory: mockAddCategory,
  updateCategory: mockUpdateCategory,
  deleteCategory: mockDeleteCategory,
}))

// REMOVED: Global mock.module() for useBudgets
// Tests that need this mock should use inline mock.module():
// import { mockUseBudgets } from '@/test/mocks/budgets-wrapper'
// mock.module('@/features/budgets/hooks/useBudgets', () => ({
//   useBudgets: () => mockUseBudgets(),
// }))

// === useSavingsGoals wrapper hook mock ===
// NOTE: These mocks are exported but NOT applied via mock.module() globally.
// This is because useSavingsGoals.test.tsx needs to test the REAL implementation.
// Component tests that need useSavingsGoals mocked should import from savings-goals-api.ts
// or use the centralized mockUseSavingsGoals from this file with inline mock.module().
export const mockRefetchGoals = mock(() => Promise.resolve())
export const mockCreateGoal = mock(() => Promise.resolve())
export const mockUpdateGoal = mock(() => Promise.resolve())
export const mockDeleteGoal = mock(() => Promise.resolve())

export const mockUseSavingsGoals = mock(() => ({
  goals: [] as SavingsGoal[],
  goalsWithProgress: [],
  loading: false,
  error: null as Error | null,
  totalTarget: 0,
  totalSaved: 0,
  refetch: mockRefetchGoals,
  createGoal: mockCreateGoal,
  updateGoal: mockUpdateGoal,
  deleteGoal: mockDeleteGoal,
}))

// REMOVED: Global mock.module() for useSavingsGoals
// Tests that need this mock should use:
// import { mockUseSavingsGoals } from '@/test/mocks/budgets-wrapper'
// mock.module('@/features/budgets/hooks/useSavingsGoals', () => ({
//   useSavingsGoals: () => mockUseSavingsGoals(),
// }))

// === useBudgetFilters wrapper hook mock ===
// NOTE: These mocks are exported but NOT applied via mock.module() globally.
// This is because useBudgetFilters.test.ts needs to test the REAL implementation
// with MemoryRouter for URL state synchronization.
// Component tests that need useBudgetFilters mocked should import from this file
// and apply mock.module() inline.
export const mockSetPeriodFilter = mock(() => {})
export const mockSetProgressRange = mock(() => {})
export const mockClearAllFilters = mock(() => {})

export const mockUseBudgetFilters = mock(() => ({
  filters: { period: [], progressMin: undefined, progressMax: undefined },
  periodFilter: [],
  progressRange: null,
  setPeriodFilter: mockSetPeriodFilter,
  setProgressRange: mockSetProgressRange,
  clearAllFilters: mockClearAllFilters,
  activeFilterCount: 0,
  hasActiveFilters: false,
}))

// REMOVED: Global mock.module() for useBudgetFilters
// Tests that need this mock should use inline mock.module():
// import { mockUseBudgetFilters } from '@/test/mocks/budgets-wrapper'
// mock.module('@/features/budgets/hooks/useBudgetFilters', () => ({
//   useBudgetFilters: () => mockUseBudgetFilters(),
// }))
