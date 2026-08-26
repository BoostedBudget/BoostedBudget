// Issue #1296: Centralized investments API mock for hook implementation tests
// This file mocks at the API level so wrapper hooks can be tested with real implementations.
// IMPORTANT: mock.module() calls are process-global in bun:test — keep this file focused
// on the investments endpoint only; component tests that need wrapper-level mocks import
// the useInfiniteInvestmentTransactions mock directly.

import { mock } from 'bun:test'
import * as RealInvestmentsModule from '@boostedbudget/api-client/endpoints/investments/investments'
import type {
  GetAccountHoldingsHistoryQueryResult,
  GetHoldingsQueryResult,
} from '@boostedbudget/api-client/endpoints/investments/investments'
import type { GetAccountHoldingsHistoryParams } from '@boostedbudget/api-client/models/getAccountHoldingsHistoryParams'
import type { GetHoldingsParams } from '@boostedbudget/api-client/models/getHoldingsParams'

// A `data: undefined` default narrows the factory's inferred return type to `undefined`, so every
// `mockReturnValue({ data: … })` in a test is then a type error against the default rather than
// against the response. Annotate the return type so `data` is `TData | undefined` from the start.
type QueryStateMock<TData> = {
  data: TData | undefined
  isLoading: boolean
  isError: boolean
  /** react-query hands the consumer the rejection too; PortfolioHoldingsWidget renders its message. */
  error?: unknown
}

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
export const mockUseGetHoldings = mock(
  (_params?: GetHoldingsParams, _options?: unknown): QueryStateMock<GetHoldingsQueryResult> => ({
    data: undefined,
    isLoading: false,
    isError: false,
  }),
)

// === useGetAccountHoldingsHistory (#1569 — a CLOSED account's retained positions) ===
// Defaults to "no history recorded", which is the honest zero-value for an account nothing was
// ever captured for; tests that want rows set it explicitly.
export const mockUseGetAccountHoldingsHistory = mock(
  (
    _params?: GetAccountHoldingsHistoryParams,
    _options?: unknown,
  ): QueryStateMock<GetAccountHoldingsHistoryQueryResult> => ({
    data: undefined,
    isLoading: false,
    isError: false,
  }),
)

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
  useGetAccountHoldingsHistory: mockUseGetAccountHoldingsHistory,
}))
