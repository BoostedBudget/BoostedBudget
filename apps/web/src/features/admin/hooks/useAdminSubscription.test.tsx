/**
 * useAdminSubscription tests
 * Issue #646: Admin Subscription Management UI
 * 
 * Uses centralized mocks from src/test/mocks/admin-subscriptions-api.ts
 */

import { mock } from 'bun:test'

// Mock the generated API client BEFORE importing anything else
const mockUseGetAdminUserSubscription = mock(() => ({}))
const mockUseUpdateAdminUserTier = mock(() => ({}))

mock.module('@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions', () => ({
    useGetAdminUserSubscription: mockUseGetAdminUserSubscription,
    useUpdateAdminUserTier: mockUseUpdateAdminUserTier,
    getGetAdminUserSubscriptionQueryKey: (id: string) => [`/api/v1/admin/users/${id}/subscription`],
}))

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useAdminSubscriptionDetail, useUpdateUserTier } from './useAdminSubscription'
import { ReactNode } from 'react'

function createWrapper() {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    )
}

describe('useAdminSubscriptionDetail', () => {
    beforeEach(() => {
        mockUseGetAdminUserSubscription.mockClear()
    })

    it('returns subscription data when userId is provided', () => {
        const mockData = {
            base_tier: 'premium',
            effective_tier: 'premium',
            subscription_status: 'active',
            limits: { accounts: 30, transaction_history_days: 730, executions_per_day: 6 },
            features: { ai_insights: true },
            manual_tier_override: null,
            manual_tier_ends_at: null,
        }
        mockUseGetAdminUserSubscription.mockReturnValue({ data: mockData, isLoading: false })

        const { result } = renderHook(() => useAdminSubscriptionDetail('user-abc'), {
            wrapper: createWrapper(),
        })

        expect(result.current.data).toEqual(mockData)
        expect(result.current.isLoading).toBe(false)
    })
})

describe('useUpdateUserTier', () => {
    beforeEach(() => {
        mockUseUpdateAdminUserTier.mockClear()
    })

    it('returns a mutate function', () => {
        mockUseUpdateAdminUserTier.mockReturnValue({
            mutate: () => { },
            isPending: false,
        })

        const { result } = renderHook(() => useUpdateUserTier('user-xyz'), {
            wrapper: createWrapper(),
        })

        expect(result.current.mutate).toBeDefined()
        expect(result.current.isPending).toBe(false)
    })
})
