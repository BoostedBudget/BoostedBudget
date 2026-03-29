// Issue #575: Centralized budgets API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests
// All test files that need to mock @boostedbudget/api-client/endpoints/budgets/budgets should use these exports
//
// IMPORTANT: This re-exports ALL real exports and only overrides the hooks we need to mock.
// This ensures actual implementation files still work correctly.

import { mock } from 'bun:test'

// Import ALL exports from the real module
import * as RealBudgetsModule from '@boostedbudget/api-client/endpoints/budgets/budgets'

// === useGetCurrentBudget (used by useCurrentBudget.test.tsx) ===
export const mockRefetchCurrentBudget = mock(() => Promise.resolve())
export const mockUseGetCurrentBudget = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchCurrentBudget,
}))
export const mockGetCurrentBudgetQueryKey = mock(() => ['budgets', 'current'])

// === useGetCategoryTransactions (used by useBudgetTransactions.test.tsx) ===
export const mockRefetchCategoryTransactions = mock(() =>
  Promise.resolve({ data: null })
)
export const mockGetCategoryTransactions = mock(() => Promise.resolve({}))
export const mockUseGetCategoryTransactions = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockRefetchCategoryTransactions,
}))
export const mockGetCategoryTransactionsQueryKey = mock(
  (budgetId: string, categoryId: string, params: unknown) => [
    'budgets',
    budgetId,
    'categories',
    categoryId,
    'transactions',
    params,
  ]
)

// === useGetAvailableCategories (used by useAvailableCategories.test.tsx) ===
export const mockRefetchAvailableCategories = mock(() => Promise.resolve())
export const mockUseGetAvailableCategories = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchAvailableCategories,
}))
export const mockGetAvailableCategoriesQueryKey = mock(() => [
  'budgets',
  'available-categories',
])

// === useGetBudgets (used by useBudgets.test.tsx) ===
export const mockRefetchBudgets = mock(() => Promise.resolve({}))
export const mockUseGetBudgets = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchBudgets,
}))
export const mockGetBudgetsQueryKey = mock(() => ['budgets'])

// === Mutation mocks for useBudgets.test.tsx ===
export const mockCreateBudgetMutateAsync = mock(() => Promise.resolve())
export const mockUseCreateBudget = mock(() => ({
  mutateAsync: mockCreateBudgetMutateAsync,
}))

export const mockUpdateBudgetMutateAsync = mock(() => Promise.resolve())
export const mockUseUpdateBudget = mock(() => ({
  mutateAsync: mockUpdateBudgetMutateAsync,
}))

export const mockDeleteBudgetMutateAsync = mock(() => Promise.resolve())
export const mockUseDeleteBudget = mock(() => ({
  mutateAsync: mockDeleteBudgetMutateAsync,
}))

export const mockAddBudgetCategoryMutateAsync = mock(() => Promise.resolve())
export const mockUseAddBudgetCategory = mock(() => ({
  mutateAsync: mockAddBudgetCategoryMutateAsync,
}))

export const mockUpdateBudgetCategoryMutateAsync = mock(() => Promise.resolve())
export const mockUseUpdateBudgetCategory = mock(() => ({
  mutateAsync: mockUpdateBudgetCategoryMutateAsync,
}))

export const mockDeleteBudgetCategoryMutateAsync = mock(() => Promise.resolve())
export const mockUseDeleteBudgetCategory = mock(() => ({
  mutateAsync: mockDeleteBudgetCategoryMutateAsync,
}))

// Non-hook function mock for getting available categories
export const mockGetAvailableCategoriesFn = mock(() => Promise.resolve([]))

// === useGetBudget (used by useBudgetDetail.test.tsx) ===
export const mockRefetchBudget = mock(() => Promise.resolve({}))
export const mockUseGetBudget = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchBudget,
}))
export const mockGetBudgetQueryKey = mock((id: string) => ['budgets', id])

// === useGetSavingsGoals ===
export const mockUseGetSavingsGoals = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve()),
}))

// Single mock.module() call - called once at preload time
// Re-export ALL real exports, only override the specific hooks we need to mock
mock.module('@boostedbudget/api-client/endpoints/budgets/budgets', () => ({
  ...RealBudgetsModule,
  useGetCurrentBudget: mockUseGetCurrentBudget,
  getGetCurrentBudgetQueryKey: mockGetCurrentBudgetQueryKey,
  useGetCategoryTransactions: mockUseGetCategoryTransactions,
  getGetCategoryTransactionsQueryKey: mockGetCategoryTransactionsQueryKey,
  getCategoryTransactions: mockGetCategoryTransactions,
  useGetAvailableCategories: mockUseGetAvailableCategories,
  getGetAvailableCategoriesQueryKey: mockGetAvailableCategoriesQueryKey,
  useGetBudgets: mockUseGetBudgets,
  getGetBudgetsQueryKey: mockGetBudgetsQueryKey,
  useCreateBudget: mockUseCreateBudget,
  useUpdateBudget: mockUseUpdateBudget,
  useDeleteBudget: mockUseDeleteBudget,
  useAddBudgetCategory: mockUseAddBudgetCategory,
  useUpdateBudgetCategory: mockUseUpdateBudgetCategory,
  useDeleteBudgetCategory: mockUseDeleteBudgetCategory,
  getAvailableCategories: mockGetAvailableCategoriesFn,
  useGetBudget: mockUseGetBudget,
  getGetBudgetQueryKey: mockGetBudgetQueryKey,
  useGetSavingsGoals: mockUseGetSavingsGoals,
}))
