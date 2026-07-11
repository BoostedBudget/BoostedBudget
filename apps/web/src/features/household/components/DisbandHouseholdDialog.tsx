/**
 * DisbandHouseholdDialog Component
 *
 * Issue #1370: Household feature — disband-household flow (Task 4a)
 *
 * Lets the owner permanently disband the household via the generated
 * `useDeleteHousehold` mutation (called directly — no wrapping feature hook,
 * same rationale as CreateHouseholdForm/HouseholdCard's rename flow).
 *
 * Design note — disband-eligibility source: the backend only allows disband
 * when zero active non-owner members remain. This component derives that
 * signal from `useHousehold()`'s `seatsUsed`/`isOwner`/`isLoading`
 * (`seatsUsed <= 1` means only the owner occupies a seat) rather than
 * accepting a member-count prop from the composing HouseholdTab (Task 4b).
 * Hook-derived keeps this dialog self-contained — render
 * `<DisbandHouseholdDialog open={...} onOpenChange={...} />` with no extra
 * wiring — mirroring LeaveHouseholdDialog's self-contained shape
 * (own-user-id via `useProfile()`), and avoids the parent needing to
 * separately compute a count that `useHousehold` already exposes.
 *
 * Eligibility is deliberately conservative — `canDisband` requires ALL of:
 * `!isLoading` (seatsUsed/isOwner default to falsy values before the
 * entitlement query resolves, per useHousehold.ts; treating that as
 * "eligible" would let the destructive flow render before membership is
 * actually known), `isOwner` (defense-in-depth: this component doesn't rely
 * solely on the composing parent to gate rendering to owners only), and
 * `seatsUsed <= 1`.
 *
 * Design note — type-to-confirm phrase: the fixed literal "DISBAND", not the
 * household's own name. A fixed word sidesteps the household name's
 * arbitrary length/punctuation/casing (some names are awkward to type
 * exactly) and keeps PII (the household name, which can contain a family
 * surname) out of the confirm phrase entirely.
 *
 * Design note — guard UX: when disband isn't eligible (still loading, not
 * the owner, or other active members exist), this renders a non-destructive
 * `info`-variant `ConfirmationDialog` explaining why; its "confirm" action
 * is just dismissal (`onOpenChange(false)`) and never touches
 * `useDeleteHousehold`. This satisfies the brief's "guarded... rather than
 * letting the user attempt it and only discover the error after the fact"
 * by making the destructive flow (and its type-to-confirm gate) entirely
 * unreachable in that state, rather than merely disabling a control within
 * it.
 */

import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useDeleteHousehold,
  getGetMyHouseholdQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { useHousehold } from '../hooks/useHousehold'
import { getErrorDetail } from '../hooks/errorStatus'

const CONFIRM_TEXT = 'DISBAND'

/** Explains why disband isn't available yet, or null when it's eligible. */
function getGuardMessage(isLoading: boolean, isOwner: boolean, seatsUsed: number): string | null {
  if (isLoading) {
    return "We're still loading your household's membership. Please wait a moment and try again."
  }
  if (!isOwner) {
    return 'Only the household owner can disband the household.'
  }
  if (seatsUsed > 1) {
    return 'This household still has other active members. Remove everyone except yourself before you can disband it.'
  }
  return null
}

export interface DisbandHouseholdDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function DisbandHouseholdDialog({ open, onOpenChange }: DisbandHouseholdDialogProps) {
  const queryClient = useQueryClient()
  const { seatsUsed, isOwner, isLoading } = useHousehold()
  const deleteHouseholdMutation = useDeleteHousehold()
  const trackEvent = useTrackEvent()

  const [isDisbanding, setIsDisbanding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const guardMessage = getGuardMessage(isLoading, isOwner, seatsUsed)
  const canDisband = guardMessage === null

  const handleConfirm = async () => {
    if (!canDisband || isDisbanding) return

    setError(null)
    setIsDisbanding(true)
    try {
      await deleteHouseholdMutation.mutateAsync()
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: getGetMyHouseholdQueryKey() }),
        queryClient.invalidateQueries({ queryKey: getGetHouseholdEntitlementQueryKey() }),
      ])
      // Fire only once the disband has actually persisted, never on failure.
      trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_DISBANDED)
      onOpenChange(false)
    } catch (err) {
      setError(getErrorDetail(err) ?? 'Could not disband the household. Please try again.')
    } finally {
      setIsDisbanding(false)
    }
  }

  if (guardMessage !== null) {
    return (
      <ConfirmationDialog
        open={open}
        onOpenChange={onOpenChange}
        onConfirm={() => onOpenChange(false)}
        variant="info"
        title="Cannot disband household"
        description={guardMessage}
        confirmButtonText="Got it"
        cancelButtonText="Close"
      />
    )
  }

  return (
    <ConfirmationDialog
      open={open}
      onOpenChange={onOpenChange}
      onConfirm={handleConfirm}
      variant="destructive"
      title="Disband household?"
      description={
        <>
          <span>
            This permanently deletes the household. Shared budgets and account access for all
            members will end. This cannot be undone.
          </span>
          {error && (
            <span className="mt-2 block text-destructive" role="alert">
              {error}
            </span>
          )}
        </>
      }
      confirmButtonText="Disband household"
      confirmText={CONFIRM_TEXT}
      loading={isDisbanding}
    />
  )
}
