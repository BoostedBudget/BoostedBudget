/**
 * useHouseholdInvites Hook
 *
 * Issue #1370: Household feature — hooks layer
 *
 * Wraps `useListHouseholdInvites`, `useCreateHouseholdInvite`, and
 * `useRevokeHouseholdInvite`. Also exposes `regenerateInvite`, which revokes an
 * existing pending invite and creates a fresh one for the same email —
 * sequentially (revoke awaited before create), so a regenerate never leaves
 * two live invites outstanding for the same email even momentarily.
 *
 * Backend error mapping (see backend/internal/huma/errors/mapper.go): both the
 * duplicate-pending-invite case and the seats-full case return HTTP 409 with a
 * human-readable `detail` ("A pending invite already exists for this email." /
 * "No seats available on this plan."). Since the backend's `detail` text is
 * already the friendly, specific message, we surface it directly for 409s
 * rather than re-deriving it from an error code.
 */

import { useCallback, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useListHouseholdInvites,
  useCreateHouseholdInvite,
  useRevokeHouseholdInvite,
  getListHouseholdInvitesQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import type { HouseholdInvite, CreateInviteResponseBody } from '@boostedbudget/api-client/models'

export interface UseHouseholdInvitesResult {
  invites: HouseholdInvite[]
  isLoading: boolean
  /** Friendly message from the most recent list/create/revoke/regenerate failure, or null. */
  error: string | null
  refetch: () => Promise<void>
  createInvite: (email: string) => Promise<CreateInviteResponseBody>
  revokeInvite: (inviteId: string) => Promise<void>
  regenerateInvite: (inviteId: string, email: string) => Promise<CreateInviteResponseBody>
}

function getErrorStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined

  if ('response' in error) {
    const response = (error as { response?: { status?: number } }).response
    if (response?.status !== undefined) return response.status
  }

  if ('status' in error) {
    return (error as { status?: number }).status
  }

  return undefined
}

function getErrorDetail(error: unknown): string | undefined {
  if (!error || typeof error !== 'object') return undefined

  if ('response' in error) {
    const response = (error as { response?: { data?: { detail?: string } } }).response
    if (response?.data?.detail) return response.data.detail
  }

  return undefined
}

/** Maps a raw mutation/query error into a user-facing string. */
function toFriendlyMessage(error: unknown): string {
  const status = getErrorStatus(error)
  const detail = getErrorDetail(error)

  // 409s (duplicate-pending-invite, seats-full) already carry a specific,
  // human-readable `detail` from the backend — surface it as-is.
  if (status === 409 && detail) return detail
  if (detail) return detail

  return 'Something went wrong. Please try again.'
}

export function useHouseholdInvites(): UseHouseholdInvitesResult {
  const queryClient = useQueryClient()
  const [actionError, setActionError] = useState<string | null>(null)

  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useListHouseholdInvites()

  const invites = (data ?? []) as unknown as HouseholdInvite[]

  const createInviteMutation = useCreateHouseholdInvite()
  const revokeInviteMutation = useRevokeHouseholdInvite()

  const invalidateInviteQueries = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: getListHouseholdInvitesQueryKey() }),
      queryClient.invalidateQueries({ queryKey: getGetHouseholdEntitlementQueryKey() }),
    ])
  }, [queryClient])

  const refetch = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: getListHouseholdInvitesQueryKey() })
    await queryRefetch()
  }, [queryClient, queryRefetch])

  const createInvite = useCallback(
    async (email: string): Promise<CreateInviteResponseBody> => {
      setActionError(null)
      try {
        const result = (await createInviteMutation.mutateAsync({
          data: { email },
        })) as unknown as CreateInviteResponseBody
        await invalidateInviteQueries()
        await queryRefetch()
        return result
      } catch (err) {
        setActionError(toFriendlyMessage(err))
        throw err
      }
    },
    [createInviteMutation, invalidateInviteQueries, queryRefetch]
  )

  const revokeInvite = useCallback(
    async (inviteId: string): Promise<void> => {
      setActionError(null)
      try {
        await revokeInviteMutation.mutateAsync({ id: inviteId })
        await invalidateInviteQueries()
        await queryRefetch()
      } catch (err) {
        setActionError(toFriendlyMessage(err))
        throw err
      }
    },
    [revokeInviteMutation, invalidateInviteQueries, queryRefetch]
  )

  const regenerateInvite = useCallback(
    async (inviteId: string, email: string): Promise<CreateInviteResponseBody> => {
      setActionError(null)
      try {
        // Sequential by design: revoke must fully complete before create runs,
        // so the same email never has two live invites outstanding at once.
        await revokeInviteMutation.mutateAsync({ id: inviteId })
        const result = (await createInviteMutation.mutateAsync({
          data: { email },
        })) as unknown as CreateInviteResponseBody
        await invalidateInviteQueries()
        await queryRefetch()
        return result
      } catch (err) {
        setActionError(toFriendlyMessage(err))
        throw err
      }
    },
    [revokeInviteMutation, createInviteMutation, invalidateInviteQueries, queryRefetch]
  )

  const error = actionError ?? (queryError ? toFriendlyMessage(queryError) : null)

  return {
    invites,
    isLoading,
    error,
    refetch,
    createInvite,
    revokeInvite,
    regenerateInvite,
  }
}
