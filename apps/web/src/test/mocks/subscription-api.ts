// Issue #575: Centralized subscription API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealSubscriptionModule from '@boostedbudget/api-client/endpoints/subscription/subscription'
import type { RevenueCatSummary } from '@boostedbudget/api-client/models'

export const mockRefreshRevenueCatSubscription = mock<() => Promise<RevenueCatSummary>>(() =>
  Promise.resolve({ access_resolved: false, paid: null } as unknown as RevenueCatSummary))
// #1652: the adapter calls the raw generated client with the request body directly — the `{ data }`
// wrapper belongs to the generated mutation hook. The intent response now names the package.
export const mockBeginRevenueCatCheckout = mock((_body: { tier: string; billing_interval: 'month' | 'year' }) =>
  Promise.resolve({ id: 'intent_test', offering: 'web', package: 'premium_monthly', expires_at: '2026-10-01T00:00:00Z' }))
export const mockReleaseRevenueCatCheckout = mock(() => Promise.resolve())

// === useGetSubscriptionTiers ===
export const mockRefetchSubscriptionTiers = mock(() => Promise.resolve())
export const mockUseGetSubscriptionTiers = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchSubscriptionTiers,
}))

// === useGetCurrentSubscription ===
export const mockUseGetCurrentSubscription = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve()),
}))

// Default mutation state for all mutation hooks
const defaultMutationState = {
  isPending: false,
  isSuccess: false,
  isError: false,
  error: null,
  reset: mock(() => {}),
}

// === useCreatePaddleCheckout ===
// Issue #44, #45, #46, #48: Paddle checkout mock for testing
export const mockCreatePaddleCheckoutMutateAsync = mock(() =>
  Promise.resolve({
    checkout_url: 'https://checkout.paddle.com/txn_123',
    transaction_id: 'txn_123',
  })
)

export const mockUseCreatePaddleCheckout = mock(() => ({
  mutateAsync: mockCreatePaddleCheckoutMutateAsync,
  ...defaultMutationState,
}))

// === useGetBillingPortalUrl ===
export const mockUseGetBillingPortalUrl = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve()),
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/subscription/subscription', () => ({
  ...RealSubscriptionModule,
  refreshRevenueCatSubscription: mockRefreshRevenueCatSubscription,
  beginRevenueCatCheckout: mockBeginRevenueCatCheckout,
  releaseRevenueCatCheckout: mockReleaseRevenueCatCheckout,
  useGetSubscriptionTiers: mockUseGetSubscriptionTiers,
  useGetCurrentSubscription: mockUseGetCurrentSubscription,
  useCreatePaddleCheckout: mockUseCreatePaddleCheckout,
  useGetBillingPortalUrl: mockUseGetBillingPortalUrl,
}))
