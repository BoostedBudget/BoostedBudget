/**
 * useHouseholdMembers Hook
 *
 * Issue #1370: Household feature — hooks layer
 *
 * Thin wrap of `useListHouseholdMembers`, plus a `removeMember` action backed by
 * `useRemoveHouseholdMember`. Removing (or self-leaving) a household changes the
 * seat count, so both the members list and the entitlement summary are
 * invalidated on success.
 */

import { useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useListHouseholdMembers,
  useRemoveHouseholdMember,
  getListHouseholdMembersQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import type { HouseholdMemberView } from '../types'

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
      // Removing/leaving changes both the roster and the seat count.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: getListHouseholdMembersQueryKey() }),
        queryClient.invalidateQueries({ queryKey: getGetHouseholdEntitlementQueryKey() }),
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
