// Issue #575: Centralized dashboard API mock for hook implementation tests
// This file mocks at the API level so wrapper hooks can be tested with real implementation
// Used by: useDashboardData.test.tsx, useDashboardLayout.test.tsx

import { mock } from 'bun:test'
import * as RealDashboardModule from '@boostedbudget/api-client/endpoints/dashboard/dashboard'

// === useGetBalanceSummary (for useDashboardData) ===
export const mockRefetchBalanceSummary = mock(() => Promise.resolve({}))
export const mockUseGetBalanceSummary = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchBalanceSummary,
}))

// === useGetBalanceHistory (for useDashboardData) ===
export const mockRefetchBalanceHistory = mock(() => Promise.resolve({}))
export const mockUseGetBalanceHistory = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchBalanceHistory,
}))

// === useGetNetWorth (for useNetWorthData) ===
// Issue #1370: added so useNetWorthData.test.tsx can exercise the real hook
// implementation (previously only wrapper-level mocked via net-worth-hooks.ts).
export const mockRefetchNetWorth = mock(() => Promise.resolve({}))
export const mockUseGetNetWorth = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchNetWorth,
}))

// === useGetNetWorthHistory (for useNetWorthData) ===
export const mockRefetchNetWorthHistory = mock(() => Promise.resolve({}))
export const mockUseGetNetWorthHistory = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchNetWorthHistory,
}))

// === useGetAccountTypes (for useDashboardData) ===
export const mockRefetchAccountTypes = mock(() => Promise.resolve({}))
export const mockUseGetAccountTypes = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchAccountTypes,
}))

// === useGetDashboardPreferences (for useDashboardLayout) ===
export const mockRefetchDashboardPreferences = mock(() => Promise.resolve({}))
export const mockUseGetDashboardPreferences = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchDashboardPreferences,
}))

// === useSaveDashboardPreferences (for useDashboardLayout) ===
export const mockSaveDashboardPreferencesMutateAsync = mock(() => Promise.resolve({}))
export const mockUseSaveDashboardPreferences = mock(() => ({
  mutateAsync: mockSaveDashboardPreferencesMutateAsync,
  isPending: false,
}))

// === useResetDashboardPreferences (for useDashboardLayout) ===
export const mockResetDashboardPreferencesMutateAsync = mock(() => Promise.resolve({}))
export const mockUseResetDashboardPreferences = mock(() => ({
  mutateAsync: mockResetDashboardPreferencesMutateAsync,
  isPending: false,
}))

// === useGetDefaultLayout (for useDefaultDashboardLayout) ===
export const mockRefetchDefaultLayout = mock(() => Promise.resolve({}))
export const mockUseGetDefaultLayout = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchDefaultLayout,
  isSuccess: false,
}))

// Query key mocks
export const mockGetBalanceSummaryQueryKey = mock((params: unknown) => [
  'balance-summary',
  params,
])
export const mockGetBalanceHistoryQueryKey = mock((params: unknown) => [
  'balance-history',
  params,
])
export const mockGetAccountTypesQueryKey = mock(() => ['account-types'])
export const mockGetDefaultLayoutQueryKey = mock(() => ['dashboard', 'default-layout'])
// NOTE: getGetNetWorthQueryKey/getGetNetWorthHistoryQueryKey are deliberately NOT
// overridden here (unlike the balance-summary/history key getters above) — they're
// left to fall through via the `...RealDashboardModule` spread below. Several tests
// (e.g. useHouseholdVisibility.test.tsx's real-QueryClient prefix-match invalidation
// test) rely on at least one of the 7 scoped query-key getters resolving to the REAL
// generated implementation (which conditionally omits the `params` key segment when
// called with no arguments) rather than a simplified test double that always appends
// a `params` element — see that test's inline comment for the full explanation.

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/dashboard/dashboard', () => ({
  ...RealDashboardModule,
  useGetBalanceSummary: mockUseGetBalanceSummary,
  useGetBalanceHistory: mockUseGetBalanceHistory,
  useGetAccountTypes: mockUseGetAccountTypes,
  useGetDashboardPreferences: mockUseGetDashboardPreferences,
  useSaveDashboardPreferences: mockUseSaveDashboardPreferences,
  useResetDashboardPreferences: mockUseResetDashboardPreferences,
  useGetDefaultLayout: mockUseGetDefaultLayout,
  useGetNetWorth: mockUseGetNetWorth,
  useGetNetWorthHistory: mockUseGetNetWorthHistory,
  getGetBalanceSummaryQueryKey: mockGetBalanceSummaryQueryKey,
  getGetBalanceHistoryQueryKey: mockGetBalanceHistoryQueryKey,
  getGetAccountTypesQueryKey: mockGetAccountTypesQueryKey,
  getGetDefaultLayoutQueryKey: mockGetDefaultLayoutQueryKey,
}))
