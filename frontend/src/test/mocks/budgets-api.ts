// Issue #575: Centralized budgets API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests
// All test files that need to mock @/api/generated/endpoints/budgets/budgets should use these exports
//
// IMPORTANT: This re-exports ALL real exports and only overrides the hooks we need to mock.
// This ensures actual implementation files still work correctly.

import { mock } from 'bun:test'

// Import ALL exports from the real module
import * as RealBudgetsModule from '@/api/generated/endpoints/budgets/budgets'

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

// Single mock.module() call - called once at preload time
// Re-export ALL real exports, only override the specific hooks we need to mock
mock.module('@/api/generated/endpoints/budgets/budgets', () => ({
  // Re-export ALL real exports (functions, constants, types will be re-exported)
  ...RealBudgetsModule,

  // Override only the specific hooks used by tests
  useGetCurrentBudget: (...args: unknown[]) => mockUseGetCurrentBudget(...args),
  getGetCurrentBudgetQueryKey: () => mockGetCurrentBudgetQueryKey(),

  useGetCategoryTransactions: (...args: unknown[]) =>
    mockUseGetCategoryTransactions(...args),
  getGetCategoryTransactionsQueryKey: (...args: unknown[]) =>
    mockGetCategoryTransactionsQueryKey(
      ...(args as [string, string, unknown])
    ),
  getCategoryTransactions: (...args: unknown[]) =>
    mockGetCategoryTransactions(...args),

  useGetAvailableCategories: (...args: unknown[]) =>
    mockUseGetAvailableCategories(...args),
  getGetAvailableCategoriesQueryKey: () => mockGetAvailableCategoriesQueryKey(),

  // useBudgets hook mocks
  useGetBudgets: (...args: unknown[]) => mockUseGetBudgets(...args),
  getGetBudgetsQueryKey: () => mockGetBudgetsQueryKey(),
  useCreateBudget: (...args: unknown[]) => mockUseCreateBudget(...args),
  useUpdateBudget: (...args: unknown[]) => mockUseUpdateBudget(...args),
  useDeleteBudget: (...args: unknown[]) => mockUseDeleteBudget(...args),
  useAddBudgetCategory: (...args: unknown[]) => mockUseAddBudgetCategory(...args),
  useUpdateBudgetCategory: (...args: unknown[]) => mockUseUpdateBudgetCategory(...args),
  useDeleteBudgetCategory: (...args: unknown[]) => mockUseDeleteBudgetCategory(...args),
  getAvailableCategories: (...args: unknown[]) => mockGetAvailableCategoriesFn(...args),

  // useBudgetDetail hook mocks
  useGetBudget: (...args: unknown[]) => mockUseGetBudget(...args),
  getGetBudgetQueryKey: (...args: unknown[]) => mockGetBudgetQueryKey(...(args as [string])),
}))
