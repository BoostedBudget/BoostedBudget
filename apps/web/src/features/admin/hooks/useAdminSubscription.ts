/**
 * useAdminSubscription
 * Issue #646: Admin Subscription Management UI
 *
 * Wraps the generated API client for admin subscription endpoints.
 */

import { useQueryClient } from '@tanstack/react-query'
import {
    useGetAdminUserSubscription,
    getGetAdminUserSubscriptionQueryKey,
    useUpdateAdminUserTier,
    useCreateAdminSubscriptionOverride,
    useRemoveAdminUserTierOverride,
} from '@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions'
import {
    getListAdminUsersQueryKey,
    getGetAdminUserDetailQueryKey,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import type { AdminSubscriptionDetailOutputBody } from '@boostedbudget/api-client/models'

export type { AdminSubscriptionDetailOutputBody }

/** Fetch the full subscription detail for a single user. */
export function useAdminSubscriptionDetail(userId: string | null) {
    return useGetAdminUserSubscription(userId ?? '', {
        query: { enabled: !!userId },
    })
}

/** Mutation: update a user's subscription tier with optional expiry. */
export function useUpdateUserTier(userId: string) {
    const queryClient = useQueryClient()

    return useUpdateAdminUserTier({
        mutation: {
            onSuccess: () => {
                // Invalidate the subscription detail so it refetches
                queryClient.invalidateQueries({
                    queryKey: getGetAdminUserSubscriptionQueryKey(userId),
                })
                // Also invalidate the user list so tier badges update immediately
                queryClient.invalidateQueries({
                    queryKey: getListAdminUsersQueryKey(),
                })
                // Round 15, Tier E item 3 — AND THE USER DETAIL, WHICH THE LIST KEY DOES NOT COVER.
                // React Query's invalidation is a PREFIX match over the key array, and these two keys
                // do not share a prefix: the list is ['/api/v1/admin/users'] while the detail is
                // ['/api/v1/admin/users/<id>'] — one element each, and different elements. So
                // invalidating the list left UserDetailModal — the very modal this override was
                // applied from — showing the PRE-override tier.
                //
                // ⚠ AND IT LOOKED FRESH. The modal renders SnapshotAgeLabel from `user.as_of`, which
                // comes out of that same stale detail response, so the stale tier was displayed under
                // an age label reading seconds — a wrong value wearing a correct-looking timestamp,
                // which is worse than an obviously stale one.
                queryClient.invalidateQueries({
                    queryKey: getGetAdminUserDetailQueryKey(userId),
                })
            },
        },
    })
}

/** Mutation: remove a user's manual subscription tier override, reverting to their base tier. */
export function useRemoveUserTierOverride(userId: string) {
    const queryClient = useQueryClient()

    return useRemoveAdminUserTierOverride({
        mutation: {
            onSuccess: () => {
                // Invalidate the subscription detail so it refetches
                queryClient.invalidateQueries({
                    queryKey: getGetAdminUserSubscriptionQueryKey(userId),
                })
                // Also invalidate the user list so tier badges update immediately
                queryClient.invalidateQueries({
                    queryKey: getListAdminUsersQueryKey(),
                })
                // Round 15, Tier E item 3 — AND THE USER DETAIL, WHICH THE LIST KEY DOES NOT COVER.
                // React Query's invalidation is a PREFIX match over the key array, and these two keys
                // do not share a prefix: the list is ['/api/v1/admin/users'] while the detail is
                // ['/api/v1/admin/users/<id>'] — one element each, and different elements. So
                // invalidating the list left UserDetailModal — the very modal this override was
                // applied from — showing the PRE-override tier.
                //
                // ⚠ AND IT LOOKED FRESH. The modal renders SnapshotAgeLabel from `user.as_of`, which
                // comes out of that same stale detail response, so the stale tier was displayed under
                // an age label reading seconds — a wrong value wearing a correct-looking timestamp,
                // which is worse than an obviously stale one.
                queryClient.invalidateQueries({
                    queryKey: getGetAdminUserDetailQueryKey(userId),
                })
            },
        },
    })
}

/** Mutation: create a per-resource limit override for a user. */
export function useCreateOverride(userId: string) {
    const queryClient = useQueryClient()

    return useCreateAdminSubscriptionOverride({
        mutation: {
            onSuccess: () => {
                queryClient.invalidateQueries({
                    queryKey: getGetAdminUserSubscriptionQueryKey(userId),
                })
            },
        },
    })
}
