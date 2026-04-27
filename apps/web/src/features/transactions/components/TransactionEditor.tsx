import { useState } from 'react'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { ReadOnlyGuard } from '@/auth'
import type { Transaction } from '../types'
import type { UserTag } from '../../tags/types'
import type { CustomCategory } from '../../categories/types'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import { TagEditor } from '../../tags/components/TagEditor'
import { CategoryEditor } from '../../categories/components/CategoryEditor'
import { getEffectiveCategory } from '@/shared/categories/getEffectiveCategory'
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

/**
 * Get display name for original category from raw JSON string.
 */
function getOriginalCategoryName(originalCategory: string | null | undefined): string | null {
  if (!originalCategory) return null
  try {
    const parsed = JSON.parse(originalCategory)
    if (Array.isArray(parsed) && parsed.length > 0) return parsed[0] as string
  } catch {
    // ignore malformed JSON
  }
  return null
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
  const [category, setCategory] = useState<string[]>(
    getEffectiveCategory(transaction)
  )
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()

  // Use generated mutation hooks
  const updateTagsMutation = useUpdateTransactionTags()
  const updateCategoryMutation = useUpdateTransactionCategory()

  const initialCategory = getEffectiveCategory(transaction)
  const initialTags = transaction.tags || []

  const hasTagChanges = JSON.stringify(tags) !== JSON.stringify(initialTags)
  const hasCategoryChanges = JSON.stringify(category) !== JSON.stringify(initialCategory)
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
        updatedTransaction = (await updateCategoryMutation.mutateAsync({
          id: transaction.id,
          data: { category },
        })) as Transaction
        trackEvent(ANALYTICS_EVENTS.TRANSACTION_CATEGORIZED, { category: category[0] ?? null })
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

  const displayName = transaction.merchant_name || transaction.name

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
          <h2 id="transaction-editor-title" className="m-0 text-lg font-semibold text-foreground">Edit Transaction</h2>
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
          <div className="text-sm font-medium text-foreground">{displayName}</div>
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
          {transaction.original_category && (
            <button
              type="button"
              className="mt-2 px-2 py-1 border border-input rounded bg-muted text-muted-foreground text-xs cursor-pointer hover:bg-accent hover:border-border transition-all disabled:cursor-not-allowed disabled:opacity-50"
              onClick={() => {
                try {
                  const parsed = JSON.parse(transaction.original_category!)
                  if (Array.isArray(parsed)) setCategory(parsed as string[])
                } catch { /* ignore malformed JSON */ }
              }}
              disabled={isSaving}
              aria-label={`Reset to original: ${getOriginalCategoryName(transaction.original_category)}`}
            >
              Reset to Original ({getOriginalCategoryName(transaction.original_category)})
            </button>
          )}
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
