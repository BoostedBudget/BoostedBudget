/**
 * ConsentPreview Component
 *
 * Issue #1370: Household feature — invite-accept consent preview (Task 6)
 *
 * Renders the SIGNED-IN invitee's own accounts (`useActiveAccounts`) as a
 * list of share/hide toggles, ahead of accepting a household invite. Every
 * account defaults to shared (switch ON), matching the epic's shared-by-default
 * consent model — the invitee can opt out before joining.
 *
 * ⚠ ONE TOGGLE PER SHARING UNIT, NOT PER ACCOUNT (issue #1569 T10.3 item C).
 * The backend hides by `user_account_id`, and two maskless accounts at one
 * institution can share one — so a per-account choice does not exist. This
 * component used to render a card and a switch per ACCOUNT while binding every
 * switch to the shared id, which meant hiding the IRA also hid the Brokerage.
 * It failed CLOSED (it over-hid, never over-shared), but it offered a choice it
 * could not honour, and a false affordance on a consent surface is a defect
 * whichever way it fails. `groupAccountsBySharingUnit` below makes the control
 * match reality instead of faking a granularity the backend cannot express.
 *
 * State-lifting choice: this component owns the toggle state itself (a
 * `Set<string>` of hidden account ids) and reports the *complete* current
 * list of hidden ids to the parent via `onHiddenAccountIdsChange` each time
 * a toggle changes. The callback is invoked imperatively from the toggle
 * event handler itself — NOT from a `useEffect` watching the hidden-ids
 * state, and NOT from inside a `setState` updater function:
 *   - Avoiding `useEffect` sidesteps a render loop: an effect keyed on
 *     `[hiddenIds, onHiddenAccountIdsChange]` would re-fire whenever the
 *     parent re-renders with a new (unmemoized) callback identity, and if
 *     that callback itself causes a parent re-render (as `setState` does),
 *     a fresh function is produced each time, chaining indefinitely.
 *   - Avoiding a `setState` updater keeps the update pure: React (under
 *     StrictMode, see `main.tsx`) double-invokes updater functions in dev to
 *     surface impure updaters, which would double-fire this side effect.
 *     `toggleShared` instead derives `next` from the current render's
 *     `hiddenIds` directly and calls `setHiddenIds(next)` with a plain
 *     value.
 */

import { useState } from 'react'
import type { ReactNode } from 'react'
import type { Account } from '@boostedbudget/api-client/models'
import { Card } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { AccountSummaryHeader } from '@/features/accounts/components/AccountSummaryHeader'
import { getAccountTypeLabel } from '@/features/accounts/utils'
import { useActiveAccounts } from '@/features/accounts/hooks/useActiveAccounts'
import { HOUSEHOLD_SHARE_CONSENT_COPY } from '../householdShareConsent'
import { groupAccountsBySharingUnit, sharingUnitDisplayName } from '../sharingUnits'

export interface ConsentPreviewProps {
  /** Called with the full list of currently-hidden `user_account_id`s whenever a toggle changes. */
  onHiddenAccountIdsChange: (hiddenAccountIds: string[]) => void
}

export function ConsentPreview({ onHiddenAccountIdsChange }: ConsentPreviewProps) {
  const { accounts, isLoading } = useActiveAccounts()
  const [hiddenIds, setHiddenIds] = useState<Set<string>>(new Set())

  if (isLoading) {
    return <p className="text-sm text-muted-foreground">Loading your accounts…</p>
  }

  if (!accounts || accounts.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        You have no linked accounts yet — you can still join this household.
      </p>
    )
  }

  const toggleShared = (shareId: string, shared: boolean) => {
    // Built from the current render's `hiddenIds` (not a functional updater)
    // and the side-effecting callback is invoked here, as a plain event
    // handler — not from inside a `setState` updater function, which
    // StrictMode double-invokes in dev and which must stay pure.
    const next = new Set(hiddenIds)
    if (shared) {
      next.delete(shareId)
    } else {
      next.add(shareId)
    }
    setHiddenIds(next)
    onHiddenAccountIdsChange(Array.from(next))
  }

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">{HOUSEHOLD_SHARE_CONSENT_COPY.invitePreviewIntro}</p>
      {groupAccountsBySharingUnit(accounts).map((unit) => {
        // `user_account_id` is the id the backend's IDOR/subset check
        // (household_service.go's AcceptInvite validates hidden_account_ids
        // against ListActiveUserAccountFingerprints(callerUserID)) actually
        // recognizes. A legacy/edge-case account without one CANNOT be
        // referenced in `hidden_account_ids` — falling back to `account.id`
        // here would silently send the wrong id type and make the backend
        // reject the ENTIRE accept() call with ErrInvalidConsent. So such an
        // account is shown read-only (always shared, no togglable switch)
        // rather than risk that failure mode.
        const { shareId, accounts: unitAccounts } = unit
        // ⚠ #1569 T10-FIX item 8: `: true` was here, and it was a false statement on a consent
        // screen. AcceptInvite builds the shareable set from `user_accounts`
        // (household_service.go), so an accounts row with no user_account_id can never enter
        // `household_account_shares` and no household read will ever see it. The old default
        // rendered it "Shared" with the switch on. It under-shares, so it fails closed — and it
        // still tells the invitee the opposite of what will happen.
        const shareable = Boolean(shareId)
        const shared = shareId ? !hiddenIds.has(shareId) : false
        const displayNames = unitAccounts.map(sharingUnitDisplayName)
        // aria-label/title/placeholder/alt are recorded verbatim by PostHog
        // session replay regardless of visual masking (see
        // docs/security/posthog-pii-masking.md and InvitePanel.tsx's
        // PendingInviteRow, which uses the same aria-describedby pattern).
        // So the switch gets a generic aria-label, and is described by a
        // separate, already-`data-ph-mask`'d text node instead.
        const descriptionId = `consent-account-name-${unit.key}`

        const statusLabel = !shareable
          ? HOUSEHOLD_SHARE_CONSENT_COPY.unshareableStatus
          : shared
            ? 'Shared'
            : 'Hidden'

        const control = (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">{statusLabel}</span>
            <Switch
              checked={shared}
              disabled={!shareable}
              title={shareable ? undefined : HOUSEHOLD_SHARE_CONSENT_COPY.unshareableNote}
              onCheckedChange={(next) => {
                if (shareId) toggleShared(shareId, next)
              }}
              aria-label={
                unitAccounts.length > 1
                  ? 'Share these accounts with your household'
                  : 'Share this account with your household'
              }
              aria-describedby={descriptionId}
            />
          </div>
        )

        const summary = (account: Account, trailingSlot?: ReactNode) => (
          <AccountSummaryHeader
            key={account.id}
            institutionName={account.institution_name ?? ''}
            mask={account.mask}
            displayName={sharingUnitDisplayName(account)}
            typeLabel={getAccountTypeLabel(account.type, account.subtype)}
            currentBalance={account.current_balance}
            availableBalance={account.available_balance}
            currencyCode={account.currency_code}
            customEmoji={account.custom_emoji}
            customIcon={account.custom_icon}
            customColor={account.custom_color}
            institutionRowTrailingSlot={trailingSlot}
          />
        )

        return (
          <Card key={unit.key}>
            <span id={descriptionId} className="sr-only" data-ph-mask>
              Share {displayNames.join(', ')} with your household
            </span>
            {unitAccounts.length === 1 ? (
              <>
                {summary(unitAccounts[0], control)}
                {!shareable && (
                  <p className="border-t px-4 pt-2 text-xs text-muted-foreground">
                    {HOUSEHOLD_SHARE_CONSENT_COPY.unshareableNote}
                  </p>
                )}
              </>
            ) : (
              // One card, one switch, every account named. ⚠ #1569 T10-FIX item 9: this used to
              // say the institution reports these accounts under one identity. It does not — it
              // reports them separately, and OUR matching collapses them:
              // FindByInstitutionAndMask keys on institution plus mask, and migration 000100's
              // NULLS NOT DISTINCT unique index forces every maskless account at one institution
              // onto one user_accounts row. The backend hides by that row, so showing two switches
              // would offer a choice that does not exist.
              <div className="space-y-2">
                {unitAccounts.map((account) => summary(account))}
                <div className="flex items-center justify-between gap-2 border-t pt-2">
                  <span className="text-xs text-muted-foreground">
                    {HOUSEHOLD_SHARE_CONSENT_COPY.sharedUnitNote}
                  </span>
                  {control}
                </div>
              </div>
            )}
          </Card>
        )
      })}
    </div>
  )
}
