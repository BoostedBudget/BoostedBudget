// Issue #575: Centralized categories API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealCategoriesModule from '@/api/generated/endpoints/categories/categories'

// === useCreateCategory ===
export const mockCreateCategoryMutateAsync = mock(() => Promise.resolve())
export const mockUseCreateCategory = mock(() => ({
  mutateAsync: mockCreateCategoryMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useGetCategories ===
export const mockRefetchCategories = mock(() => Promise.resolve())
export const mockUseGetCategories = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchCategories,
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/categories/categories', () => ({
  ...RealCategoriesModule,
  useCreateCategory: (...args: unknown[]) => mockUseCreateCategory(...args),
  useGetCategories: (...args: unknown[]) => mockUseGetCategories(...args),
}))
