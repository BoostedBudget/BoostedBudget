// Issue #575: Centralized categories API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealCategoriesModule from '@boostedbudget/api-client/endpoints/categories/categories'

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

// === useGetUserCategories ===
export const mockRefetchUserCategories = mock(() => Promise.resolve())
export const mockUseGetUserCategories = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchUserCategories,
}))

// === useGetPlaidCategories ===
export const mockRefetchPlaidCategories = mock(() => Promise.resolve())
export const mockUseGetPlaidCategories = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
  refetch: mockRefetchPlaidCategories,
}))

// === useGetPlaidPrimaryCategories ===
export const mockUseGetPlaidPrimaryCategories = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
}))

// === useGetPlaidDetailedCategories ===
export const mockUseGetPlaidDetailedCategories = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
}))

// Query key mock
export const mockGetUserCategoriesQueryKey = mock(() => ['user-categories'])

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/categories/categories', () => ({
  ...RealCategoriesModule,
  useCreateCategory: mockUseCreateCategory,
  useGetCategories: mockUseGetCategories,
  useGetUserCategories: mockUseGetUserCategories,
  useGetPlaidCategories: mockUseGetPlaidCategories,
  useGetPlaidPrimaryCategories: mockUseGetPlaidPrimaryCategories,
  useGetPlaidDetailedCategories: mockUseGetPlaidDetailedCategories,
  getGetUserCategoriesQueryKey: mockGetUserCategoriesQueryKey,
}))
