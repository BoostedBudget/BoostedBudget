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
 * Missing `user_account_id` handling: same edge case as ConsentPreview/
 * AccountCustomizationModal — an account without a `user_account_id` can't
 * be referenced by the backend's share-visibility endpoint. Such rows are
 * shown read-only (switch disabled, always "shared") and are excluded from
 * the writable set for both actions, rather than risking an id the backend
 * doesn't recognize.
 *
 * Error surfacing: a failed write is NOT silently swallowed. `toast.error`
 * (sonner) reports the failure, an inline message is also shown in the
 * dialog, and the dialog stays open (so the user can retry) rather than
 * closing over an incomplete state.
 */

import { useState } from 'react'
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
  const writableAccounts = accounts.filter((account): account is Account & { user_account_id: string } =>
    !!account.user_account_id
  )

  async function writeAll(forceShared: boolean) {
    setIsSaving(true)
    setError(null)

    const getVisibility = (userAccountId: string): 'shared' | 'hidden' =>
      forceShared || !hiddenIds.has(userAccountId) ? 'shared' : 'hidden'

    const results = await Promise.allSettled(
      writableAccounts.map((account) =>
        setVisibility(account.user_account_id, getVisibility(account.user_account_id))
      )
    )

    setIsSaving(false)

    const failureCount = results.filter((result) => result.status === 'rejected').length
    if (failureCount > 0) {
      const message =
        failureCount === writableAccounts.length
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
    const sharedCount = writableAccounts.filter(
      (account) => getVisibility(account.user_account_id) === 'shared'
    ).length
    trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED, {
      method: forceShared ? 'skip' : 'save',
      shared_count: sharedCount,
      hidden_count: writableAccounts.length - sharedCount,
    })

    onOpenChange(false)
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Share your new accounts?</DialogTitle>
        <DialogDescription>
          Choose which of the accounts you just connected to share with your household. All
          accounts are shared by default — you can change this any time from account settings.
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-3">
        {accounts.map((account) => {
          const shareId = account.user_account_id
          const shared = shareId ? !hiddenIds.has(shareId) : true
          const displayName = account.nickname || account.official_name || account.name
          const descriptionId = `share-new-account-name-${account.id}`

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
                    <span className="text-xs text-muted-foreground">
                      {shared ? 'Shared' : 'Hidden'}
                    </span>
                    <Switch
                      checked={shared}
                      disabled={!shareId || isSaving}
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
