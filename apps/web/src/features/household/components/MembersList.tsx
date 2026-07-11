/**
 * MembersList Component
 *
 * Issue #1370: Household feature — members roster (Task 4b)
 *
 * `DataTable<HouseholdMemberView>` showing the household roster: identity
 * (avatar + display_name), role, and joined date.
 *
 * Design note — owner-only Remove, self-row excluded: `rowActions` is only
 * passed when the viewer is the owner (`isOwner` from `useHousehold()`), and
 * within that, the Remove button is withheld on the row whose `role` is
 * `'owner'`. A household has exactly one owner, and only the owner ever sees
 * `rowActions` at all — so "the row with role owner" and "the current
 * viewer's own row" are the same row whenever Remove is visible at all. This
 * avoids a second dependency on `useProfile()` just to compare ids (unlike
 * LeaveHouseholdDialog, which genuinely needs its own user id because ANY
 * member, not just the owner, can open that dialog).
 *
 * Design note — remove confirmation: reuses the existing `ConfirmationDialog`
 * primitive (variant `warning`, one shared instance tracking which row is
 * pending removal) rather than a bare `window.confirm()`. Same rationale as
 * `LeaveHouseholdDialog` — removing a member is a meaningful but reversible
 * action (the member can be re-invited), so `warning` (not `destructive`) is
 * the right variant, and reusing the shared primitive keeps this consistent
 * with the rest of the household feature's confirm flows.
 *
 * Design note — PII masking: only the identity column (avatar + name)
 * carries PII, so only `member` is in `maskedColumnIds` per this project's
 * PostHog PII masking policy. `role`/`joined_at` are not personally
 * identifying on their own.
 *
 * Design note — joined_at formatting: unlike InvitePanel's `formatExpiresAt`
 * (which pins `timeZone: 'UTC'` because `expires_at` is a UTC-midnight
 * *cutoff*), `joined_at` is a real event timestamp (any time of day), so it's
 * formatted in the viewer's local timezone — matching the plain `formatDate`
 * helper already used in `admin/components/UsersPage.tsx`.
 *
 * Design note — unknown `role` values: `HouseholdMemberView.role` is typed as
 * a plain `string` in the generated model (not a closed union), so the Remove
 * eligibility check below is written as "removable unless `role === 'owner'`"
 * rather than "removable only if `role === 'member'`". This is the
 * conservative direction for defense-in-depth: if the backend ever sends an
 * unexpected role value, it stays safely removable (never accidentally
 * treated as the un-removable owner row). `RoleBadge`'s own label fallback
 * for non-'owner' values is that component's concern (Task 1, already
 * reviewed/approved) and out of scope here.
 *
 * Design note — per-row Remove button naming: every row's Remove button
 * shares the same accessible name ("Remove"), which is ambiguous for
 * screen-reader users navigating by role across multiple rows. Each row's
 * rendered name gets a stable id, and the Remove button references it via
 * `aria-describedby` (never `aria-label`, which PostHog session replay
 * records verbatim regardless of visual masking — see
 * docs/security/posthog-pii-masking.md) — the same pattern InvitePanel
 * already uses for its per-row Revoke/Regenerate buttons.
 */

import { useState } from 'react'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { DataTable } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { RoleBadge } from './RoleBadge'
import { useHousehold } from '../hooks/useHousehold'
import { useHouseholdMembers } from '../hooks/useHouseholdMembers'
import type { HouseholdMemberView } from '../types'

export interface MembersListProps {
  className?: string
}

const IDENTITY_COLUMN_ID = 'member'

/** Initials for the avatar fallback, derived from a (possibly absent) display name. */
function getInitials(displayName?: string): string {
  const trimmed = displayName?.trim()
  if (!trimmed) return '?'
  return trimmed
    .split(/\s+/)
    .map((part) => part[0])
    .join('')
    .toUpperCase()
    .slice(0, 2)
}

/** Formats an ISO timestamp as a short human-readable date in the viewer's local timezone. */
function formatJoinedAt(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

export function MembersList({ className }: MembersListProps = {}) {
  const { members, isLoading, removeMember } = useHouseholdMembers()
  const { isOwner } = useHousehold()
  const trackEvent = useTrackEvent()

  const [pendingRemoval, setPendingRemoval] = useState<HouseholdMemberView | null>(null)
  const [isRemoving, setIsRemoving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleDialogOpenChange = (open: boolean) => {
    if (!open) {
      setPendingRemoval(null)
      setError(null)
    }
  }

  const handleConfirmRemove = async () => {
    if (!pendingRemoval || isRemoving) return

    setError(null)
    setIsRemoving(true)
    try {
      await removeMember(pendingRemoval.user_id)
      // Fire only once the removal has actually persisted, never on failure.
      // This is the OWNER removing someone else — distinct from
      // HOUSEHOLD_LEFT, which LeaveHouseholdDialog fires for a member's own
      // leave action via the same removeMember() call.
      trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_MEMBER_REMOVED)
      setPendingRemoval(null)
    } catch {
      setError('Could not remove this member. Please try again.')
    } finally {
      setIsRemoving(false)
    }
  }

  const columns: ColumnDef<HouseholdMemberView>[] = [
    {
      id: IDENTITY_COLUMN_ID,
      header: 'Name',
      render: (member) => (
        <div className="flex items-center gap-2">
          <Avatar className="size-8">
            {/* Decorative: the name is already rendered as adjacent visible
                (and masked) text, so alt is empty rather than duplicating
                the display name into an `alt` attribute — PostHog session
                replay records alt/aria-label/title/placeholder verbatim
                regardless of visual masking. */}
            <AvatarImage src={member.avatar_url} alt="" />
            <AvatarFallback>{getInitials(member.display_name)}</AvatarFallback>
          </Avatar>
          <span id={`member-name-${member.user_id}`} className="font-medium">
            {member.display_name ?? 'Household member'}
          </span>
        </div>
      ),
    },
    {
      id: 'role',
      header: 'Role',
      render: (member) => <RoleBadge role={member.role === 'owner' ? 'owner' : 'member'} />,
    },
    {
      id: 'joined_at',
      header: 'Joined',
      render: (member) => formatJoinedAt(member.joined_at),
    },
  ]

  return (
    <div className={className}>
      <DataTable<HouseholdMemberView>
        data={members}
        columns={columns}
        rowKey={(member) => member.user_id}
        sort={null}
        onSortChange={() => {}}
        filters={{}}
        onFiltersChange={() => {}}
        loading={isLoading}
        maskedColumnIds={[IDENTITY_COLUMN_ID]}
        emptyState={<p className="text-sm text-muted-foreground">No members yet.</p>}
        rowActions={
          isOwner
            ? (member) =>
                member.role === 'owner' ? null : (
                  <Button
                    type="button"
                    variant="destructive"
                    size="sm"
                    aria-describedby={`member-name-${member.user_id}`}
                    onClick={() => {
                      setError(null)
                      setPendingRemoval(member)
                    }}
                  >
                    Remove
                  </Button>
                )
            : undefined
        }
      />

      {isOwner && (
        <ConfirmationDialog
          open={pendingRemoval !== null}
          onOpenChange={handleDialogOpenChange}
          onConfirm={handleConfirmRemove}
          variant="warning"
          title="Remove this member?"
          description={
            <>
              <span data-ph-mask>
                {(pendingRemoval?.display_name ?? 'This member') +
                  ' will lose access to shared budgets and accounts. They can be re-invited later.'}
              </span>
              {error && (
                <span className="mt-2 block text-destructive" role="alert">
                  {error}
                </span>
              )}
            </>
          }
          confirmButtonText="Remove member"
          loading={isRemoving}
        />
      )}
    </div>
  )
}
