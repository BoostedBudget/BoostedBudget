/**
 * ConsentPreview Component
 *
 * Issue #1370: Household feature — invite-accept consent preview (Task 6)
 *
 * Renders the SIGNED-IN invitee's own accounts (`useActiveAccounts`) as a
 * list of per-account share/hide toggles, ahead of accepting a household
 * invite. Every account defaults to shared (switch ON), matching the epic's
 * shared-by-default consent model — the invitee can opt individual accounts
 * out before joining.
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
import { Card } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { AccountSummaryHeader } from '@/features/accounts/components/AccountSummaryHeader'
import { getAccountTypeLabel } from '@/features/accounts/utils'
import { useActiveAccounts } from '@/features/accounts/hooks/useActiveAccounts'

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
      <p className="text-sm text-muted-foreground">
        Choose which of your accounts to share with this household. All accounts are shared by
        default — you can change this any time after joining.
      </p>
      {accounts.map((account) => {
        // `user_account_id` is the id the backend's IDOR/subset check
        // (household_service.go's AcceptInvite validates hidden_account_ids
        // against ListActiveUserAccountFingerprints(callerUserID)) actually
        // recognizes. A legacy/edge-case account without one CANNOT be
        // referenced in `hidden_account_ids` — falling back to `account.id`
        // here would silently send the wrong id type and make the backend
        // reject the ENTIRE accept() call with ErrInvalidConsent. So such an
        // account is shown read-only (always shared, no togglable switch)
        // rather than risk that failure mode.
        const shareId = account.user_account_id
        const shared = shareId ? !hiddenIds.has(shareId) : true
        const displayName = account.nickname || account.official_name || account.name
        // aria-label/title/placeholder/alt are recorded verbatim by PostHog
        // session replay regardless of visual masking (see
        // docs/security/posthog-pii-masking.md and InvitePanel.tsx's
        // PendingInviteRow, which uses the same aria-describedby pattern).
        // So the switch gets a generic aria-label, and is described by a
        // separate, already-`data-ph-mask`'d text node instead.
        const descriptionId = `consent-account-name-${account.id}`

        return (
          <Card key={account.id}>
            <span id={descriptionId} className="sr-only" data-ph-mask>
              Share {displayName} with your household
            </span>
            <AccountSummaryHeader
              institutionName={account.institution_name ?? ''}
              mask={account.mask}
              displayName={displayName}
              typeLabel={getAccountTypeLabel(account.type, account.subtype)}
              currentBalance={account.current_balance}
              availableBalance={account.available_balance}
              currencyCode={account.currency_code}
              customEmoji={account.custom_emoji}
              customIcon={account.custom_icon}
              customColor={account.custom_color}
              institutionRowTrailingSlot={
                <div className="flex items-center gap-2">
                  <span className="text-xs text-muted-foreground">{shared ? 'Shared' : 'Hidden'}</span>
                  <Switch
                    checked={shared}
                    disabled={!shareId}
                    title={shareId ? undefined : 'Sharing preference unavailable for this account'}
                    onCheckedChange={(next) => {
                      if (shareId) toggleShared(shareId, next)
                    }}
                    aria-label="Share this account with your household"
                    aria-describedby={descriptionId}
                  />
                </div>
              }
            />
          </Card>
        )
      })}
    </div>
  )
}
