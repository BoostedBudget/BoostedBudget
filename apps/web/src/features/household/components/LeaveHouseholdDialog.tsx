/**
 * LeaveHouseholdDialog Component
 *
 * Issue #1370: Household feature — leave-household flow (Task 4a)
 *
 * A `ConfirmationDialog` (variant `warning` — leaving is a meaningful but
 * reversible action; the member can rejoin if re-invited, so it doesn't
 * warrant the `destructive` styling reserved for irreversible actions like
 * disbanding) for a non-owner member to leave the household via
 * `removeMember(ownUserId)` from `useHouseholdMembers`.
 *
 * Design note — own-user-id source: this component resolves its own user id
 * internally via `useProfile()` rather than accepting an `ownUserId` prop.
 * The Task 4 brief left this to judgment. Reading it internally keeps the
 * dialog fully self-contained for the composing HouseholdTab (Task 4b) —
 * render `<LeaveHouseholdDialog open={...} onOpenChange={...} />` with no
 * extra wiring — and mirrors DisbandHouseholdDialog's self-contained shape
 * (which reads its own eligibility signal from `useHousehold()`) for
 * consistency across the Task 4 components.
 *
 * `removeMember` (see useHouseholdMembers.ts) already invalidates the
 * members list + entitlement summary internally on success, so no
 * additional invalidation is performed here.
 */

import { useState } from 'react'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { useProfile } from '@/features/profile/hooks/useProfile'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { useHouseholdMembers } from '../hooks/useHouseholdMembers'

export interface LeaveHouseholdDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function LeaveHouseholdDialog({ open, onOpenChange }: LeaveHouseholdDialogProps) {
  const { user } = useProfile()
  const { removeMember } = useHouseholdMembers()
  const trackEvent = useTrackEvent()

  const [isLeaving, setIsLeaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleConfirm = async () => {
    if (!user?.id || isLeaving) return

    setError(null)
    setIsLeaving(true)
    try {
      await removeMember(user.id)
      // Fire only once the leave has actually persisted, never on failure.
      // This is the member's OWN leave action — distinct from
      // HOUSEHOLD_MEMBER_REMOVED, which MembersList fires when the owner
      // removes someone else via the same removeMember() call.
      trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_LEFT)
      onOpenChange(false)
    } catch {
      setError('Could not leave the household. Please try again.')
    } finally {
      setIsLeaving(false)
    }
  }

  return (
    <ConfirmationDialog
      open={open}
      onOpenChange={onOpenChange}
      onConfirm={handleConfirm}
      variant="warning"
      title="Leave household?"
      description={
        <>
          <span>
            You&apos;ll lose access to shared budgets and accounts. You can rejoin later if
            you&apos;re re-invited.
          </span>
          {error && (
            <span className="mt-2 block text-destructive" role="alert">
              {error}
            </span>
          )}
        </>
      }
      confirmButtonText="Leave household"
      loading={isLeaving}
    />
  )
}
