// Issue #1296: Centralized investments API mock for hook implementation tests
// This file mocks at the API level so wrapper hooks can be tested with real implementations.
// IMPORTANT: mock.module() calls are process-global in bun:test — keep this file focused
// on the investments endpoint only; component tests that need wrapper-level mocks import
// the useInfiniteInvestmentTransactions mock directly.

import { mock } from 'bun:test'
import * as RealInvestmentsModule from '@boostedbudget/api-client/endpoints/investments/investments'

// === getInvestmentTransactions (raw fetcher — used by useInfiniteInvestmentTransactions) ===
export const mockGetInvestmentTransactions = mock(() =>
  Promise.resolve({ transactions: [], limit: 50 }),
)

// === useGetPortfolioSummary ===
export const mockUseGetPortfolioSummary = mock(() => ({
  data: undefined,
  isLoading: false,
  isError: false,
}))

// === useGetPortfolioHistory ===
export const mockUseGetPortfolioHistory = mock(() => ({
  data: undefined,
  isLoading: false,
  isError: false,
}))

// === useGetHoldings ===
export const mockUseGetHoldings = mock(() => ({
  data: undefined,
  isLoading: false,
  isError: false,
}))

// === useInfiniteInvestmentTransactions (wrapper hook mock for component tests) ===
export const mockFetchNextInvestmentPage = mock(() => Promise.resolve())
export const mockUseInfiniteInvestmentTransactions = mock(() => ({
  transactions: [],
  fetchNextPage: mockFetchNextInvestmentPage,
  hasNextPage: false,
  isFetchingNextPage: false,
  isLoading: false,
  isError: false,
  isSuccess: false,
}))

// API-client mock — called once at preload time
// NOTE: useGetPortfolioAllocationBreakdown lives in analytics/analytics (not investments).
// It is mocked in analytics-api.ts instead.
mock.module('@boostedbudget/api-client/endpoints/investments/investments', () => ({
  ...RealInvestmentsModule,
  getInvestmentTransactions: mockGetInvestmentTransactions,
  useGetPortfolioSummary: mockUseGetPortfolioSummary,
  useGetPortfolioHistory: mockUseGetPortfolioHistory,
  useGetHoldings: mockUseGetHoldings,
}))

