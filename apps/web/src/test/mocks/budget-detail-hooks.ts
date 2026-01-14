// Issue #575: Centralized budget detail hook mocks
// These mock the WRAPPER hooks (useBudgetDetail, useAvailableCategories)
// for component tests that need to test in isolation from the actual hook implementation.
//
// NOTE: Hook implementation tests use API-level mocks from budgets-api.ts instead.

import { mock } from 'bun:test'

// === useBudgetDetail mock ===
export const mockBudgetDetailRefetch = mock(() => Promise.resolve())
export const mockUseBudgetDetail = mock(() => ({
  budget: null,
  loading: false,
  error: null,
  refetch: mockBudgetDetailRefetch,
}))

// Mock both absolute and relative paths
mock.module('@/features/budgets/hooks/useBudgetDetail', () => ({
  useBudgetDetail: (...args: unknown[]) => mockUseBudgetDetail(...args),
}))

mock.module('../hooks/useBudgetDetail', () => ({
  useBudgetDetail: (...args: unknown[]) => mockUseBudgetDetail(...args),
}))

// === useAvailableCategories mock ===
export const mockUseAvailableCategories = mock(() => ({
  categories: [],
}))

mock.module('@/features/budgets/hooks/useAvailableCategories', () => ({
  useAvailableCategories: () => mockUseAvailableCategories(),
}))

mock.module('../hooks/useAvailableCategories', () => ({
  useAvailableCategories: () => mockUseAvailableCategories(),
}))
