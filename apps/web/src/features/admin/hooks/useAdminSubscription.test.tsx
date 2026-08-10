/**
 * useAdminSubscription tests
 * Issue #646: Admin Subscription Management UI
 * Issue #1413/#1414 Task 13b: Remove Override mutation hook + onSuccess invalidation coverage.
 *
 * Uses centralized mocks from src/test/mocks/admin-subscriptions-api.ts
 *
 * Issue #1413 Fix 7 (G7.5): this file previously called `mock.module()` on the api-client
 * module directly, WITHOUT spreading the real module — replacing it with a 4-export stub
 * process-globally for every file loaded later in the same bun process (the exact fragility
 * SubscriptionOverrideModal.test.tsx was migrated away from — see
 * feedback_bun_test_mock_module_global.md). Now it consumes the centralized, spread-based mock
 * in src/test/mocks/admin-subscriptions-api.ts (preloaded via bunfig.toml) instead, same as
 * SubscriptionOverrideModal.test.tsx — no local mock.module() call needed at all.
 */

import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { renderHook } from '@testing-library/react'
import {
    mockUseGetAdminUserSubscription,
    mockUseUpdateAdminUserTier,
    mockUseRemoveAdminUserTierOverride,
} from '@/test/mocks/admin-subscriptions-api'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
    getListAdminUsersQueryKey,
    getGetAdminUserDetailQueryKey,
} from '@boostedbudget/api-client/endpoints/admin/admin'
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

    // Round 15, Tier E item 3. React Query invalidation is a PREFIX match over the key ARRAY, and
    // these two keys share no prefix: ['/api/v1/admin/users'] vs ['/api/v1/admin/users/user-xyz'] —
    // one element each, different elements. So invalidating the list did NOT refetch the user
    // detail, and UserDetailModal — the modal the override is applied from — kept showing the
    // pre-override tier, under a SnapshotAgeLabel reading seconds because `as_of` came from that
    // same stale response.
    //
    // ⚠ The assertion is on the exact key, not on a call count: a test that only counted
    // invalidations would already have passed against the two that were there.
    it('invalidates the user DETAIL query key on success — the list key does not cover it', () => {
        mockUseUpdateAdminUserTier.mockReturnValue({ mutate: () => { }, isPending: false })
        const { wrapper, qc } = createWrapperWithClient()
        const invalidateSpy = spyOn(qc, 'invalidateQueries')

        renderHook(() => useUpdateUserTier('user-xyz'), { wrapper })

        const options = mockUseUpdateAdminUserTier.mock.calls.at(-1)?.[0] as MutationOptionsArg | undefined
        options?.mutation?.onSuccess?.()

        expect(invalidateSpy).toHaveBeenCalledWith({
            queryKey: getGetAdminUserDetailQueryKey('user-xyz'),
        })
    })

    // ⚠ SIBLING: the two keys really are distinct, which is the whole premise. If a future
    // regeneration ever made the detail key a prefix-extension of the list key, the assertion above
    // would start passing for the wrong reason and this would fail instead.
    it('the user detail key is not reachable by a prefix match on the list key', () => {
        expect(getGetAdminUserDetailQueryKey('user-xyz')[0]).not.toBe(getListAdminUsersQueryKey()[0])
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

    // Round 15, Tier E item 3 — stated for REMOVE as well as APPLY. Removing a comp is the same
    // stale-tier symptom in the opposite direction, and a fix scoped to the apply mutation only
    // would leave it live.
    it('invalidates the user DETAIL query key on success — the list key does not cover it', () => {
        mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: () => { }, isPending: false })
        const { wrapper, qc } = createWrapperWithClient()
        const invalidateSpy = spyOn(qc, 'invalidateQueries')

        renderHook(() => useRemoveUserTierOverride('user-xyz'), { wrapper })

        const options = mockUseRemoveAdminUserTierOverride.mock.calls.at(-1)?.[0] as MutationOptionsArg | undefined
        options?.mutation?.onSuccess?.()

        expect(invalidateSpy).toHaveBeenCalledWith({
            queryKey: getGetAdminUserDetailQueryKey('user-xyz'),
        })
    })
})
