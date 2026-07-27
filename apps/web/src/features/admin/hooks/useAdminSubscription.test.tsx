/**
 * useAdminSubscription tests
 * Issue #646: Admin Subscription Management UI
 * Issue #1413/#1414 Task 13b: Remove Override mutation hook + onSuccess invalidation coverage.
 *
 * Uses centralized mocks from src/test/mocks/admin-subscriptions-api.ts
 */

import { mock } from 'bun:test'

// Mock the generated API client BEFORE importing anything else
const mockUseGetAdminUserSubscription = mock(() => ({}))
const mockUseUpdateAdminUserTier = mock((_options?: unknown) => ({}))
const mockUseRemoveAdminUserTierOverride = mock((_options?: unknown) => ({}))

mock.module('@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions', () => ({
    useGetAdminUserSubscription: mockUseGetAdminUserSubscription,
    useUpdateAdminUserTier: mockUseUpdateAdminUserTier,
    useRemoveAdminUserTierOverride: mockUseRemoveAdminUserTierOverride,
    getGetAdminUserSubscriptionQueryKey: (id: string) => [`/api/v1/admin/users/${id}/subscription`],
}))

import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { getListAdminUsersQueryKey } from '@boostedbudget/api-client/endpoints/admin/admin'
import { useAdminSubscriptionDetail, useUpdateUserTier, useRemoveUserTierOverride } from './useAdminSubscription'
import { ReactNode } from 'react'

type MutationOptionsArg = { mutation?: { onSuccess?: (...args: unknown[]) => void } }

function createWrapper() {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    )
}

/** Returns a wrapper plus the underlying QueryClient so tests can spy on invalidateQueries. */
function createWrapperWithClient() {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    function wrapper({ children }: { children: ReactNode }) {
        return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    }
    return { wrapper, qc }
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
            limits: { transaction_history_days: 730 },
            features: { api_access: true },
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

    it('invalidates the subscription detail and admin user list query keys on success', () => {
        mockUseUpdateAdminUserTier.mockReturnValue({ mutate: () => { }, isPending: false })
        const { wrapper, qc } = createWrapperWithClient()
        const invalidateSpy = spyOn(qc, 'invalidateQueries')

        renderHook(() => useUpdateUserTier('user-xyz'), { wrapper })

        const options = mockUseUpdateAdminUserTier.mock.calls.at(-1)?.[0] as MutationOptionsArg | undefined
        options?.mutation?.onSuccess?.()

        expect(invalidateSpy).toHaveBeenCalledWith({
            queryKey: ['/api/v1/admin/users/user-xyz/subscription'],
        })
        expect(invalidateSpy).toHaveBeenCalledWith({
            queryKey: getListAdminUsersQueryKey(),
        })
    })
})

describe('useRemoveUserTierOverride', () => {
    beforeEach(() => {
        mockUseRemoveAdminUserTierOverride.mockClear()
    })

    it('returns a mutate function', () => {
        mockUseRemoveAdminUserTierOverride.mockReturnValue({
            mutate: () => { },
            isPending: false,
        })

        const { result } = renderHook(() => useRemoveUserTierOverride('user-xyz'), {
            wrapper: createWrapper(),
        })

        expect(result.current.mutate).toBeDefined()
        expect(result.current.isPending).toBe(false)
    })

    it('invalidates the subscription detail query key on success', () => {
        mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: () => { }, isPending: false })
        const { wrapper, qc } = createWrapperWithClient()
        const invalidateSpy = spyOn(qc, 'invalidateQueries')

        renderHook(() => useRemoveUserTierOverride('user-xyz'), { wrapper })

        const options = mockUseRemoveAdminUserTierOverride.mock.calls.at(-1)?.[0] as MutationOptionsArg | undefined
        options?.mutation?.onSuccess?.()

        expect(invalidateSpy).toHaveBeenCalledWith({
            queryKey: ['/api/v1/admin/users/user-xyz/subscription'],
        })
    })

    it('invalidates the admin user list query key on success', () => {
        mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: () => { }, isPending: false })
        const { wrapper, qc } = createWrapperWithClient()
        const invalidateSpy = spyOn(qc, 'invalidateQueries')

        renderHook(() => useRemoveUserTierOverride('user-xyz'), { wrapper })

        const options = mockUseRemoveAdminUserTierOverride.mock.calls.at(-1)?.[0] as MutationOptionsArg | undefined
        options?.mutation?.onSuccess?.()

        expect(invalidateSpy).toHaveBeenCalledWith({
            queryKey: getListAdminUsersQueryKey(),
        })
    })
})
