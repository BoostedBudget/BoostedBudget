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
import { Eye, EyeOff, Loader2, Users, UserX } from 'lucide-react'
import { ColorPicker } from './ColorPicker'
import { IconSelector } from './IconSelector'
import { EmojiInput } from './EmojiInput'
import { AccountIconDisplay } from './AccountIconDisplay'
import { useHousehold, useHouseholdVisibility } from '@/features/household'
import type { Account, AccountIcon, UpdateAccountRequest } from '../types'

// Internal component to handle modal content with its own state
interface ModalContentProps {
  account: Account
  onSave: (account: Account, changes: Partial<UpdateAccountRequest>) => Promise<void>
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
  const [showHideConfirm, setShowHideConfirm] = useState(false)
  const [activeTab, setActiveTab] = useState<'icon' | 'emoji'>(account.custom_emoji ? 'emoji' : 'icon')

  // Issue #1370 Task 8: household sharing is a separate domain/endpoint from
  // UpdateAccountRequest (see handleSave below). Hooks run unconditionally
  // (Rules of Hooks); the share row itself only renders when
  // canShowShareSwitch is true.
  const { isInHousehold } = useHousehold()
  const { isShared, isLoading: isSharesLoading, setVisibility } = useHouseholdVisibility()
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
    const changes: Partial<UpdateAccountRequest> = {}

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
      // not block or hide the outcome of the other. `onSave`, when invoked,
      // is the parent's wrapped handler and closes the modal itself on
      // success — that's unaffected by whatever happens to the
      // share-visibility call.
      const results = await Promise.allSettled([
        hasAppearanceChanges ? onSave(account, changes) : Promise.resolve(),
        shareVisibilityChanged && userAccountId
          ? setVisibility(userAccountId, wantsShared ? 'shared' : 'hidden')
          : Promise.resolve(),
      ])
      const shareResult = results[1]

      // onSave's own mutation hook is responsible for surfacing appearance-save
      // failures (toast/logging), same as before this change. setVisibility has
      // no such caller-side handling, so a rejection here must be surfaced
      // explicitly — otherwise the user gets no signal that sharing didn't
      // change, even though the switch itself already flipped back visually
      // next time the modal reopens (since the failed write never persisted).
      if (shareVisibilityChanged && shareResult.status === 'rejected') {
        toast.error('Failed to update household sharing. Please try again.')
      }

      // When only the share visibility changed (no appearance changes), nothing
      // else closes the modal — do it explicitly, but only once the share save
      // has actually succeeded. A failed share-only save must leave the modal
      // open so the user can see the error and retry, not silently believe the
      // change went through.
      if (!hasAppearanceChanges && shareResult.status === 'fulfilled') {
        onCancel()
      }
    } finally {
      setIsSubmitting(false)
    }
  }, [account, nickname, customColor, customIcon, customEmoji, isHidden, activeTab, onSave, onCancel, userAccountId, wantsShared, initialShareVisibility, setVisibility])

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
  onSave: (account: Account, changes: Partial<UpdateAccountRequest>) => Promise<void>
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
  const handleSave = useCallback(async (account: Account, changes: Partial<UpdateAccountRequest>) => {
    await onSave(account, changes)
    onOpenChange(false)
  }, [onSave, onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md max-h-[90vh] overflow-y-auto">
        {account && (
          <ModalContent
            key={account.id}
            account={account}
            onSave={handleSave}
            onCancel={() => onOpenChange(false)}
            isSaving={isSaving}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
