/**
 * Centralized mock for admin subscription API endpoints.
 * Issue #646: Admin Subscription Management UI
 * 
 * This file is preloaded via bunfig.toml to prevent mock.module() pollution.
 */

import { mock } from 'bun:test'
import * as RealSubsModule from '@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions'

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

// === getGetAdminUserSubscriptionQueryKey ===
export const mockGetGetAdminUserSubscriptionQueryKey = mock((id: string) => [`/api/v1/admin/users/${id}/subscription`])

// Single mock.module() call
mock.module('@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions', () => ({
    ...RealSubsModule,
    useGetAdminUserSubscription: mockUseGetAdminUserSubscription,
    useUpdateAdminUserTier: mockUseUpdateAdminUserTier,
    getGetAdminUserSubscriptionQueryKey: mockGetGetAdminUserSubscriptionQueryKey,
}))
