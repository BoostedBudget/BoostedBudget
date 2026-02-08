// Issue #575: Centralized subscription API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealSubscriptionModule from '@boostedbudget/api-client/endpoints/subscription/subscription'

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
  isPending: false,
  isSuccess: false,
  isError: false,
  error: null,
  reset: mock(() => {}),
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/subscription/subscription', () => ({
  ...RealSubscriptionModule,
  useGetSubscriptionTiers: mockUseGetSubscriptionTiers,
  useGetCurrentSubscription: mockUseGetCurrentSubscription,
  useCreatePaddleCheckout: mockUseCreatePaddleCheckout,
}))
