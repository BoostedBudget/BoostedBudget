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

// === usePreviewPaddleSubscriptionUpdate ===
export const mockPreviewUpdateMutateAsync = mock(() =>
  Promise.resolve({
    proration_amount: '5.00',
    currency: 'USD',
    next_charge_date: '2026-03-15',
  })
)

export const mockUsePreviewPaddleSubscriptionUpdate = mock(() => ({
  mutateAsync: mockPreviewUpdateMutateAsync,
  ...defaultMutationState,
}))

// === useUpdatePaddleSubscription ===
export const mockUpdateSubscriptionMutateAsync = mock(() =>
  Promise.resolve({
    subscription_id: 'sub_123',
    status: 'active',
    message: 'Subscription updated successfully',
  })
)

export const mockUseUpdatePaddleSubscription = mock(() => ({
  mutateAsync: mockUpdateSubscriptionMutateAsync,
  ...defaultMutationState,
}))

// === useCancelPaddleSubscription ===
export const mockCancelSubscriptionMutateAsync = mock(() =>
  Promise.resolve({
    subscription_id: 'sub_123',
    message: 'Subscription cancelled',
    ends_at: '2026-03-15',
  })
)

export const mockUseCancelPaddleSubscription = mock(() => ({
  mutateAsync: mockCancelSubscriptionMutateAsync,
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
  useGetSubscriptionTiers: mockUseGetSubscriptionTiers,
  useGetCurrentSubscription: mockUseGetCurrentSubscription,
  useCreatePaddleCheckout: mockUseCreatePaddleCheckout,
  usePreviewPaddleSubscriptionUpdate: mockUsePreviewPaddleSubscriptionUpdate,
  useUpdatePaddleSubscription: mockUseUpdatePaddleSubscription,
  useCancelPaddleSubscription: mockUseCancelPaddleSubscription,
  useGetBillingPortalUrl: mockUseGetBillingPortalUrl,
}))
