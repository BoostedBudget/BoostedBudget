/**
 * useHouseholdMembers Hook
 *
 * Issue #1370: Household feature — hooks layer
 *
 * Thin wrap of `useListHouseholdMembers`, plus a `removeMember` action backed by
 * `useRemoveHouseholdMember`. Removing (or self-leaving) a household changes the
 * seat count, so the members list and the entitlement summary are invalidated
 * on success. `removeMember` is also used for self-leave (via
 * `LeaveHouseholdDialog`), which must invalidate `getMyHousehold` too —
 * otherwise `useHousehold()`'s cached `isInHousehold` stays stale `true` after
 * leaving, leaving the Household tab/HouseholdCard visible in a broken state.
 * It also changes what's included in the household aggregate (a removed/left
 * member's shared balances should no longer be counted), so the same 9 scoped
 * dashboard/analytics/investments queries invalidated by useAcceptInvite.ts/
 * useHouseholdVisibility.ts are invalidated here too — see
 * `invalidateScopedQueries.ts` for the shared helper.
 */

import { useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useListHouseholdMembers,
  useRemoveHouseholdMember,
  getListHouseholdMembersQueryKey,
  getGetHouseholdEntitlementQueryKey,
  getGetMyHouseholdQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import type { HouseholdMemberView } from '../types'
import { invalidateScopedDashboardAnalyticsQueries } from './invalidateScopedQueries'

export interface UseHouseholdMembersResult {
  members: HouseholdMemberView[]
  isLoading: boolean
  error: Error | null
  refetch: () => Promise<void>
  removeMember: (userId: string) => Promise<void>
}

export function useHouseholdMembers(): UseHouseholdMembersResult {
  const queryClient = useQueryClient()

  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useListHouseholdMembers()

  const members = (data ?? []) as unknown as HouseholdMemberView[]

  const error = queryError
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch household members')
    : null

  const removeMemberMutation = useRemoveHouseholdMember()

  const refetch = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: getListHouseholdMembersQueryKey() })
    await queryRefetch()
  }, [queryClient, queryRefetch])

  const removeMember = useCallback(
    async (userId: string): Promise<void> => {
      await removeMemberMutation.mutateAsync({ userId })
      // Removing/leaving changes the roster, the seat count, membership
      // status itself (getMyHousehold — critical for self-leave, see file
      // header), and the household-scoped dashboard/analytics aggregates.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: getListHouseholdMembersQueryKey() }),
        queryClient.invalidateQueries({ queryKey: getGetHouseholdEntitlementQueryKey() }),
        queryClient.invalidateQueries({ queryKey: getGetMyHouseholdQueryKey() }),
        invalidateScopedDashboardAnalyticsQueries(queryClient),
      ])
      await queryRefetch()
    },
    [removeMemberMutation, queryClient, queryRefetch]
  )

  return {
    members,
    isLoading,
    error,
    refetch,
    removeMember,
  }
}
