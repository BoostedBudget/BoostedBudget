// Issue #575: Centralized transactions API mock for hook implementation tests
// This file mocks at the API level so wrapper hooks can be tested with real implementation
// Used by: useTransactions.test.tsx

import { mock } from 'bun:test'
import * as RealTransactionsModule from '@boostedbudget/api-client/endpoints/transactions/transactions'

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
  updateTransaction: mock(() => Promise.resolve()),
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
  getGetTransactionsQueryKey: mockGetTransactionsQueryKey,
}))
