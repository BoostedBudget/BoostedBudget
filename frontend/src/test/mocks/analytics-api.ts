// Issue #575: Centralized analytics API mock for hook implementation tests
// This file mocks at the API level so wrapper hooks can be tested with real implementation
// Used by: useCashFlowData.test.tsx

import { mock } from 'bun:test'
import * as RealAnalyticsModule from '@/api/generated/endpoints/analytics/analytics'

// === useGetCashFlowSummary ===
export const mockRefetchCashFlowSummary = mock(() => Promise.resolve({}))
export const mockUseGetCashFlowSummary = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchCashFlowSummary,
}))

// === useGetCashFlowHistory ===
export const mockRefetchCashFlowHistory = mock(() => Promise.resolve({}))
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

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/analytics/analytics', () => ({
  ...RealAnalyticsModule,
  useGetCashFlowSummary: (...args: unknown[]) => mockUseGetCashFlowSummary(...args),
  useGetCashFlowHistory: (...args: unknown[]) => mockUseGetCashFlowHistory(...args),
  getGetCashFlowSummaryQueryKey: (...args: unknown[]) =>
    mockGetCashFlowSummaryQueryKey(...args),
  getGetCashFlowHistoryQueryKey: (...args: unknown[]) =>
    mockGetCashFlowHistoryQueryKey(...args),
}))
