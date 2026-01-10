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
}))
