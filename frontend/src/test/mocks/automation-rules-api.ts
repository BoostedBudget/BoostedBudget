// Issue #575: Centralized automation-rules API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import type { AutomationRule } from '@/api/generated/models'
import * as RealAutomationRulesModule from '@/api/generated/endpoints/automation-rules/automation-rules'

// === useGetAutomationRules ===
export const mockRefetchAutomationRules = mock(() => Promise.resolve())
export const mockUseGetAutomationRules = mock(() => ({
  data: [] as AutomationRule[],
  isLoading: false,
  error: null,
  refetch: mockRefetchAutomationRules,
}))

// === useCreateAutomationRule ===
export const mockCreateAutomationRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseCreateAutomationRule = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockCreateAutomationRuleMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useUpdateAutomationRule ===
export const mockUpdateAutomationRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseUpdateAutomationRule = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockUpdateAutomationRuleMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useDeleteAutomationRule ===
export const mockDeleteAutomationRuleMutate = mock(() => {})
export const mockDeleteAutomationRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseDeleteAutomationRule = mock(() => ({
  mutate: mockDeleteAutomationRuleMutate,
  mutateAsync: mockDeleteAutomationRuleMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useToggleAutomationRule ===
export const mockToggleAutomationRuleMutate = mock(() => {})
export const mockToggleAutomationRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseToggleAutomationRule = mock(() => ({
  mutate: mockToggleAutomationRuleMutate,
  mutateAsync: mockToggleAutomationRuleMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useTestAutomationRule ===
export const mockTestAutomationRuleMutate = mock(() => {})
export const mockTestAutomationRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseTestAutomationRule = mock(() => ({
  mutate: mockTestAutomationRuleMutate,
  mutateAsync: mockTestAutomationRuleMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useTriggerAutomationRule ===
export const mockTriggerAutomationRuleMutateAsync = mock(() => Promise.resolve())
export const mockUseTriggerAutomationRule = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockTriggerAutomationRuleMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === useGetAutomationRuleByID ===
export const mockUseGetAutomationRuleByID = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve()),
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/automation-rules/automation-rules', () => ({
  ...RealAutomationRulesModule,
  useGetAutomationRules: mockUseGetAutomationRules,
  useGetAutomationRuleByID: mockUseGetAutomationRuleByID,
  useCreateAutomationRule: mockUseCreateAutomationRule,
  useUpdateAutomationRule: mockUseUpdateAutomationRule,
  useDeleteAutomationRule: mockUseDeleteAutomationRule,
  useToggleAutomationRule: mockUseToggleAutomationRule,
  useTestAutomationRule: mockUseTestAutomationRule,
  useTriggerAutomationRule: mockUseTriggerAutomationRule,
}))
