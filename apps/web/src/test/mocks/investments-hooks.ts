// Issue #1296: Wrapper hook mocks for investment component tests.
// These mock the WRAPPER hooks (useInfiniteInvestmentTransactions)
// for component tests that need to test in isolation from the actual hook implementation.
//
// NOTE: Hook implementation tests (e.g., useInfiniteInvestmentTransactions.test.tsx) use
// API-level mocks from investments-api.ts instead, so they test real hook behavior.
// This file is NOT in bunfig.toml preload — import it directly in component test files
// that need wrapper-level mocks.

import { mock } from 'bun:test'
import { mockFetchNextInvestmentPage, mockUseInfiniteInvestmentTransactions } from './investments-api'

// Mock both absolute and relative paths for the wrapper hook
mock.module('@/features/investments/hooks/useInfiniteInvestmentTransactions', () => ({
  useInfiniteInvestmentTransactions: (...args: unknown[]) =>
    mockUseInfiniteInvestmentTransactions(...args),
}))

// Re-export the mock handles for test files that need them
export { mockFetchNextInvestmentPage, mockUseInfiniteInvestmentTransactions }
