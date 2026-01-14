// Issue #575: Centralized cash flow wrapper hook mocks
// These mock the WRAPPER hooks (useCashFlowSummary, useCashFlowHistory)
// for component tests that need to test in isolation from the actual hook implementation.
//
// NOTE: Hook implementation tests (e.g., useCashFlowData.test.tsx) use API-level mocks
// from analytics-api.ts instead, so they test real hook behavior.

import { mock } from 'bun:test'

// === useCashFlowSummary mock ===
export const mockCashFlowSummaryRefetch = mock(() => Promise.resolve())
export const mockUseCashFlowSummary = mock(() => ({
  data: null,
  loading: false,
  error: null,
  refetch: mockCashFlowSummaryRefetch,
}))

// === useCashFlowHistory mock ===
export const mockCashFlowHistoryRefetch = mock(() => Promise.resolve())
export const mockUseCashFlowHistory = mock(() => ({
  data: null,
  loading: false,
  error: null,
  refetch: mockCashFlowHistoryRefetch,
}))

// Mock both absolute and relative paths
mock.module('@/features/dashboard/hooks/useCashFlowData', () => ({
  useCashFlowSummary: (...args: unknown[]) => mockUseCashFlowSummary(...args),
  useCashFlowHistory: (...args: unknown[]) => mockUseCashFlowHistory(...args),
}))

mock.module('../hooks/useCashFlowData', () => ({
  useCashFlowSummary: (...args: unknown[]) => mockUseCashFlowSummary(...args),
  useCashFlowHistory: (...args: unknown[]) => mockUseCashFlowHistory(...args),
}))
