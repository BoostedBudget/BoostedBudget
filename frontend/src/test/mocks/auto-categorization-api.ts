// Issue #575: Centralized auto-categorization API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealAutoCategorizationModule from '@/api/generated/endpoints/auto-categorization/auto-categorization'

// === useGetUserRules ===
export const mockRefetchUserRules = mock(() => Promise.resolve())
export const mockUseGetUserRules = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchUserRules,
}))

// === useCreateRule ===
export const mockCreateRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseCreateRule = mock(() => ({
  mutateAsync: mockCreateRuleMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useDeleteRule ===
export const mockDeleteRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseDeleteRule = mock(() => ({
  mutateAsync: mockDeleteRuleMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useGetRuleByID ===
export const mockRefetchRuleByID = mock(() => Promise.resolve())
export const mockUseGetRuleByID = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchRuleByID,
}))

// === useUpdateRule ===
export const mockUpdateRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseUpdateRule = mock(() => ({
  mutateAsync: mockUpdateRuleMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useToggleRule ===
export const mockToggleRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseToggleRule = mock(() => ({
  mutateAsync: mockToggleRuleMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/auto-categorization/auto-categorization', () => ({
  ...RealAutoCategorizationModule,
  useGetUserRules: (...args: unknown[]) => mockUseGetUserRules(...args),
  useCreateRule: (...args: unknown[]) => mockUseCreateRule(...args),
  useDeleteRule: (...args: unknown[]) => mockUseDeleteRule(...args),
  useGetRuleByID: (...args: unknown[]) => mockUseGetRuleByID(...args),
  useUpdateRule: (...args: unknown[]) => mockUseUpdateRule(...args),
  useToggleRule: (...args: unknown[]) => mockUseToggleRule(...args),
}))
