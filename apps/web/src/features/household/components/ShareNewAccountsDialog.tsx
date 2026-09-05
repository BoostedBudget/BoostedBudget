/**
 * ShareNewAccountsDialog Component
 *
 * Issue #1370: Household feature — post-Plaid-link share dialog (Task 9)
 *
 * Backend semantics (verified against analytics_repository.go): household
 * aggregation includes an account ONLY when `household_account_shares
 * .visibility = 'shared' AND revoked_at IS NULL`. There is NO
 * null-defaults-to-shared behavior — a freshly-linked account has no share
 * row at all (`visibility: null`), so it is EXCLUDED from the household view
 * until an explicit write sets it to `'shared'`. This dialog is that
 * explicit-write consent moment, triggered right after a household member
 * links new accounts via Plaid.
 *
 * "Skip" is NOT a no-op: per the semantics above, dismissing without an
 * explicit write would leave every listed account excluded from the
 * household. Skip therefore still writes `visibility: 'shared'` for every
 * writable account (accepting the shared-by-default), the same as clicking
 * "Save & Continue" with every switch left on. Only abandoning the dialog
 * entirely (closing the tab, or dismissing via the dialog's own X/overlay)
 * leaves accounts at `null`/excluded — a privacy-safe fallback that requires
 * no action from this component.
 *
 * Structural model: this mirrors ConsentPreview.tsx's per-row rendering
 * (Card + AccountSummaryHeader + Switch, PII kept out of aria-label via a
 * separate data-ph-mask'd description node), but the container is a Dialog
 * (a modal moment) rather than an inline page section, and there are two
 * explicit actions instead of an implicit "current toggle state" commit.
 *
 * Outer/inner split (AccountCustomizationModal precedent): `AccountsPage`
 * renders this component unconditionally (its own `open` prop just toggles
 * Radix visibility), including for solo users and on every render before
 * any Plaid link has happened. If `useHouseholdVisibility()` — which calls
 * `useListMyAccountShares()`, a real API-backed query — were called directly
 * in this top-level component, it would fire that household-scoped request
 * for EVERY user on the accounts page, solo included, which is exactly the
 * "no behavior change for solo users" regression the epic wants to avoid.
 * So, same as `AccountCustomizationModal`'s outer component staying
 * hook-free and only mounting `ModalContent` (which owns the household
 * hooks) once `account` is non-null, this outer component stays hook-free
 * and only mounts `ShareNewAccountsDialogContent` (which owns
 * `useHouseholdVisibility` and all the interactive state) once `accounts`
 * is non-empty — which only happens after `AccountsPage.handleAccountsLinked`
 * has already confirmed both `isInHousehold` and a just-linked match.
 *
 * Batch-write approach: `Promise.allSettled`, not `Promise.all` — a single
 * account's write failing must not prevent the others from being attempted
 * or reported. Modeled on AccountCustomizationModal.tsx's Promise.allSettled
 * use for its own (2-way) share-visibility + appearance save.
 *
 * ⚠ ONE CONTROL PER SHARING UNIT, NOT PER ACCOUNT (#1569 T10-FIX item 5).
 * ConsentPreview was fixed for this in T10.3 item C and this dialog — its
 * sibling consent surface — was not, so it kept rendering a switch per
 * ACCOUNT while calling `setVisibility(account.user_account_id, …)`. Two
 * maskless accounts at one institution are forced to share one
 * `user_account_id` (migration 000100), so the user got two controls over one
 * backend value and Save issued two writes for it, whose order decided the
 * result. The grouping decision now lives in one module, `../sharingUnits`,
 * and both surfaces read it.
 *
 * Missing `user_account_id` handling: same edge case as ConsentPreview/
 * AccountCustomizationModal — an account without a `user_account_id` can't
 * be referenced by the backend's share-visibility endpoint, and cannot enter
 * `household_account_shares` at all. Such rows are shown read-only and
 * labelled NOT SHARED (#1569 T10-FIX item 8: they used to read "Shared",
 * which was under-sharing plus a false statement), and are excluded from the
 * writable set for both actions.
 *
 * Error surfacing: a failed write is NOT silently swallowed. `toast.error`
 * (sonner) reports the failure, an inline message is also shown in the
 * dialog, and the dialog stays open (so the user can retry) rather than
 * closing over an incomplete state.
 */

import { useState } from 'react'
import type { ReactNode } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import { AccountSummaryHeader } from '@/features/accounts/components/AccountSummaryHeader'
import { getAccountTypeLabel } from '@/features/accounts/utils'
import type { Account } from '@/features/accounts/types'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { useHouseholdVisibility } from '../hooks/useHouseholdVisibility'
import { HOUSEHOLD_SHARE_CONSENT_COPY } from '../householdShareConsent'
import { groupAccountsBySharingUnit, sharingUnitDisplayName } from '../sharingUnits'

export interface ShareNewAccountsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The accounts just linked via Plaid — fetched/diffed by the caller (AccountsPage), not here. */
  accounts: Account[]
}

export function ShareNewAccountsDialog({ open, onOpenChange, accounts }: ShareNewAccountsDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {accounts.length > 0 ? (
          <ShareNewAccountsDialogContent
            key={accounts.map((account) => account.id).join(',')}
            accounts={accounts}
            onOpenChange={onOpenChange}
          />
        ) : (
          // Defensive-only: AccountsPage never opens this dialog with an empty
          // `accounts` list (handleAccountsLinked only sets isShareDialogOpen
          // once at least one just-linked account is found). Radix still
          // requires every DialogContent to have a DialogTitle for a11y, so a
          // visually-hidden fallback keeps that contract even if this state is
          // ever reached some other way.
          <DialogTitle className="sr-only">Share your new accounts</DialogTitle>
        )}
      </DialogContent>
    </Dialog>
  )
}

interface ShareNewAccountsDialogContentProps {
  accounts: Account[]
  onOpenChange: (open: boolean) => void
}

function ShareNewAccountsDialogContent({ accounts, onOpenChange }: ShareNewAccountsDialogContentProps) {
  const { setVisibility } = useHouseholdVisibility()
  const trackEvent = useTrackEvent()
  // Set of user_account_ids the member has toggled OFF (hidden). Every
  // account starts shared (switch on) — mirrors ConsentPreview's hiddenIds
  // model.
  const [hiddenIds, setHiddenIds] = useState<Set<string>>(new Set())
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const toggleShared = (shareId: string, shared: boolean) => {
    const next = new Set(hiddenIds)
    if (shared) {
      next.delete(shareId)
    } else {
      next.add(shareId)
    }
    setHiddenIds(next)
  }

  // Accounts without a user_account_id can't be referenced by the
  // share-visibility endpoint — excluded from both write paths.
  const units = groupAccountsBySharingUnit(accounts)
  // One write per SHARING UNIT. Mapping over accounts issued a duplicate write for every maskless
  // pair — two PUTs at the same key, racing, for a value the user set once.
  const writableShareIds = units
    .map((unit) => unit.shareId)
    .filter((shareId): shareId is string => Boolean(shareId))

  async function writeAll(forceShared: boolean) {
    setIsSaving(true)
    setError(null)

    const getVisibility = (userAccountId: string): 'shared' | 'hidden' =>
      forceShared || !hiddenIds.has(userAccountId) ? 'shared' : 'hidden'

    const results = await Promise.allSettled(
      writableShareIds.map((shareId) => setVisibility(shareId, getVisibility(shareId)))
    )

    setIsSaving(false)

    const failureCount = results.filter((result) => result.status === 'rejected').length
    if (failureCount > 0) {
      const message =
        failureCount === writableShareIds.length
          ? 'Failed to update household sharing. Please try again.'
          : `Failed to update household sharing for ${failureCount} account${failureCount === 1 ? '' : 's'}. Please try again.`
      setError(message)
      toast.error(message)
      return
    }

    // Fire only once the batch write has actually persisted, never on
    // failure. Both "Save & Continue" and "Skip" write visibility (Skip is
    // not a no-op — see the component doc comment), so both fire the same
    // event, distinguished by `method`. Counts are aggregate (not PII).
    // Counted per sharing unit, matching what was actually written. Counting accounts reported a
    // maskless pair as two decisions when the user made one.
    const sharedCount = writableShareIds.filter((shareId) => getVisibility(shareId) === 'shared').length
    trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED, {
      method: forceShared ? 'skip' : 'save',
      shared_count: sharedCount,
      hidden_count: writableShareIds.length - sharedCount,
    })

    onOpenChange(false)
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{HOUSEHOLD_SHARE_CONSENT_COPY.shareNewAccountsTitle}</DialogTitle>
        <DialogDescription>{HOUSEHOLD_SHARE_CONSENT_COPY.shareNewAccountsDescription}</DialogDescription>
      </DialogHeader>

      <div className="space-y-3">
        {units.map((unit) => {
          const { shareId, accounts: unitAccounts } = unit
          const shareable = Boolean(shareId)
          const shared = shareId ? !hiddenIds.has(shareId) : false
          const displayNames = unitAccounts.map(sharingUnitDisplayName)
          const descriptionId = `share-new-account-name-${unit.key}`
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
                disabled={!shareable || isSaving}
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
              {/* aria-label is recorded verbatim by session replay, so the names live in a
                  separate data-ph-mask'd node the switch is described by. */}
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

        {error && (
          <p className="text-sm text-destructive" role="alert">
            {error}
          </p>
        )}
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={() => void writeAll(true)} disabled={isSaving}>
          Skip
        </Button>
        <Button type="button" onClick={() => void writeAll(false)} disabled={isSaving}>
          Save & Continue
        </Button>
      </DialogFooter>
    </>
  )
}
