/**
 * HouseholdTab Component
 *
 * Issue #1370: Household feature — composing tab (Task 4b)
 *
 * Top-level component for the (future) 'household' tab on ProfilePage.
 * Mounting this into ProfilePage itself is a separate task (Task 3, not this
 * one) — this component is self-contained and ready to be dropped in as-is.
 *
 * Branching, driven entirely by `useHousehold()`:
 * - `isLoading` → simple loading message.
 * - `error` (any failure other than the expected "no household yet" 404,
 *   which `useHousehold()` already maps to `isInHousehold: false` rather than
 *   `error` — see useHousehold.ts) → an error message + Retry button, checked
 *   BEFORE the `isInHousehold` branch. Without this check, a transient
 *   entitlement-query failure could fall through into `!isInHousehold` (never
 *   true here) or into rendering `HouseholdCard`/`MembersList` with
 *   `isOwner`/`seatsUsed`/`seatsTotal` silently defaulted to falsy/zero
 *   values, misrendering an owner as a plain member. Caught in review —
 *   thanks, Codex.
 * - `!isInHousehold` → `CreateHouseholdForm`.
 * - `isInHousehold` → `HouseholdCard` + `MembersList`, plus:
 *   - owner with available seats (`seatsUsed < seatsTotal`) → `InvitePanel`.
 *     (Also correctly hidden when seats are over-limit, `seatsUsed >
 *     seatsTotal` — `<` is false either way.)
 *   - owner → a "Disband household" button opening `DisbandHouseholdDialog`.
 *   - non-owner → a "Leave household" button opening `LeaveHouseholdDialog`.
 *
 * Design note — gating-boundary judgment call: per the Task 4 brief,
 * `CreateHouseholdForm` is rendered here unconditionally whenever
 * `!isInHousehold`, with NO re-check of `seatsTotal > 1` / tier eligibility.
 * The brief explicitly assigns that eligibility gate (is this caller even a
 * Premium+/household-tier subscriber?) to the composing ProfilePage task —
 * this component assumes it is only ever mounted for callers who have
 * already passed that check, mirroring how `InvitePanel`'s own doc comment
 * assumes its owner/seats-available gating is the caller's responsibility.
 */

import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { CreateHouseholdForm } from './components/CreateHouseholdForm'
import { HouseholdCard } from './components/HouseholdCard'
import { MembersList } from './components/MembersList'
import { InvitePanel } from './components/InvitePanel'
import { LeaveHouseholdDialog } from './components/LeaveHouseholdDialog'
import { DisbandHouseholdDialog } from './components/DisbandHouseholdDialog'
import { useHousehold } from './hooks/useHousehold'

export interface HouseholdTabProps {
  className?: string
}

export function HouseholdTab({ className }: HouseholdTabProps = {}) {
  const { isLoading, isInHousehold, isOwner, seatsUsed, seatsTotal, error, refetch } = useHousehold()

  const [isLeaveDialogOpen, setIsLeaveDialogOpen] = useState(false)
  const [isDisbandDialogOpen, setIsDisbandDialogOpen] = useState(false)

  if (isLoading) {
    return <p className="text-sm text-muted-foreground">Loading household…</p>
  }

  if (error) {
    return (
      <div className="space-y-3 py-8 text-center">
        <p className="text-destructive">{error.message || 'Failed to load household. Please try again.'}</p>
        <Button type="button" variant="outline" onClick={() => refetch()}>
          Retry
        </Button>
      </div>
    )
  }

  if (!isInHousehold) {
    return <CreateHouseholdForm className={className} />
  }

  const hasAvailableSeats = seatsUsed < seatsTotal

  return (
    <div className={className ? `space-y-6 ${className}` : 'space-y-6'}>
      <HouseholdCard />
      <MembersList />

      {isOwner && hasAvailableSeats && <InvitePanel />}

      {isOwner ? (
        <>
          <Button type="button" variant="destructive" onClick={() => setIsDisbandDialogOpen(true)}>
            Disband household
          </Button>
          <DisbandHouseholdDialog open={isDisbandDialogOpen} onOpenChange={setIsDisbandDialogOpen} />
        </>
      ) : (
        <>
          <Button type="button" variant="outline" onClick={() => setIsLeaveDialogOpen(true)}>
            Leave household
          </Button>
          <LeaveHouseholdDialog open={isLeaveDialogOpen} onOpenChange={setIsLeaveDialogOpen} />
        </>
      )}
    </div>
  )
}
