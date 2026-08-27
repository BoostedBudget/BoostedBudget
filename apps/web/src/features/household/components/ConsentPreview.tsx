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

export interface ConsentPreviewProps {
  /** Called with the full list of currently-hidden `user_account_id`s whenever a toggle changes. */
  onHiddenAccountIdsChange: (hiddenAccountIds: string[]) => void
}

/** The name the invitee recognises this account by, in the order the rest of the app uses. */
function displayNameOf(account: Account): string {
  return account.nickname || account.official_name || account.name
}

/**
 * One sharing UNIT: the accounts a single `user_account_id` covers, plus that id.
 *
 * Issue #1569 T10.3 item C. `user_account_id` is the chain/fingerprint key, and two MASKLESS
 * accounts at one institution really can share it — the collision is documented at length in
 * backend/internal/repository/account_liveness_sql.go, which refuses to collapse portfolios on it
 * precisely because it is real. The backend hides by that id, so the unit the invitee can actually
 * make a decision about is the user_account, never the account.
 */
interface ConsentUnit {
  key: string
  /** `undefined` when the account has no `user_account_id` — see the read-only note below. */
  shareId?: string
  accounts: Account[]
}

/**
 * Groups accounts into the units the consent control can actually address.
 *
 * Accounts with NO `user_account_id` are each their own unit and are never grouped with one
 * another: they share the ABSENCE of a sharing id, not a shared one, and merging them would fold
 * unrelated accounts into a single read-only card. Order follows first appearance, so the list the
 * invitee sees keeps the account order the API returned.
 */
function groupAccountsBySharingUnit(accounts: Account[]): ConsentUnit[] {
  const units: ConsentUnit[] = []
  const byShareId = new Map<string, ConsentUnit>()

  for (const account of accounts) {
    const shareId = account.user_account_id || undefined
    if (!shareId) {
      units.push({ key: `account-${account.id}`, accounts: [account] })
      continue
    }
    const existing = byShareId.get(shareId)
    if (existing) {
      existing.accounts.push(account)
      continue
    }
    const unit: ConsentUnit = { key: `share-${shareId}`, shareId, accounts: [account] }
    byShareId.set(shareId, unit)
    units.push(unit)
  }

  return units
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
        const shared = shareId ? !hiddenIds.has(shareId) : true
        const displayNames = unitAccounts.map(displayNameOf)
        // aria-label/title/placeholder/alt are recorded verbatim by PostHog
        // session replay regardless of visual masking (see
        // docs/security/posthog-pii-masking.md and InvitePanel.tsx's
        // PendingInviteRow, which uses the same aria-describedby pattern).
        // So the switch gets a generic aria-label, and is described by a
        // separate, already-`data-ph-mask`'d text node instead.
        const descriptionId = `consent-account-name-${unit.key}`

        const control = (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">{shared ? 'Shared' : 'Hidden'}</span>
            <Switch
              checked={shared}
              disabled={!shareId}
              title={shareId ? undefined : 'Sharing preference unavailable for this account'}
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
            displayName={displayNameOf(account)}
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
              summary(unitAccounts[0], control)
            ) : (
              // One card, one switch, every account named. The institution reports these accounts
              // under one identity we cannot tell apart, so the backend hides them together —
              // showing two switches would offer a choice that does not exist.
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
