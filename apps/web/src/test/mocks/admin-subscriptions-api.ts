/**
 * Centralized mock for admin subscription API endpoints.
 * Issue #646: Admin Subscription Management UI
 * 
 * This file is preloaded via bunfig.toml to prevent mock.module() pollution.
 */

import { mock } from 'bun:test'
import * as RealSubsModule from '@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions'

// SDK boundary doubles leave RevenueCat components' React Query behavior real.
export const mockGetAdminRevenueCatHealth = mock<typeof RealSubsModule.getAdminRevenueCatHealth>(async () => ({ configured: false, available: false, metrics: {} as never }))
export const mockListAdminRevenueCatWebhooks = mock<typeof RealSubsModule.listAdminRevenueCatWebhooks>(async () => [])
export const mockGetAdminRevenueCatUser = mock<typeof RealSubsModule.getAdminRevenueCatUser>(async () => { throw new Error('Unavailable') })
export const mockRequestAdminRevenueCatGrant = mock<typeof RealSubsModule.requestAdminRevenueCatGrant>()
export const mockRetryAdminRevenueCatOperation = mock<typeof RealSubsModule.retryAdminRevenueCatOperation>()
export const mockRefreshAdminRevenueCatUser = mock<typeof RealSubsModule.refreshAdminRevenueCatUser>()
export const mockReplayAdminRevenueCatWebhook = mock<typeof RealSubsModule.replayAdminRevenueCatWebhook>()

// === useGetAdminUserSubscription ===
export const mockUseGetAdminUserSubscription = mock(() => ({
    data: undefined,
    isLoading: true,
    error: null,
}))

// === useUpdateAdminUserTier ===
export const mockUpdateAdminUserTierMutate = mock(() => { })
export const mockUseUpdateAdminUserTier = mock(() => ({
    mutate: mockUpdateAdminUserTierMutate,
    isPending: false,
}))

// === useRemoveAdminUserTierOverride (Issue #1413/#1414 Task 13b: Remove Override button) ===
export const mockRemoveAdminUserTierOverrideMutate = mock(() => { })
export const mockUseRemoveAdminUserTierOverride = mock(() => ({
    mutate: mockRemoveAdminUserTierOverrideMutate,
    isPending: false,
}))

// === getGetAdminUserSubscriptionQueryKey ===
export const mockGetGetAdminUserSubscriptionQueryKey = mock((id: string) => [`/api/v1/admin/users/${id}/subscription`])

// Single mock.module() call
mock.module('@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions', () => ({
    ...RealSubsModule,
    getAdminRevenueCatHealth: mockGetAdminRevenueCatHealth,
    listAdminRevenueCatWebhooks: mockListAdminRevenueCatWebhooks,
    getAdminRevenueCatUser: mockGetAdminRevenueCatUser,
    requestAdminRevenueCatGrant: mockRequestAdminRevenueCatGrant,
    retryAdminRevenueCatOperation: mockRetryAdminRevenueCatOperation,
    refreshAdminRevenueCatUser: mockRefreshAdminRevenueCatUser,
    replayAdminRevenueCatWebhook: mockReplayAdminRevenueCatWebhook,
    useGetAdminUserSubscription: mockUseGetAdminUserSubscription,
    useUpdateAdminUserTier: mockUseUpdateAdminUserTier,
    useRemoveAdminUserTierOverride: mockUseRemoveAdminUserTierOverride,
    getGetAdminUserSubscriptionQueryKey: mockGetGetAdminUserSubscriptionQueryKey,
}))
