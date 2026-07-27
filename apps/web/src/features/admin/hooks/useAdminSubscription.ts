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
import { getListAdminUsersQueryKey } from '@boostedbudget/api-client/endpoints/admin/admin'
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
