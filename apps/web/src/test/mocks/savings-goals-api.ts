/**
 * Centralized mock for savings-goals API hooks
 * Issue #575: Prevents mock.module() pollution across test files
 *
 * This mock provides consistent behavior for all savings-goals API hooks.
 * Tests can customize return values via mockReturnValue() in beforeEach.
 */

import { mock } from 'bun:test'
import * as RealSavingsGoalsModule from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'

// Mutation mocks (export for test customization)
export const mockCreateSavingsGoalMutateAsync = mock(() => Promise.resolve(null as any))
export const mockUpdateSavingsGoalMutateAsync = mock(() => Promise.resolve(null as any))
export const mockDeleteSavingsGoalMutateAsync = mock(() => Promise.resolve(undefined))

// Contribution mutation mocks
export const mockLogSavingsGoalContributionMutateAsync = mock(() => Promise.resolve(null as any))
export const mockConfirmSavingsGoalContributionMutateAsync = mock(() => Promise.resolve(null as any))
export const mockRejectSavingsGoalContributionMutateAsync = mock(() => Promise.resolve(null as any)) // R3-7
export const mockDeleteSavingsGoalContributionMutateAsync = mock(() => Promise.resolve(undefined))

// Refetch mocks
export const mockSavingsGoalsRefetch = mock(() => Promise.resolve(null as any))
export const mockActiveSavingsGoalsRefetch = mock(() => Promise.resolve(null as any))

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

// #1569 Task 12: deleted-goals listing + purge.
export const mockDeletedSavingsGoalsRefetch = mock(() => Promise.resolve(null as any))
export const mockUseGetDeletedSavingsGoals = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockDeletedSavingsGoalsRefetch,
}))
export const mockPurgeSavingsGoalMutateAsync = mock(() => Promise.resolve(undefined))
export const mockUsePurgeSavingsGoal = mock(() => ({
  mutateAsync: mockPurgeSavingsGoalMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))
export const mockGetGetDeletedSavingsGoalsQueryKey = mock(() => ['getDeletedSavingsGoals'])

// #1569 Task 13 (E14): basis-aware progress summary — the only source of the `freshness`
// flag that explains a frozen balance-basis amount.
export const mockUseGetSavingsProgressSummary = mock(() => ({
  data: undefined as any,
  isLoading: false,
  error: null,
}))

export const mockUseGetSavingsGoal = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve(null as any)),
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

// Contribution query hook
export const mockUseListSavingsGoalContributions = mock(() => ({
  data: undefined as any[] | undefined,
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve(null as any)),
}))

// Contribution mutation hooks
export const mockUseLogSavingsGoalContribution = mock(() => ({
  mutateAsync: mockLogSavingsGoalContributionMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

export const mockUseConfirmSavingsGoalContribution = mock(() => ({
  mutateAsync: mockConfirmSavingsGoalContributionMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

export const mockUseRejectSavingsGoalContribution = mock(() => ({  // R3-7
  mutateAsync: mockRejectSavingsGoalContributionMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

export const mockUseDeleteSavingsGoalContribution = mock(() => ({
  mutateAsync: mockDeleteSavingsGoalContributionMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/savings-goals/savings-goals', () => ({
  ...RealSavingsGoalsModule,
  useGetSavingsGoals: mockUseGetSavingsGoals,
  useGetActiveSavingsGoals: mockUseGetActiveSavingsGoals,
  useGetSavingsGoal: mockUseGetSavingsGoal,
  useGetSavingsProgressSummary: mockUseGetSavingsProgressSummary,
  useGetDeletedSavingsGoals: mockUseGetDeletedSavingsGoals,
  usePurgeSavingsGoal: mockUsePurgeSavingsGoal,
  getGetDeletedSavingsGoalsQueryKey: mockGetGetDeletedSavingsGoalsQueryKey,
  useCreateSavingsGoal: mockUseCreateSavingsGoal,
  useUpdateSavingsGoal: mockUseUpdateSavingsGoal,
  useDeleteSavingsGoal: mockUseDeleteSavingsGoal,
  getGetSavingsGoalsQueryKey: mockGetGetSavingsGoalsQueryKey,
  getGetActiveSavingsGoalsQueryKey: mockGetGetActiveSavingsGoalsQueryKey,
  getGetSavingsGoalQueryKey: mockGetGetSavingsGoalQueryKey,
  useListSavingsGoalContributions: mockUseListSavingsGoalContributions,
  useLogSavingsGoalContribution: mockUseLogSavingsGoalContribution,
  useConfirmSavingsGoalContribution: mockUseConfirmSavingsGoalContribution,
  useRejectSavingsGoalContribution: mockUseRejectSavingsGoalContribution, // R3-7
  useDeleteSavingsGoalContribution: mockUseDeleteSavingsGoalContribution,
}))
