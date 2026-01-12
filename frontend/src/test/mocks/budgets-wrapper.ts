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

// Mock both absolute and relative paths
mock.module('@/features/budgets/hooks/useBudgets', () => ({
  useBudgets: () => mockUseBudgets(),
}))

mock.module('../hooks/useBudgets', () => ({
  useBudgets: () => mockUseBudgets(),
}))

// === useSavingsGoals wrapper hook mock ===
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

mock.module('@/features/budgets/hooks/useSavingsGoals', () => ({
  useSavingsGoals: () => mockUseSavingsGoals(),
}))

mock.module('../hooks/useSavingsGoals', () => ({
  useSavingsGoals: () => mockUseSavingsGoals(),
}))

// === useBudgetFilters wrapper hook mock ===
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

mock.module('@/features/budgets/hooks/useBudgetFilters', () => ({
  useBudgetFilters: () => mockUseBudgetFilters(),
}))

mock.module('../hooks/useBudgetFilters', () => ({
  useBudgetFilters: () => mockUseBudgetFilters(),
}))
