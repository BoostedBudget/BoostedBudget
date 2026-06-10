// Issue #1283: Centralized boosts API mock (renamed from automation-rules-api)
// to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import type { Boost } from '@boostedbudget/api-client/models'
import * as RealBoostsModule from '@boostedbudget/api-client/endpoints/boosts/boosts'

// === useGetBoosts ===
export const mockRefetchBoosts = mock(() => Promise.resolve())
export const mockUseGetBoosts = mock(() => ({
  data: [] as Boost[],
  isLoading: false,
  error: null,
  refetch: mockRefetchBoosts,
}))

// === useCreateBoost ===
export const mockCreateBoostMutateAsync = mock(() => Promise.resolve())
export const mockUseCreateBoost = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockCreateBoostMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useUpdateBoost ===
export const mockUpdateBoostMutateAsync = mock(() => Promise.resolve())
export const mockUseUpdateBoost = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockUpdateBoostMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useDeleteBoost ===
export const mockDeleteBoostMutate = mock(() => {})
export const mockDeleteBoostMutateAsync = mock(() => Promise.resolve())
export const mockUseDeleteBoost = mock(() => ({
  mutate: mockDeleteBoostMutate,
  mutateAsync: mockDeleteBoostMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useToggleBoost ===
export const mockToggleBoostMutate = mock(() => {})
export const mockToggleBoostMutateAsync = mock(() => Promise.resolve())
export const mockUseToggleBoost = mock(() => ({
  mutate: mockToggleBoostMutate,
  mutateAsync: mockToggleBoostMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useTestBoost ===
export const mockTestBoostMutate = mock(() => {})
export const mockTestBoostMutateAsync = mock(() => Promise.resolve())
export const mockUseTestBoost = mock(() => ({
  mutate: mockTestBoostMutate,
  mutateAsync: mockTestBoostMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useTriggerBoost ===
export const mockTriggerBoostMutateAsync = mock(() => Promise.resolve())
export const mockUseTriggerBoost = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockTriggerBoostMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useGetBoostByID ===
export const mockUseGetBoostByID = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve()),
}))

// === useGetBoostExecutions ===
export const mockUseGetBoostExecutions = mock(() => ({
  data: { items: [], total: 0, limit: 5, offset: 0 },
  isLoading: false,
  error: null,
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/boosts/boosts', () => ({
  ...RealBoostsModule,
  useGetBoosts: mockUseGetBoosts,
  useGetBoostByID: mockUseGetBoostByID,
  useGetBoostExecutions: mockUseGetBoostExecutions,
  useCreateBoost: mockUseCreateBoost,
  useUpdateBoost: mockUseUpdateBoost,
  useDeleteBoost: mockUseDeleteBoost,
  useToggleBoost: mockUseToggleBoost,
  useTestBoost: mockUseTestBoost,
  useTriggerBoost: mockUseTriggerBoost,
}))
