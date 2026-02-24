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
} from '@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions'
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
