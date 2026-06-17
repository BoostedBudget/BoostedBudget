// Issue #575: Centralized transactions API mock for hook implementation tests
// This file mocks at the API level so wrapper hooks can be tested with real implementation

import { mock } from 'bun:test'
import * as RealTransactionsModule from '@boostedbudget/api-client/endpoints/transactions/transactions'
import type { InvestmentActivity } from '@/features/transactions/types'

// === Investment activity fixture (#1299) ===
export const makeInvestmentActivityFixture = (id: string): InvestmentActivity => ({
  id,
  user_id: 'user-123',
  account_id: 'account-inv',
  source: 'investment' as const,
  amount: -1500.0,
  date: '2025-10-25',
  name: `Buy AAPL (${id})`,
  ticker_symbol: 'AAPL',
  security_name: 'Apple Inc.',
  activity_type: 'buy',
  investment_quantity: 10,
  investment_price: 150.0,
  investment_fees: 0.0,
  created_at: '2025-10-25T10:00:00Z',
  updated_at: '2025-10-25T10:00:00Z',
})

// === getTransactions (raw fetcher — used by useInfiniteTransactions) ===
export const mockGetTransactions = mock(() => Promise.resolve({ transactions: [], limit: 50 }))

// === useGetTransactions ===
export const mockRefetchTransactions = mock(() => Promise.resolve({}))
export const mockUseGetTransactions = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchTransactions,
}))

// === useUpdateTransactionTags ===
export const mockUpdateTransactionTagsMutateAsync = mock(() => Promise.resolve({}))
export const mockUseUpdateTransactionTags = mock(() => ({
  mutateAsync: mockUpdateTransactionTagsMutateAsync,
  isPending: false,
}))

// === useUpdateTransactionCategory ===
export const mockUpdateTransactionCategoryMutateAsync = mock(() => Promise.resolve({}))
export const mockUseUpdateTransactionCategory = mock(() => ({
  mutateAsync: mockUpdateTransactionCategoryMutateAsync,
  isPending: false,
}))

// === useInfiniteTransactions ===
export const mockFetchNextPage = mock(() => Promise.resolve())
export const mockUseInfiniteTransactions = mock(() => ({
  transactions: [],
  fetchNextPage: mockFetchNextPage,
  hasNextPage: false,
  isFetchingNextPage: false,
  isLoading: false,
  isError: false,
  isSuccess: false,
  updateTransaction: mock(() => Promise.resolve()),
}))

// === useSetTransactionReviewed / useBulkSetTransactionReviewed (#1203) ===
export const mockSetTransactionReviewedMutateAsync = mock(() => Promise.resolve({}))
export const mockUseSetTransactionReviewed = mock(() => ({
  mutateAsync: mockSetTransactionReviewedMutateAsync,
  isPending: false,
}))
export const mockBulkSetTransactionReviewedMutateAsync = mock(() => Promise.resolve({}))
export const mockUseBulkSetTransactionReviewed = mock(() => ({
  mutateAsync: mockBulkSetTransactionReviewedMutateAsync,
  isPending: false,
}))

// === useBulkUpdateTransactionCategory / useGetTransactionReviewSummary (#1210) ===
export const mockBulkUpdateTransactionCategoryMutateAsync = mock(() =>
  Promise.resolve({ requested: 1, updated: 1 }),
)
export const mockUseBulkUpdateTransactionCategory = mock(() => ({
  mutateAsync: mockBulkUpdateTransactionCategoryMutateAsync,
  isPending: false,
}))
export const mockUseGetTransactionReviewSummary = mock(() => ({
  data: undefined,
  isLoading: false,
  isError: false,
}))

// Query key mock
export const mockGetTransactionsQueryKey = mock(() => ['transactions'])

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/transactions/transactions', () => ({
  ...RealTransactionsModule,
  getTransactions: mockGetTransactions,
  useGetTransactions: mockUseGetTransactions,
  useUpdateTransactionTags: mockUseUpdateTransactionTags,
  useUpdateTransactionCategory: mockUseUpdateTransactionCategory,
  useSetTransactionReviewed: mockUseSetTransactionReviewed,
  useBulkSetTransactionReviewed: mockUseBulkSetTransactionReviewed,
  useBulkUpdateTransactionCategory: mockUseBulkUpdateTransactionCategory,
  useGetTransactionReviewSummary: mockUseGetTransactionReviewSummary,
  getGetTransactionsQueryKey: mockGetTransactionsQueryKey,
}))
