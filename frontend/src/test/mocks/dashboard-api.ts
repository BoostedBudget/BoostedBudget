// Issue #575: Centralized dashboard API mock for hook implementation tests
// This file mocks at the API level so wrapper hooks can be tested with real implementation
// Used by: useDashboardData.test.tsx, useDashboardLayout.test.tsx

import { mock } from 'bun:test'
import * as RealDashboardModule from '@/api/generated/endpoints/dashboard/dashboard'

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

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/dashboard/dashboard', () => ({
  ...RealDashboardModule,
  useGetBalanceSummary: mockUseGetBalanceSummary,
  useGetBalanceHistory: mockUseGetBalanceHistory,
  useGetAccountTypes: mockUseGetAccountTypes,
  useGetDashboardPreferences: mockUseGetDashboardPreferences,
  useSaveDashboardPreferences: mockUseSaveDashboardPreferences,
  useResetDashboardPreferences: mockUseResetDashboardPreferences,
  useGetDefaultLayout: mockUseGetDefaultLayout,
  getGetBalanceSummaryQueryKey: mockGetBalanceSummaryQueryKey,
  getGetBalanceHistoryQueryKey: mockGetBalanceHistoryQueryKey,
  getGetAccountTypesQueryKey: mockGetAccountTypesQueryKey,
  getGetDefaultLayoutQueryKey: mockGetDefaultLayoutQueryKey,
}))
