// Issue #957: Centralized merchants API mock preloaded before all tests

import { mock } from 'bun:test'
import * as RealMerchantsModule from '@boostedbudget/api-client/endpoints/merchants/merchants'

// === useGetMerchants ===
export const mockUseGetMerchants = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve()),
}))

// === useGetMerchantDuplicateSuggestions ===
export const mockUseGetMerchantDuplicateSuggestions = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
}))

// === useSetMerchantOverride ===
export const mockSetMerchantOverrideMutateAsync = mock(() => Promise.resolve({}))
export const mockUseSetMerchantOverride = mock(() => ({
  mutateAsync: mockSetMerchantOverrideMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useClearMerchantOverride ===
export const mockClearMerchantOverrideMutateAsync = mock(() => Promise.resolve({}))
export const mockUseClearMerchantOverride = mock(() => ({
  mutateAsync: mockClearMerchantOverrideMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useBulkSetMerchantOverrides ===
export const mockBulkSetMerchantOverridesMutateAsync = mock(() => Promise.resolve([]))
export const mockUseBulkSetMerchantOverrides = mock(() => ({
  mutateAsync: mockBulkSetMerchantOverridesMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useMergeMerchants ===
export const mockMergeMerchantsMutateAsync = mock(() => Promise.resolve([]))
export const mockUseMergeMerchants = mock(() => ({
  mutateAsync: mockMergeMerchantsMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

mock.module('@boostedbudget/api-client/endpoints/merchants/merchants', () => ({
  ...RealMerchantsModule,
  useGetMerchants: mockUseGetMerchants,
  useGetMerchantDuplicateSuggestions: mockUseGetMerchantDuplicateSuggestions,
  useSetMerchantOverride: mockUseSetMerchantOverride,
  useClearMerchantOverride: mockUseClearMerchantOverride,
  useBulkSetMerchantOverrides: mockUseBulkSetMerchantOverrides,
  useMergeMerchants: mockUseMergeMerchants,
}))
