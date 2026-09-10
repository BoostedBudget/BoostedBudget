/**
 * AccountCustomizationModal Component
 *
 * Modal dialog for customizing account appearance:
 * - Nickname
 * - Custom color
 * - Custom icon or emoji
 * - Hidden status
 *
 * Issue #307: Account Customization
 */

import { useState, useCallback, useEffect, useRef } from 'react'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { Eye, EyeOff, Loader2, Users, UserX, LineChart, RefreshCwOff, ChevronDown } from 'lucide-react'
import { ColorPicker } from './ColorPicker'
import { IconSelector } from './IconSelector'
import { EmojiInput } from './EmojiInput'
import { AccountIconDisplay } from './AccountIconDisplay'
import { useHousehold, useHouseholdVisibility } from '@/features/household'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import type { Account, AccountCustomizationChanges, AccountIcon } from '../types'

// Internal component to handle modal content with its own state
interface ModalContentProps {
  account: Account
  onSave: (account: Account, changes: AccountCustomizationChanges) => Promise<void>
  onCancel: () => void
  isSaving: boolean
}

function ModalContent({ account, onSave, onCancel, isSaving }: ModalContentProps) {
  // Form state - initialized from account
  const [nickname, setNickname] = useState(account.nickname || '')
  const [customColor, setCustomColor] = useState<string | null>(account.custom_color || null)
  const [customIcon, setCustomIcon] = useState<AccountIcon | null>((account.custom_icon as AccountIcon) || null)
  const [customEmoji, setCustomEmoji] = useState<string | null>(account.custom_emoji || null)
  const [isHidden, setIsHidden] = useState(account.is_hidden || false)
  const [excludeInvestmentSync, setExcludeInvestmentSync] = useState(
    account.exclude_investment_sync || false
  )
  const [showHideConfirm, setShowHideConfirm] = useState(false)
  const [activeTab, setActiveTab] = useState<'icon' | 'emoji'>(account.custom_emoji ? 'emoji' : 'icon')
  // #1618 (dropped requirement, closed in #1609/P4-T5): the exclude-investment toggle only
  // matters for an account that is double-reported (bank sync + investment sync), not for every
  // chained account — showing it unconditionally for a plain checking account is noise the issue
  // explicitly called out. The user chose "behind an advanced section" over gating by account
  // shape, so it lives collapsed here rather than being conditionally rendered. Collapsed by
  // default regardless of the account's own value, so a user who already excluded investment
  // activity on this account still has to open Advanced to see or change it — the section hides
  // *discoverability noise*, not the account's own state.
  const [advancedOpen, setAdvancedOpen] = useState(false)

  // Issue #1370 Task 8: household sharing is a separate domain/endpoint from
  // UpdateAccountRequest (see handleSave below). Hooks run unconditionally
  // (Rules of Hooks); the share row itself only renders when
  // canShowShareSwitch is true.
  const { isInHousehold } = useHousehold()
  const { isShared, isLoading: isSharesLoading, setVisibility } = useHouseholdVisibility()
  const trackEvent = useTrackEvent()
  const userAccountId = account.user_account_id
  // No user_account_id means the account can't be looked up via
  // useHouseholdVisibility, so the row is hidden entirely rather than
  // disabled — same "hidden, not inert" pattern as ScopeSegmentedControl.
  const canShowShareSwitch = isInHousehold && !!userAccountId
  const [initialShareVisibility, setInitialShareVisibility] = useState(() =>
    userAccountId ? isShared(userAccountId) : false
  )
  const [wantsShared, setWantsShared] = useState(initialShareVisibility)
  // The lazy-init above can run before useHouseholdVisibility's query has
  // resolved (this modal may be the first place on the page that calls it),
  // in which case `isShared` sees an empty `shares` list and both states are
  // wrongly seeded to `false`. Once real data lands, re-sync — but only if
  // the user hasn't already interacted with the switch, so a slow background
  // refetch can never clobber an in-progress edit.
  const userToggledShareRef = useRef(false)
  useEffect(() => {
    if (isSharesLoading || !userAccountId || userToggledShareRef.current) return
    const shared = isShared(userAccountId)
    setInitialShareVisibility(shared)
    setWantsShared(shared)
  }, [isSharesLoading, userAccountId, isShared])
  // Belt-and-suspenders for the loading race above: the resync effect only
  // protects an in-progress *toggle*, but the user could otherwise still
  // click Save while the query is loading and the switch is showing a
  // provisional (possibly wrong) value — since nothing was "changed" from
  // that wrong baseline, no PATCH would fire and the modal would just close
  // having silently shown the wrong state. Disabling the switch while
  // loading removes the window entirely: there's nothing to act on until the
  // real value is known.
  const isShareSwitchDisabled = isSharesLoading

  // Local submit guard: `isSaving` (prop) only reflects the parent's own
  // appearance-save mutation, so a share-only save (no appearance changes)
  // has no external pending flag — without this, a slow setVisibility call
  // could be double-submitted by a second click.
  const [isSubmitting, setIsSubmitting] = useState(false)

  // Build display name for preview
  const displayName = nickname || account.official_name || account.name || 'Account'

  // Handle save
  const handleSave = useCallback(async () => {
    const changes: AccountCustomizationChanges = {}

    // Only include changed fields
    if (nickname !== (account.nickname || '')) {
      changes.nickname = nickname || null
    }
    if (customColor !== (account.custom_color || null)) {
      changes.custom_color = customColor
    }
    if (activeTab === 'icon') {
      // When using icon, clear emoji
      if (customIcon !== (account.custom_icon || null)) {
        changes.custom_icon = customIcon
      }
      if (account.custom_emoji) {
        changes.custom_emoji = null
      }
    } else {
      // When using emoji, clear icon
      if (customEmoji !== (account.custom_emoji || null)) {
        changes.custom_emoji = customEmoji
      }
      if (account.custom_icon) {
        changes.custom_icon = null
      }
    }
    if (isHidden !== (account.is_hidden || false)) {
      changes.is_hidden = isHidden
    }
    if (excludeInvestmentSync !== (account.exclude_investment_sync || false)) {
      changes.exclude_investment_sync = excludeInvestmentSync
    }

    const hasAppearanceChanges = Object.keys(changes).length > 0
    const shareVisibilityChanged = !!userAccountId && wantsShared !== initialShareVisibility

    if (!hasAppearanceChanges && !shareVisibilityChanged) {
      onCancel()
      return
    }

    setIsSubmitting(true)
    try {
      // Household sharing hits a separate endpoint from the appearance
      // `changes` payload above, so it must never be folded into `onSave`.
      // Fired via Promise.allSettled (not Promise.all): a failure in one must
      // not block or hide the outcome of the other. Unlike before, `onSave`
      // here is the raw parent prop — it no longer closes the modal as a
      // side effect. Closing is decided below, once BOTH results are known,
      // so a share-visibility failure can never be masked by an unrelated
      // appearance-save success (or vice versa).
      const results = await Promise.allSettled([
        hasAppearanceChanges ? onSave(account, changes) : Promise.resolve(),
        shareVisibilityChanged && userAccountId
          ? setVisibility(userAccountId, wantsShared ? 'shared' : 'hidden')
          : Promise.resolve(),
      ])
      const [appearanceResult, shareResult] = results
      const appearanceFailed = hasAppearanceChanges && appearanceResult.status === 'rejected'
      const shareFailed = shareVisibilityChanged && shareResult.status === 'rejected'

      // onSave's own mutation hook is responsible for surfacing appearance-save
      // failures (toast/logging), same as before this change. setVisibility has
      // no such caller-side handling, so a rejection here must be surfaced
      // explicitly — otherwise the user gets no signal that sharing didn't
      // change, even though the switch itself already flipped back visually
      // next time the modal reopens (since the failed write never persisted).
      if (shareFailed) {
        // Directionally accurate messaging (privacy-consequential case): if the
        // user was trying to REVOKE household sharing and the write failed, the
        // account is STILL shared — say so explicitly rather than a generic
        // failure message, since silently believing an account went private
        // when it didn't is the real risk here. Trying to share (safe-by-
        // default) can use a generic message.
        toast.error(
          wantsShared
            ? 'Failed to update household sharing. Please try again.'
            : 'Failed to stop sharing with your household — this account is still visible to household members. Please try again.'
        )
      } else if (shareVisibilityChanged) {
        // Fire only once the share-visibility write has actually persisted,
        // never on failure and never merely on toggling the switch.
        trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_ACCOUNT_SHARE_CHANGED, {
          visibility: wantsShared ? 'shared' : 'hidden',
        })
      }

      // Only close the modal once every attempted write has actually
      // succeeded. This intentionally covers all four save shapes:
      // appearance-only, share-only, both, and neither attempted (the early
      // return above already handles "no changes"). Closing on a partial
      // mixed-save failure would let a failed share-visibility write hide
      // behind a successful appearance save, silently misleading the user
      // about their household-sharing state.
      if (!appearanceFailed && !shareFailed) {
        onCancel()
      }
    } finally {
      setIsSubmitting(false)
    }
  }, [account, nickname, customColor, customIcon, customEmoji, isHidden, excludeInvestmentSync, activeTab, onSave, onCancel, userAccountId, wantsShared, initialShareVisibility, setVisibility, trackEvent])

  return (
    <>
      <DialogHeader>
        <DialogTitle>Customize Account</DialogTitle>
        <DialogDescription>
          Personalize how this account appears in your dashboard.
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-6 py-4">
        {/* Preview Card */}
        <div className="rounded-lg border p-4 bg-muted/30">
          <div className="flex items-center gap-3">
            {/* Icon/Emoji preview */}
            <div
              className={cn(
                'flex items-center justify-center w-10 h-10 rounded-lg text-lg font-medium',
                customColor ? 'text-white' : 'bg-muted'
              )}
              style={customColor ? { backgroundColor: customColor } : undefined}
            >
              {activeTab === 'emoji' && customEmoji ? (
                customEmoji
              ) : customIcon ? (
                <AccountIconDisplay icon={customIcon} className="h-5 w-5" />
              ) : (
                displayName.charAt(0).toUpperCase()
              )}
            </div>

            {/* Account info */}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span
                  className="font-medium truncate"
                  style={customColor ? { color: customColor } : undefined}
                  data-ph-mask
                >
                  {displayName}
                </span>
                {isHidden && (
                  <span className="text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded">
                    Hidden
                  </span>
                )}
              </div>
              <span className="text-sm text-muted-foreground truncate block">
                <span data-ph-mask>{account.institution_name}</span>
                {' '}- {account.subtype || account.type}
              </span>
            </div>
          </div>
        </div>

        {/* Nickname */}
        <div className="space-y-2">
          <label htmlFor="nickname" className="text-sm font-medium">
            Nickname
          </label>
          <input
            id="nickname"
            type="text"
            value={nickname}
            onChange={(e) => setNickname(e.target.value)}
            placeholder="Account name"
            maxLength={255}
            className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
          />
          <p className="text-xs text-muted-foreground">
            A custom name to help you identify this account.
          </p>
        </div>

        {/* Color */}
        <div className="space-y-2">
          <label className="text-sm font-medium">Color</label>
          <ColorPicker
            value={customColor}
            onChange={setCustomColor}
          />
        </div>

        {/* Icon or Emoji */}
        <div className="space-y-2">
          <label className="text-sm font-medium">Icon or Emoji</label>
          <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as 'icon' | 'emoji')}>
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="icon">Icon</TabsTrigger>
              <TabsTrigger value="emoji">Emoji</TabsTrigger>
            </TabsList>
            <TabsContent value="icon" className="mt-3">
              <IconSelector
                value={customIcon}
                onChange={setCustomIcon}
                iconColor={customColor || undefined}
              />
            </TabsContent>
            <TabsContent value="emoji" className="mt-3">
              <EmojiInput
                value={customEmoji}
                onChange={setCustomEmoji}
              />
            </TabsContent>
          </Tabs>
        </div>

        {/* Hidden toggle */}
        <div className="flex items-center justify-between rounded-lg border p-3">
          <div className="space-y-0.5">
            <div className="flex items-center gap-2 text-sm font-medium">
              {isHidden ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              Hide Account
            </div>
            <p className="text-xs text-muted-foreground">
              Hidden accounts won't appear in the default view.
            </p>
          </div>
          <Switch
            checked={isHidden}
            onCheckedChange={(checked) => {
              if (checked) {
                setShowHideConfirm(true)
              } else {
                setIsHidden(false)
              }
            }}
            aria-label={isHidden ? 'Show account' : 'Hide account'}
            title={isHidden ? 'Show account' : 'Hide account'}
          />
        </div>

        {/* Advanced disclosure — Issue #1618 (dropped requirement, closed by #1609/P4-T5). Houses
            the exclude-investment toggle, currently the only control relevant to a minority of
            accounts (ones double-reported via both bank sync and investment sync) rather than to
            every account in the modal. Collapsed by default: this is a *discoverability* gate,
            distinct from and orthogonal to the `disabled={!account.user_account_id}` gate on the
            switch below, which is about storage availability, not relevance. Modeled on the
            existing hand-rolled disclosure pattern (see DeletedGoalsPanel.tsx) rather than a new
            dependency — no Collapsible/Accordion primitive exists in this design system yet. */}
        <div>
          <button
            type="button"
            onClick={() => setAdvancedOpen((v) => !v)}
            aria-expanded={advancedOpen}
            aria-controls="account-customization-advanced"
            className="flex w-full items-center justify-between rounded-lg border border-border px-3 py-2 text-left transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <span className="text-sm font-medium text-foreground">Advanced</span>
            <ChevronDown
              aria-hidden="true"
              className={cn(
                'h-4 w-4 text-muted-foreground transition-transform motion-reduce:transition-none',
                advancedOpen && 'rotate-180'
              )}
            />
          </button>

          {advancedOpen && (
            <div id="account-customization-advanced" className="mt-2">
              {/* Exclude investment activity toggle — Issue #1609 Item 5 / #1618b. Some accounts
                  sync through both banking sync and investment sync, so their activity can appear
                  twice in the feed (once from each source). This is the user-facing control for
                  that: it maps straight onto exclude_investment_sync.

                  ⚠ #1631 M4 — the copy below MUST keep its second sentence. The flag is no longer
                  a read filter: while it is on, the backend stops STORING this account's
                  investment transactions, and turning it back off backfills only 2 years (Plaid's
                  limit on /investments/transactions/get). An account excluded for longer
                  permanently loses its earliest excluded period. This toggle is the only place the
                  user is told that before they commit, which is a condition of the retention ADR's
                  #1631 amendment permitting non-ingestion at all — so a shorter, friendlier
                  de-duplication-only promise here is a defect, not a copy improvement.

                  That is CHECKABLE, not just asserted: AccountCustomizationModal.test.tsx's
                  "warns that exclusion stops saving activity and that the backfill reaches only 2
                  years" fails if the /2 years/ or /stop saving/ wording is dropped. It matches on
                  those two facts rather than the whole sentence, so the copy can still be reworded
                  — it just cannot lose the warning. */}
              <div className="flex items-center justify-between rounded-lg border p-3">
                <div className="space-y-0.5">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    {excludeInvestmentSync ? (
                      <RefreshCwOff className="h-4 w-4" />
                    ) : (
                      <LineChart className="h-4 w-4" />
                    )}
                    Exclude Investment Activity
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {account.user_account_id
                      ? "Stops this account's activity from showing up twice — once from bank sync, once from investment sync. While this is on we stop saving its investment activity, and turning it back off restores only the last 2 years."
                      : 'Not available for this account yet — it has no stable account record to store the setting on.'}
                  </p>
                </div>
                <Switch
                  checked={excludeInvestmentSync}
                  onCheckedChange={setExcludeInvestmentSync}
                  disabled={!account.user_account_id}
                  aria-label={
                    excludeInvestmentSync ? 'Include investment activity' : 'Exclude investment activity'
                  }
                  title={
                    !account.user_account_id
                      ? 'Unavailable until this account is linked to a stable account record'
                      : excludeInvestmentSync
                        ? 'Include investment activity'
                        : 'Exclude investment activity'
                  }
                />
              </div>
            </div>
          )}
        </div>

        {/* Share with household toggle — Issue #1370 Task 8. Distinct from
            "Hide Account" above: that controls the OWNER's own default view;
            this controls whether OTHER household members can see the account
            at all (household aggregation only counts visibility === 'shared'). */}
        {canShowShareSwitch && (
          <div className="flex items-center justify-between rounded-lg border p-3">
            <div className="space-y-0.5">
              <div className="flex items-center gap-2 text-sm font-medium">
                {wantsShared ? <Users className="h-4 w-4" /> : <UserX className="h-4 w-4" />}
                {wantsShared ? 'Share with household' : 'Hidden from household'}
              </div>
              <p className="text-xs text-muted-foreground">
                {wantsShared
                  ? 'Other household members can see this account in shared views.'
                  : 'Excluded from household totals and hidden from other members.'}
              </p>
            </div>
            <Switch
              checked={wantsShared}
              onCheckedChange={(checked) => {
                // Mark the switch as user-touched so the loading-resync
                // effect above never overwrites this edit with a lagging
                // fetch result.
                userToggledShareRef.current = true
                setWantsShared(checked)
              }}
              disabled={isShareSwitchDisabled}
              aria-label={wantsShared ? 'Hide from household' : 'Share with household'}
              title={
                isShareSwitchDisabled
                  ? 'Loading household sharing status…'
                  : wantsShared ? 'Hide from household' : 'Share with household'
              }
            />
          </div>
        )}
      </div>

      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={onCancel}
          disabled={isSaving || isSubmitting}
        >
          Cancel
        </Button>
        <Button
          type="button"
          onClick={handleSave}
          disabled={isSaving || isSubmitting}
        >
          {(isSaving || isSubmitting) && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          Save Changes
        </Button>
      </DialogFooter>

      {/* Hide Account Confirmation — Issue #542 */}
      <ConfirmationDialog
        open={showHideConfirm}
        onOpenChange={(open) => !open && setShowHideConfirm(false)}
        onConfirm={() => {
          setIsHidden(true)
          setShowHideConfirm(false)
        }}
        title="Hide Account?"
        description="This account will be hidden from your default view."
        variant="warning"
        confirmButtonText="Hide Account"
      />
    </>
  )
}

export interface AccountCustomizationModalProps {
  /** Account to customize */
  account: Account | null
  /** Whether the modal is open */
  open: boolean
  /** Callback when modal should close */
  onOpenChange: (open: boolean) => void
  /** Callback when save is clicked - passes full account for user_account_id resolution (Issue #171) */
  onSave: (account: Account, changes: AccountCustomizationChanges) => Promise<void>
  /** Whether save is in progress */
  isSaving?: boolean
}

/**
 * AccountCustomizationModal for editing account appearance
 *
 * @example
 * ```tsx
 * <AccountCustomizationModal
 *   account={selectedAccount}
 *   open={isModalOpen}
 *   onOpenChange={setIsModalOpen}
 *   onSave={handleSave}
 *   isSaving={isUpdating}
 * />
 * ```
 */
export function AccountCustomizationModal({
  account,
  open,
  onOpenChange,
  onSave,
  isSaving = false,
}: AccountCustomizationModalProps) {
  // Issue #1370 Task 8: closing the modal used to be an implicit side effect
  // of this wrapper's `onSave` call succeeding — which meant an appearance
  // save succeeding could close the modal even though a parallel
  // share-visibility write (fired inside ModalContent's handleSave) had
  // failed. `ModalContent` now evaluates both outcomes itself and calls
  // `onCancel` (below) only once every attempted write has succeeded, so
  // `onSave` is passed straight through with no wrapping needed here.
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md max-h-[90vh] overflow-y-auto">
        {account && (
          <ModalContent
            key={account.id}
            account={account}
            onSave={onSave}
            onCancel={() => onOpenChange(false)}
            isSaving={isSaving}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
