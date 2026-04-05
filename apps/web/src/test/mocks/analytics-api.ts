// Issue #575: Centralized analytics API mock for hook implementation tests
// This file mocks at the API level so wrapper hooks can be tested with real implementation
// Used by: useCashFlowData.test.tsx, useSpendingByCategory.test.ts, useSpendingByMerchant.test.ts, useSpendingTrends.test.ts

import { mock } from 'bun:test'
import * as RealAnalyticsModule from '@boostedbudget/api-client/endpoints/analytics/analytics'

// Shared mock refetch factory
const createMockRefetch = () => mock(() => Promise.resolve({}))

// === useGetCashFlowSummary ===
export const mockRefetchCashFlowSummary = createMockRefetch()
export const mockUseGetCashFlowSummary = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchCashFlowSummary,
}))

// === useGetCashFlowHistory ===
export const mockRefetchCashFlowHistory = createMockRefetch()
export const mockUseGetCashFlowHistory = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchCashFlowHistory,
}))

// Query key mocks
export const mockGetCashFlowSummaryQueryKey = mock((params: unknown) => [
  'cash-flow-summary',
  params,
])
export const mockGetCashFlowHistoryQueryKey = mock((params: unknown) => [
  'cash-flow-history',
  params,
])

// === useGetSpendingByCategory ===
export const mockUseGetSpendingByCategory = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
  refetch: createMockRefetch(),
}))
export const mockGetSpendingByCategoryQueryKey = mock((params: unknown) => [
  'spending-by-category',
  params,
])

// === useGetSpendingByMerchant ===
export const mockUseGetSpendingByMerchant = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
  refetch: createMockRefetch(),
}))

// === useGetSpendingTrends ===
export const mockUseGetSpendingTrends = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
  refetch: createMockRefetch(),
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/analytics/analytics', () => ({
  ...RealAnalyticsModule,
  useGetCashFlowSummary: mockUseGetCashFlowSummary,
  useGetCashFlowHistory: mockUseGetCashFlowHistory,
  getGetCashFlowSummaryQueryKey: mockGetCashFlowSummaryQueryKey,
  getGetCashFlowHistoryQueryKey: mockGetCashFlowHistoryQueryKey,
  useGetSpendingByCategory: mockUseGetSpendingByCategory,
  getGetSpendingByCategoryQueryKey: mockGetSpendingByCategoryQueryKey,
  useGetSpendingByMerchant: mockUseGetSpendingByMerchant,
  useGetSpendingTrends: mockUseGetSpendingTrends,
}))
