/**
 * useAcceptInvite Hook
 *
 * Issue #1370: Household feature — hooks layer
 *
 * Wraps `useGetHouseholdInvitePreview(token)`, `useAcceptHouseholdInvite`, and
 * `useDeclineHouseholdInvite`. Exposes a discriminated `errorKind` covering
 * BOTH the preview query's error and the accept/decline mutations' errors, so
 * a consuming component can render one of four states without re-deriving
 * status-code logic itself:
 *   - 404 → 'not_found'    (invalid/expired/unknown token)
 *   - 403 → 'email_mismatch' (signed-in user's email != invite's invitee_email)
 *   - 409 → 'seats_full'   (ErrHouseholdSeatsFull on accept)
 *   - anything else        → 'unknown'
 *
 * On a successful accept, invalidates `getMyHousehold`, `listHouseholdMembers`,
 * `getHouseholdEntitlement`, `listMyAccountShares`, the user's profile
 * (effective tier changes on joining a household), the accounts list
 * (account visibility can change), and the 9 scoped dashboard/analytics
 * queries (both `personal` and `household` scope variants) — joining a
 * household changes almost everything the user sees. See
 * `invalidateScopedQueries.ts` for the full query-key-shape investigation
 * behind the shared scoped-invalidation helper (also used by
 * useHouseholdVisibility.ts).
 */

import { useCallback, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useGetHouseholdInvitePreview,
  useAcceptHouseholdInvite,
  useDeclineHouseholdInvite,
  getGetMyHouseholdQueryKey,
  getListHouseholdMembersQueryKey,
  getGetHouseholdEntitlementQueryKey,
  getListMyAccountSharesQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import { getGetUserProfileQueryKey } from '@boostedbudget/api-client/endpoints/profile/profile'
import { getGetAccountsQueryKey } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import type { GetInvitePreviewResponseBody, AcceptInviteResponseBody } from '@boostedbudget/api-client/models'
import { invalidateScopedDashboardAnalyticsQueries } from './invalidateScopedQueries'
import { getErrorStatus } from './errorStatus'

export type AcceptInviteErrorKind = 'not_found' | 'email_mismatch' | 'seats_full' | 'unknown' | null

export interface UseAcceptInviteResult {
  preview: GetInvitePreviewResponseBody | null
  isPreviewLoading: boolean
  /** Discriminated error from whichever of preview/accept/decline most recently failed. */
  errorKind: AcceptInviteErrorKind
  accept: (hiddenAccountIds?: string[]) => Promise<AcceptInviteResponseBody>
  decline: () => Promise<void>
  refetchPreview: () => Promise<void>
}

function classifyErrorKind(error: unknown): AcceptInviteErrorKind {
  if (!error) return null

  switch (getErrorStatus(error)) {
    case 404:
      return 'not_found'
    case 403:
      return 'email_mismatch'
    case 409:
      return 'seats_full'
    default:
      return 'unknown'
  }
}

export function useAcceptInvite(token: string): UseAcceptInviteResult {
  const queryClient = useQueryClient()
  const [mutationErrorKind, setMutationErrorKind] = useState<AcceptInviteErrorKind>(null)

  const {
    data: previewData,
    isLoading: isPreviewLoading,
    error: previewQueryError,
    refetch: refetchPreviewQuery,
  } = useGetHouseholdInvitePreview(token, {
    query: {
      enabled: !!token,
      // Terminal 4xx states (bad token, wrong email, seats full) shouldn't
      // retry — they won't succeed on a retry.
      retry: (failureCount, error) => {
        const status = getErrorStatus(error)
        if (status === 404 || status === 403 || status === 409) return false
        return failureCount < 2
      },
    },
  })

  const preview: GetInvitePreviewResponseBody | null = previewData
    ? (previewData as unknown as GetInvitePreviewResponseBody)
    : null

  const acceptMutation = useAcceptHouseholdInvite()
  const declineMutation = useDeclineHouseholdInvite()

  const refetchPreview = useCallback(async () => {
    await refetchPreviewQuery()
  }, [refetchPreviewQuery])

  const accept = useCallback(
    async (hiddenAccountIds?: string[]): Promise<AcceptInviteResponseBody> => {
      setMutationErrorKind(null)
      try {
        const result = (await acceptMutation.mutateAsync({
          token,
          data: { consent: true, hidden_account_ids: hiddenAccountIds ?? null },
        })) as unknown as AcceptInviteResponseBody

        // Joining a household changes almost everything the user sees.
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: getGetMyHouseholdQueryKey() }),
          queryClient.invalidateQueries({ queryKey: getListHouseholdMembersQueryKey() }),
          queryClient.invalidateQueries({ queryKey: getGetHouseholdEntitlementQueryKey() }),
          queryClient.invalidateQueries({ queryKey: getListMyAccountSharesQueryKey() }),
          queryClient.invalidateQueries({ queryKey: getGetUserProfileQueryKey() }),
          queryClient.invalidateQueries({ queryKey: getGetAccountsQueryKey() }),
          invalidateScopedDashboardAnalyticsQueries(queryClient),
        ])

        return result
      } catch (err) {
        setMutationErrorKind(classifyErrorKind(err))
        throw err
      }
    },
    [acceptMutation, token, queryClient]
  )

  const decline = useCallback(async (): Promise<void> => {
    setMutationErrorKind(null)
    try {
      await declineMutation.mutateAsync({ token })
    } catch (err) {
      setMutationErrorKind(classifyErrorKind(err))
      throw err
    }
  }, [declineMutation, token])

  const errorKind = mutationErrorKind ?? classifyErrorKind(previewQueryError)

  return {
    preview,
    isPreviewLoading,
    errorKind,
    accept,
    decline,
    refetchPreview,
  }
}
