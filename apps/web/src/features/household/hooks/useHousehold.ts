/**
 * useHousehold Hook
 *
 * Issue #1370: Household feature — hooks layer
 *
 * Wraps `useGetMyHousehold` + `useGetHouseholdEntitlement`. `useGetMyHousehold`
 * 404s when the caller doesn't belong to a household yet — that's an expected,
 * common state, not an error, so it's mapped to `isInHousehold: false` rather
 * than surfaced via `error`. `useGetHouseholdEntitlement` is independent of
 * membership (a solo Premium user still has an entitlement summary), so seat
 * counts and ownership are always available once it loads.
 */

import { useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useGetMyHousehold,
  useGetHouseholdEntitlement,
  getGetMyHouseholdQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import type { Household, HouseholdEntitlementSummary } from '../types'
import { getErrorStatus } from './errorStatus'

export interface UseHouseholdResult {
  household: Household | null
  summary: HouseholdEntitlementSummary | null
  isInHousehold: boolean
  isOwner: boolean
  seatsUsed: number
  seatsTotal: number
  /**
   * ISO timestamp of when the household first went over its plan's seat
   * limit (mapped from `Household.over_capacity_since`). `undefined` when
   * the household is within its seat limit, or when there's no household
   * to report on yet.
   */
  overCapacitySince: string | undefined
  isLoading: boolean
  error: Error | null
  refetch: () => Promise<void>
}

/**
 * Check if error is a 404 error (caller has no household yet).
 * Mirrors the pattern in `features/budgets/hooks/useCurrentBudget.ts`.
 */
function is404Error(error: unknown): boolean {
  return getErrorStatus(error) === 404
}

export function useHousehold(): UseHouseholdResult {
  const queryClient = useQueryClient()

  const {
    data: householdData,
    isLoading: isHouseholdLoading,
    error: householdQueryError,
    refetch: refetchHousehold,
  } = useGetMyHousehold({
    query: {
      staleTime: 30 * 1000,
      retry: (failureCount, error) => {
        if (is404Error(error)) return false
        return failureCount < 2
      },
    },
  })

  const {
    data: entitlementData,
    isLoading: isEntitlementLoading,
    error: entitlementQueryError,
    refetch: refetchEntitlement,
  } = useGetHouseholdEntitlement({
    query: {
      staleTime: 30 * 1000,
    },
  })

  const hasNoHousehold = is404Error(householdQueryError)
  const isInHousehold = !hasNoHousehold && !!householdData

  const household: Household | null = householdData ? (householdData.household as unknown as Household) : null
  const summary: HouseholdEntitlementSummary | null = entitlementData
    ? (entitlementData as unknown as HouseholdEntitlementSummary)
    : null

  const seatsUsed = summary?.seats_active ?? 0
  const seatsTotal = summary?.users_included ?? 0
  const isOwner = summary?.is_owner ?? false
  const overCapacitySince = household?.over_capacity_since ?? undefined

  // The expected 404 (no household) is not surfaced as an error; any other
  // failure on either query is.
  const householdError = householdQueryError && !hasNoHousehold ? householdQueryError : null
  const rawError = householdError ?? entitlementQueryError ?? null
  const error = rawError
    ? rawError instanceof Error
      ? rawError
      : new Error('Failed to fetch household')
    : null

  const isLoading = isHouseholdLoading || isEntitlementLoading

  const refetch = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: getGetMyHouseholdQueryKey() }),
      queryClient.invalidateQueries({ queryKey: getGetHouseholdEntitlementQueryKey() }),
    ])
    await Promise.all([refetchHousehold(), refetchEntitlement()])
  }, [queryClient, refetchHousehold, refetchEntitlement])

  return {
    household,
    summary,
    isInHousehold,
    isOwner,
    seatsUsed,
    seatsTotal,
    overCapacitySince,
    isLoading,
    error,
    refetch,
  }
}
