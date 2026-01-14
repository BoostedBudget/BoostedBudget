// Issue #575: Centralized net worth hook mocks
// These mock the WRAPPER hook (useNetWorthData) for component tests
// that need to test in isolation from the actual hook implementation.
//
// NOTE: Hook implementation tests use API-level mocks from analytics-api.ts instead.

import { mock } from 'bun:test'

// === useNetWorthData mock ===
export const mockNetWorthRefetch = mock(() => {})
export const mockUseNetWorthData = mock(() => ({
  summary: null,
  history: null,
  loading: false,
  error: null,
  refetch: mockNetWorthRefetch,
}))

// Mock both absolute and relative paths
mock.module('@/features/dashboard/hooks/useNetWorthData', () => ({
  useNetWorthData: (...args: unknown[]) => mockUseNetWorthData(...args),
}))

mock.module('../hooks/useNetWorthData', () => ({
  useNetWorthData: (...args: unknown[]) => mockUseNetWorthData(...args),
}))
