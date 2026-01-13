/**
 * Centralized mock for savings-goals API hooks
 * Issue #575: Prevents mock.module() pollution across test files
 *
 * This mock provides consistent behavior for all savings-goals API hooks.
 * Tests can customize return values via mockReturnValue() in beforeEach.
 */

import { mock } from 'bun:test'
import * as RealSavingsGoalsModule from '@/api/generated/endpoints/savings-goals/savings-goals'

// Mutation mocks (export for test customization)
export const mockCreateSavingsGoalMutateAsync = mock(() => Promise.resolve({} as any))
export const mockUpdateSavingsGoalMutateAsync = mock(() => Promise.resolve({} as any))
export const mockDeleteSavingsGoalMutateAsync = mock(() => Promise.resolve(undefined))

// Refetch mocks
export const mockSavingsGoalsRefetch = mock(() => Promise.resolve({} as any))
export const mockActiveSavingsGoalsRefetch = mock(() => Promise.resolve({} as any))

// Query hooks
export const mockUseGetSavingsGoals = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockSavingsGoalsRefetch,
}))

export const mockUseGetActiveSavingsGoals = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockActiveSavingsGoalsRefetch,
}))

export const mockUseGetSavingsGoal = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve({} as any)),
}))

// Mutation hooks
export const mockUseCreateSavingsGoal = mock(() => ({
  mutateAsync: mockCreateSavingsGoalMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

export const mockUseUpdateSavingsGoal = mock(() => ({
  mutateAsync: mockUpdateSavingsGoalMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

export const mockUseDeleteSavingsGoal = mock(() => ({
  mutateAsync: mockDeleteSavingsGoalMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// Query key getters
export const mockGetGetSavingsGoalsQueryKey = mock(() => ['savings-goals'] as const)
export const mockGetGetActiveSavingsGoalsQueryKey = mock(() => ['savings-goals', 'active'] as const)
export const mockGetGetSavingsGoalQueryKey = mock((id?: string) => ['savings-goals', id] as const)

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/savings-goals/savings-goals', () => ({
  ...RealSavingsGoalsModule,
  useGetSavingsGoals: (...args: unknown[]) => mockUseGetSavingsGoals(...args),
  useGetActiveSavingsGoals: (...args: unknown[]) => mockUseGetActiveSavingsGoals(...args),
  useGetSavingsGoal: (...args: unknown[]) => mockUseGetSavingsGoal(...args),
  useCreateSavingsGoal: (...args: unknown[]) => mockUseCreateSavingsGoal(...args),
  useUpdateSavingsGoal: (...args: unknown[]) => mockUseUpdateSavingsGoal(...args),
  useDeleteSavingsGoal: (...args: unknown[]) => mockUseDeleteSavingsGoal(...args),
  getGetSavingsGoalsQueryKey: (...args: unknown[]) => mockGetGetSavingsGoalsQueryKey(...args),
  getGetActiveSavingsGoalsQueryKey: (...args: unknown[]) => mockGetGetActiveSavingsGoalsQueryKey(...args),
  getGetSavingsGoalQueryKey: (...args: unknown[]) => mockGetGetSavingsGoalQueryKey(...args),
}))
