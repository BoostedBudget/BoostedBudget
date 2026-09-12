import { useRef, useState } from 'react'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import type { Account } from '@/features/accounts/types'
import { formatTransactionAmount } from '@/shared/transactions/amount'
import { formatDate } from '@/shared/transactions/dates'
import { mergeTransactionEdit, invalidateTransactionEdit } from '../hooks/transactionEditCache'
import { useQueryClient } from '@tanstack/react-query'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { ReadOnlyGuard } from '@/auth'
import type { Transaction } from '../types'
import type { UserTag } from '../../tags/types'
import type { CustomCategory } from '../../categories/types'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import { TagEditor } from '../../tags/components/TagEditor'
import { CategoryEditor } from '../../categories/components/CategoryEditor'
import { useReviewMutations } from '../hooks/useReviewMutations'
import { getEffectiveCategory } from '@/shared/categories/getEffectiveCategory'
import { categorySelectionEqual, seedCategorySelection, type CategorySelection } from '@/shared/categories/categorySelection'
import { getMerchantDisplayName } from '@/shared/transactions/merchant'
import { ReviewBadge, reviewBadgeVariantFromTransaction } from '@/shared/components/ReviewBadge'
import {
  useUpdateTransactionTags,
  useUpdateTransactionCategory,
} from '@boostedbudget/api-client/endpoints/transactions/transactions'

interface TransactionEditorProps {
  transaction: Transaction
  account?: Pick<Account, 'name'>
  availableTags: UserTag[]
  availableCategories: CustomCategory[]
  plaidPrimaries?: Primary[]
  onClose: () => void
  onSave: (updatedTransaction: Transaction) => void
  onTagCreated?: (tag: UserTag) => void
  onCategoryCreated?: (category: CustomCategory) => void
  isOpen: boolean
}

export function TransactionEditor(props: TransactionEditorProps) {
  return props.isOpen ? <TransactionEditorSession key={props.transaction.id} {...props} /> : null
}

function TransactionEditorSession({
  transaction,
  account,
  availableTags,
  availableCategories,
  plaidPrimaries,
  onClose,
  onSave,
  onTagCreated,
  onCategoryCreated,
  isOpen,
}: TransactionEditorProps) {
  // This controlled dialog has no Radix DialogTrigger. Remember the actual
  // opener before the dialog mounts and moves focus into its FocusScope.
  const [opener] = useState(() => document.activeElement instanceof HTMLElement ? document.activeElement : null)
  const [saved, setSaved] = useState(transaction)
  const [tags, setTags] = useState<string[]>(transaction.tags || [])
  const [category, setCategory] = useState<CategorySelection>(() =>
    seedCategorySelection(transaction)
  )
  const [isSaving, setIsSaving] = useState(false)
  const inFlight = useRef(false)
  const [error, setError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()
  const queryClient = useQueryClient()

  // Use generated mutation hooks
  const updateTagsMutation = useUpdateTransactionTags()
  const updateCategoryMutation = useUpdateTransactionCategory()
  const { setReviewed, isPending: isReviewing } = useReviewMutations()

  const hasTagChanges = JSON.stringify(tags) !== JSON.stringify(saved.tags || [])
  const hasCategoryChanges = !categorySelectionEqual(category, seedCategorySelection(saved))
  const hasChanges = hasTagChanges || hasCategoryChanges

  const handleToggleReviewed = async () => {
    if (inFlight.current || hasChanges) return
    inFlight.current = true
    setIsSaving(true)
    setError(null)
    try {
      const next = !saved.reviewed
      await setReviewed(transaction.id, next)
      void invalidateTransactionEdit(queryClient, transaction.id, { reviewHookInvalidated: true })
      // The review hook already started the authoritative refetch. Do not merge
      // an older snapshot afterward or let the parent cancel that refetch.
      const updated = { ...saved, reviewed: next }
      setSaved(updated)
      onSave(updated)
    } catch {
      setError('Failed to update review status. Please try again.')
    } finally {
      inFlight.current = false
      setIsSaving(false)
    }
  }

  const handleSave = async () => {
    if (inFlight.current || !hasChanges) return
    inFlight.current = true
    setIsSaving(true)
    setError(null)
    let didWrite = false
    try {
      // Track the latest version of the transaction after each update
      let updatedTransaction = saved

      // Save tag changes
      if (hasTagChanges) {
        updatedTransaction = (await updateTagsMutation.mutateAsync({
          id: transaction.id,
          data: { tags },
        })) as Transaction
        didWrite = true
        mergeTransactionEdit(queryClient, updatedTransaction)
        setSaved(previous => ({ ...previous, tags: updatedTransaction.tags }))
        setTags(updatedTransaction.tags || [])
      }

      // Save category changes
      if (hasCategoryChanges) {
        updatedTransaction = (await updateCategoryMutation.mutateAsync({
          id: transaction.id,
          data: category,
        })) as Transaction
        didWrite = true
        mergeTransactionEdit(queryClient, updatedTransaction)
        setSaved(updatedTransaction)
        trackEvent(ANALYTICS_EVENTS.TRANSACTION_CATEGORIZED)
      }

      // Cache ownership stays here; parents may update their selection or close.
      onSave(updatedTransaction)
      onClose()
    } catch {
      setError('Failed to save changes. Please try again.')
    } finally {
      if (didWrite) void invalidateTransactionEdit(queryClient, transaction.id)
      inFlight.current = false
      setIsSaving(false)
    }
  }

  if (!isOpen) return null

  const displayName = getMerchantDisplayName(transaction)
  const amount = formatTransactionAmount(transaction.amount)
  function dismiss(): void {
    if (!inFlight.current) onClose()
  }

  return (
    <Dialog open={isOpen} onOpenChange={open => { if (!open) dismiss() }}>
      <DialogContent
        className="flex max-h-[90dvh] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl motion-reduce:animate-none"
        showCloseButton={false}
        onEscapeKeyDown={event => {
          // Radix captures Escape before the picker bubbles it: prevent dialog dismissal,
          // but let the picker handle the event and return focus to its Change button.
          const inCategoryPicker = event.target instanceof Element && event.target.closest('[data-category-picker-panel]')
          if (inFlight.current || inCategoryPicker) event.preventDefault()
        }}
        onInteractOutside={event => { if (inFlight.current) event.preventDefault() }}
        onCloseAutoFocus={event => {
          event.preventDefault()
          if (opener?.isConnected) {
            opener.focus()
            return
          }
          // A successful edit can remove the opener from a filtered list.
          const fallback = document.querySelector<HTMLElement>('main, [role="main"]') ?? document.body
          const hadTabIndex = fallback.hasAttribute('tabindex')
          if (!hadTabIndex) fallback.setAttribute('tabindex', '-1')
          fallback.focus()
          if (!hadTabIndex) fallback.removeAttribute('tabindex')
        }}
      >
        <DialogHeader className="shrink-0 flex-row items-center justify-between px-6 py-4 border-b border-border">
          <DialogTitle>
            Edit Transaction
          </DialogTitle>
          <DialogDescription className="sr-only">View transaction details and edit tags, category, and review status.</DialogDescription>
          <button
            type="button"
            className="flex items-center justify-center w-8 h-8 p-0 border-none bg-transparent text-muted-foreground cursor-pointer text-2xl leading-none rounded hover:bg-muted hover:text-foreground transition-colors"
            onClick={dismiss}
            disabled={isSaving}
            aria-label="Close"
          >
            ×
          </button>
        </DialogHeader>

        <div className="min-h-0 overflow-y-auto">
        <div className="flex items-center justify-between px-6 py-4 bg-muted">
          <div className="text-sm font-medium text-foreground">
            <span data-ph-mask>{displayName}</span>
            {transaction.name !== displayName && <p data-ph-mask className="text-xs text-muted-foreground">{transaction.name}</p>}
            {account && <p data-ph-mask className="text-xs text-muted-foreground">{account.name}</p>}
            <p className="text-xs text-muted-foreground">{formatDate(transaction.date)}</p>
            {transaction.pending && <span className="text-xs">Pending</span>}
          </div>
          <div className={`text-base font-semibold ${amount.signClass}`} data-ph-mask>
            {amount.text}
          </div>
        </div>

        {error && (
          <div className="mx-6 my-0 px-3 py-3 bg-destructive/10 text-destructive rounded-md text-sm" role="alert">
            {error}
          </div>
        )}

        <div className="px-6 py-4 border-b border-border/50">
          <label className="block mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Tags</label>
          <TagEditor
            selectedTags={tags}
            availableTags={availableTags}
            onChange={setTags}
            onTagCreated={onTagCreated}
            disabled={isSaving}
          />
        </div>

        <div className="px-6 py-4 border-b border-border/50">
          <label className="block mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Category</label>
          <CategoryEditor
            selectedCategory={category}
            currentCategory={getEffectiveCategory(saved)}
            bankCategory={getEffectiveCategory({ ...transaction, category_override_kind: null })}
            availableCategories={availableCategories}
            plaidPrimaries={plaidPrimaries}
            onChange={setCategory}
            onCategoryCreated={onCategoryCreated}
            disabled={isSaving}
          />
        </div>

        <div className="px-6 py-4 border-b border-border/50">
          <label className="block mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Review
          </label>
          <div className="flex items-center justify-between">
            <ReviewBadge
              variant={reviewBadgeVariantFromTransaction(saved)}
              data-testid="review-status-badge"
            />
            <ReadOnlyGuard message="Resubscribe to review transactions">
              <button
                type="button"
                onClick={handleToggleReviewed}
                disabled={isSaving || isReviewing || hasChanges}
                className="px-3 py-1.5 border border-input rounded-md bg-card text-foreground text-sm font-medium cursor-pointer hover:bg-muted transition-all disabled:cursor-not-allowed disabled:opacity-50"
              >
                {saved.reviewed ? 'Mark unreviewed' : 'Mark reviewed'}
              </button>
            </ReadOnlyGuard>
          </div>
        </div>

        </div>
        <DialogFooter className="shrink-0 border-t border-border px-6 py-4">
          <button
            type="button"
            className="px-4 py-2 border border-input rounded-md bg-card text-foreground text-sm font-medium cursor-pointer hover:bg-muted transition-all disabled:cursor-not-allowed disabled:opacity-50"
            onClick={dismiss}
            disabled={isSaving}
          >
            Cancel
          </button>
          <ReadOnlyGuard message="Resubscribe to edit transactions">
            <button
              type="button"
              className="px-4 py-2 border-none rounded-md bg-primary text-primary-foreground text-sm font-medium cursor-pointer hover:opacity-90 transition-opacity disabled:cursor-not-allowed disabled:opacity-50"
              onClick={handleSave}
              disabled={isSaving || !hasChanges}
            >
              {isSaving ? 'Saving...' : 'Save'}
            </button>
          </ReadOnlyGuard>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
