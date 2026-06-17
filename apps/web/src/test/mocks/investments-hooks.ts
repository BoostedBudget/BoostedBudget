// Issue #1296: Wrapper hook mocks for investment component tests.
// Preloaded globally via bunfig.toml (after investments-api.ts).
//
// Mocks the WRAPPER hook (useInfiniteInvestmentTransactions) process-globally so that
// component tests (InvestmentTransactionsTable, PortfolioPage) use the synchronous mock
// without needing real React Query — preventing waitFor timeout races in the full suite.
//
// Hook implementation tests (useInfiniteInvestmentTransactions.test.tsx) use API-level
// mocks from investments-api.ts and run via test-real-implementations.sh (real-impl pass).

import { mock } from 'bun:test'
import { mockFetchNextInvestmentPage, mockUseInfiniteInvestmentTransactions } from './investments-api'

// Mock both absolute and relative paths for the wrapper hook
mock.module('@/features/investments/hooks/useInfiniteInvestmentTransactions', () => ({
  useInfiniteInvestmentTransactions: (...args: unknown[]) =>
    mockUseInfiniteInvestmentTransactions(...args),
}))

// Re-export the mock handles for test files that need them
export { mockFetchNextInvestmentPage, mockUseInfiniteInvestmentTransactions }
