// Issue #575: Centralized dashboard hooks mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

// Default refetch function
export const mockRefetch = mock(() => {})

// Default data state for useDashboardData
export const defaultDashboardDataState = {
  summary: null,
  history: null,
  accountTypes: null,
  loading: false,
  error: null,
  refetch: mockRefetch,
}

// Default data state for useNetWorthData
export const defaultNetWorthDataState = {
  summary: null,
  history: null,
  loading: false,
  error: null,
  refetch: mockRefetch,
}

// Default data state for useCashFlowSummary
export const defaultCashFlowSummaryState = {
  data: null,
  loading: false,
  error: null,
  refetch: mockRefetch,
}

// Default data state for useCashFlowHistory
export const defaultCashFlowHistoryState = {
  data: null,
  loading: false,
  error: null,
  refetch: mockRefetch,
}

// Configurable mock functions - tests customize in beforeEach
export const mockUseDashboardData = mock(() => defaultDashboardDataState)
export const mockUseNetWorthData = mock(() => defaultNetWorthDataState)
export const mockUseCashFlowSummary = mock(() => defaultCashFlowSummaryState)
export const mockUseCashFlowHistory = mock(() => defaultCashFlowHistoryState)

// Single mock.module() calls - called once at preload time
mock.module('@/features/dashboard/hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))

mock.module('@/features/dashboard/hooks/useNetWorthData', () => ({
  useNetWorthData: (...args: unknown[]) => mockUseNetWorthData(...args),
}))

mock.module('@/features/dashboard/hooks/useCashFlowData', () => ({
  useCashFlowSummary: (...args: unknown[]) => mockUseCashFlowSummary(...args),
  useCashFlowHistory: (...args: unknown[]) => mockUseCashFlowHistory(...args),
}))

// Also mock relative path imports
mock.module('../hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))

mock.module('../hooks/useNetWorthData', () => ({
  useNetWorthData: (...args: unknown[]) => mockUseNetWorthData(...args),
}))

mock.module('../hooks/useCashFlowData', () => ({
  useCashFlowSummary: (...args: unknown[]) => mockUseCashFlowSummary(...args),
  useCashFlowHistory: (...args: unknown[]) => mockUseCashFlowHistory(...args),
}))
