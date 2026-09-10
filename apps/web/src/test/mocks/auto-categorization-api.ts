// Issue #575: Centralized auto-categorization API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealAutoCategorizationModule from '@boostedbudget/api-client/endpoints/auto-categorization/auto-categorization'

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

// === useGetUserRuleExecutions ===
// Issue #1569 T9.4: the account-wide automation audit trail. Default is an EMPTY page rather than
// this file's usual `isLoading: true`, mirroring mockUseGetUserBoostExecutions: the panel that
// consumes it renders nothing at all when total is 0, so every unrelated test that mounts the
// rules page keeps the DOM it had before this hook existed.
export const mockRefetchUserRuleExecutions = mock(() => Promise.resolve())
export const mockUseGetUserRuleExecutions = mock(() => ({
  data: { executions: [], total: 0, limit: 20, offset: 0 },
  isLoading: false,
  error: null,
  refetch: mockRefetchUserRuleExecutions,
}))

// === usePreviewRule (#1625) ===
// The rule dry-run. Defaults to a no-match result so a component that renders the panel without
// exercising it stays quiet; tests that care set mockPreviewRuleMutateAsync explicitly.
export const mockPreviewRuleMutateAsync = mock(() =>
  Promise.resolve({ scanned_count: 0, match_count: 0, matches: [] })
)
export const mockUsePreviewRule = mock(() => ({
  mutateAsync: mockPreviewRuleMutateAsync,
  isPending: false,
  error: null,
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/auto-categorization/auto-categorization', () => ({
  ...RealAutoCategorizationModule,
  useGetUserRules: mockUseGetUserRules,
  useGetUserRuleExecutions: mockUseGetUserRuleExecutions,
  useCreateRule: mockUseCreateRule,
  useDeleteRule: mockUseDeleteRule,
  useGetRuleByID: mockUseGetRuleByID,
  useUpdateRule: mockUseUpdateRule,
  useToggleRule: mockUseToggleRule,
  usePreviewRule: mockUsePreviewRule,
}))
