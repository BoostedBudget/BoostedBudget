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

import { useState, useCallback } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import { Eye, EyeOff, Loader2 } from 'lucide-react'
import { ColorPicker } from './ColorPicker'
import { IconSelector } from './IconSelector'
import { EmojiInput } from './EmojiInput'
import { AccountIconDisplay } from './AccountIconDisplay'
import type { Account, AccountIcon, UpdateAccountRequest } from '../types'

// Internal component to handle modal content with its own state
interface ModalContentProps {
  account: Account
  onSave: (accountId: string, changes: Partial<UpdateAccountRequest>) => Promise<void>
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
  const [activeTab, setActiveTab] = useState<'icon' | 'emoji'>(account.custom_emoji ? 'emoji' : 'icon')

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

    // Only save if there are changes
    if (Object.keys(changes).length > 0) {
      await onSave(account.id, changes)
    } else {
      onCancel()
    }
  }, [account, nickname, customColor, customIcon, customEmoji, isHidden, activeTab, onSave, onCancel])

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
                {account.institution_name} - {account.subtype || account.type}
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
            placeholder={account.official_name || account.name}
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
          <button
            type="button"
            role="switch"
            aria-checked={isHidden}
            onClick={() => setIsHidden(!isHidden)}
            className={cn(
              'relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
              isHidden ? 'bg-primary' : 'bg-input'
            )}
          >
            <span
              className={cn(
                'pointer-events-none block h-5 w-5 rounded-full bg-background shadow-lg ring-0 transition-transform',
                isHidden ? 'translate-x-5' : 'translate-x-0'
              )}
            />
          </button>
        </div>
      </div>

      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={onCancel}
          disabled={isSaving}
        >
          Cancel
        </Button>
        <Button
          type="button"
          onClick={handleSave}
          disabled={isSaving}
        >
          {isSaving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          Save Changes
        </Button>
      </DialogFooter>
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
  /** Callback when save is clicked */
  onSave: (accountId: string, changes: Partial<UpdateAccountRequest>) => Promise<void>
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
  const handleCancel = useCallback(() => {
    onOpenChange(false)
  }, [onOpenChange])

  const handleSave = useCallback(async (accountId: string, changes: Partial<UpdateAccountRequest>) => {
    await onSave(accountId, changes)
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
            onCancel={handleCancel}
            isSaving={isSaving}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
