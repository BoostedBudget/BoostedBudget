import { useState } from 'react'
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
import { effectiveCategoryToStringArray } from '@/shared/categories/getEffectiveCategory'
import { getMerchantDisplayName } from '@/shared/transactions/merchant'
import { ReviewBadge, reviewBadgeVariantFromTransaction } from '@/shared/components/ReviewBadge'
import {
  useUpdateTransactionTags,
  useUpdateTransactionCategory,
} from '@boostedbudget/api-client/endpoints/transactions/transactions'

interface TransactionEditorProps {
  transaction: Transaction
  availableTags: UserTag[]
  availableCategories: CustomCategory[]
  plaidPrimaries?: Primary[]
  onClose: () => void
  onSave: (updatedTransaction: Transaction) => void
  onTagCreated?: (tag: UserTag) => void
  onCategoryCreated?: (category: CustomCategory) => void
  isOpen: boolean
}

export function TransactionEditor({
  transaction,
  availableTags,
  availableCategories,
  plaidPrimaries,
  onClose,
  onSave,
  onTagCreated,
  onCategoryCreated,
  isOpen,
}: TransactionEditorProps) {
  const [tags, setTags] = useState<string[]>(transaction.tags || [])
  const [category, setCategory] = useState<string[]>(() =>
    effectiveCategoryToStringArray(transaction)
  )
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()
  const queryClient = useQueryClient()

  // Use generated mutation hooks
  const updateTagsMutation = useUpdateTransactionTags()
  const updateCategoryMutation = useUpdateTransactionCategory()
  const { setReviewed, isPending: isReviewing } = useReviewMutations()

  const handleToggleReviewed = async () => {
    setError(null)
    try {
      const next = !transaction.reviewed
      await setReviewed(transaction.id, next)
      onSave({ ...transaction, reviewed: next })
    } catch {
      setError('Failed to update review status. Please try again.')
    }
  }

  const hasTagChanges = JSON.stringify(tags) !== JSON.stringify(transaction.tags || [])
  const hasCategoryChanges = JSON.stringify(category) !== JSON.stringify(effectiveCategoryToStringArray(transaction))
  const hasChanges = hasTagChanges || hasCategoryChanges

  const handleSave = async () => {
    setIsSaving(true)
    setError(null)

    try {
      // Track the latest version of the transaction after each update
      let updatedTransaction = transaction

      // Save tag changes
      if (hasTagChanges) {
        updatedTransaction = (await updateTagsMutation.mutateAsync({
          id: transaction.id,
          data: { tags },
        })) as Transaction
      }

      // Save category changes
      if (hasCategoryChanges) {
        const customCat = availableCategories.find((c) => c.name === category[0])
        // CategoryEditor emits display names for Plaid primaries -- resolve back to enum
        const plaidMatch = !customCat ? plaidPrimaries?.find((p) => p.display === category[0]) : null
        const categoryRequest = category.length === 0
          ? { kind: 'clear' as const }
          : customCat
            ? { kind: 'custom' as const, custom_category_id: customCat.id }
            : { kind: 'pfc' as const, pfc_primary: plaidMatch?.enum ?? category[0], pfc_detailed: category[1] }
        updatedTransaction = (await updateCategoryMutation.mutateAsync({
          id: transaction.id,
          data: categoryRequest,
        })) as Transaction
        queryClient.invalidateQueries({ queryKey: ['review-summary'] })
        trackEvent(ANALYTICS_EVENTS.TRANSACTION_CATEGORIZED)
      }

      // Pass the updated transaction to parent for immediate state update
      onSave(updatedTransaction)
      onClose()
    } catch {
      setError('Failed to save changes. Please try again.')
    } finally {
      setIsSaving(false)
    }
  }

  if (!isOpen) return null

  const displayName = getMerchantDisplayName(transaction)

  return (
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center z-[1000]"
      onClick={onClose}
    >
      <div
        className="bg-card rounded-lg shadow-xl w-full max-w-md max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-labelledby="transaction-editor-title"
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <h2 id="transaction-editor-title" className="m-0 text-lg font-semibold text-foreground">
            Edit Transaction
          </h2>
          <button
            type="button"
            className="flex items-center justify-center w-8 h-8 p-0 border-none bg-transparent text-muted-foreground cursor-pointer text-2xl leading-none rounded hover:bg-muted hover:text-foreground transition-colors"
            onClick={onClose}
            aria-label="Close"
          >
            ×
          </button>
        </div>

        <div className="flex items-center justify-between px-6 py-4 bg-muted">
          <div className="text-sm font-medium text-foreground">
            <span data-ph-mask>{displayName}</span>
          </div>
          <div className="text-base font-semibold text-foreground" data-ph-mask>
            {new Intl.NumberFormat('en-US', {
              style: 'currency',
              currency: 'USD',
              minimumFractionDigits: 2,
            }).format(Math.abs(transaction.amount))}
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
              variant={reviewBadgeVariantFromTransaction(transaction)}
              data-testid="review-status-badge"
            />
            <ReadOnlyGuard message="Resubscribe to review transactions">
              <button
                type="button"
                onClick={handleToggleReviewed}
                disabled={isReviewing}
                className="px-3 py-1.5 border border-input rounded-md bg-card text-foreground text-sm font-medium cursor-pointer hover:bg-muted transition-all disabled:cursor-not-allowed disabled:opacity-50"
              >
                {transaction.reviewed ? 'Mark unreviewed' : 'Mark reviewed'}
              </button>
            </ReadOnlyGuard>
          </div>
        </div>

        <div className="flex justify-end gap-3 px-6 py-4">
          <button
            type="button"
            className="px-4 py-2 border border-input rounded-md bg-card text-foreground text-sm font-medium cursor-pointer hover:bg-muted transition-all disabled:cursor-not-allowed disabled:opacity-50"
            onClick={onClose}
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
        </div>
      </div>
    </div>
  )
}
