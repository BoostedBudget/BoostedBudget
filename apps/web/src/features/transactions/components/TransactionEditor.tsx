import { useState } from 'react'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import type { Transaction } from '../types'
import type { UserTag } from '../../tags/types'
import type { CustomCategory } from '../../categories/types'
import { TagEditor } from '../../tags/components/TagEditor'
import { CategoryEditor } from '../../categories/components/CategoryEditor'
import {
  useUpdateTransactionTags,
  useUpdateTransactionCategory,
} from '@boostedbudget/api-client/endpoints/transactions/transactions'
import './TransactionEditor.css'

interface TransactionEditorProps {
  transaction: Transaction
  availableTags: UserTag[]
  availableCategories: CustomCategory[]
  onClose: () => void
  onSave: (updatedTransaction: Transaction) => void
  onTagCreated?: (tag: UserTag) => void
  onCategoryCreated?: (category: CustomCategory) => void
  isOpen: boolean
}

/**
 * Parse category from JSON string or array
 */
function parseCategory(categoryJson: string | null | undefined): string[] {
  if (!categoryJson) return []

  try {
    if (typeof categoryJson === 'string') {
      const parsed = JSON.parse(categoryJson)
      if (Array.isArray(parsed)) return parsed
    }
    return []
  } catch {
    return []
  }
}

/**
 * Get display name for original category
 */
function getOriginalCategoryName(originalCategory: string | null | undefined): string | null {
  const parsed = parseCategory(originalCategory)
  return parsed.length > 0 ? parsed[0] : null
}

/**
 * Format amount for display
 */
function formatAmount(amount: number): string {
  const absAmount = Math.abs(amount)
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
  }).format(absAmount)
}

export function TransactionEditor({
  transaction,
  availableTags,
  availableCategories,
  onClose,
  onSave,
  onTagCreated,
  onCategoryCreated,
  isOpen,
}: TransactionEditorProps) {
  const [tags, setTags] = useState<string[]>(transaction.tags || [])
  const [category, setCategory] = useState<string[]>(
    transaction.category_override
      ? parseCategory(transaction.category_override)
      : parseCategory(transaction.category)
  )
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()

  // Use generated mutation hooks
  const updateTagsMutation = useUpdateTransactionTags()
  const updateCategoryMutation = useUpdateTransactionCategory()

  const initialTags = transaction.tags || []
  const initialCategory = transaction.category_override
    ? parseCategory(transaction.category_override)
    : parseCategory(transaction.category)

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
    <div className="transaction-editor-overlay" onClick={onClose}>
      <div
        className="transaction-editor-modal"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-labelledby="transaction-editor-title"
      >
        <div className="transaction-editor__header">
          <h2 id="transaction-editor-title">Edit Transaction</h2>
          <button
            type="button"
            className="transaction-editor__close"
            onClick={onClose}
            aria-label="Close"
          >
            ×
          </button>
        </div>

        <div className="transaction-editor__transaction-info">
          <div className="transaction-editor__name">{displayName}</div>
          <div className="transaction-editor__amount">
            {formatAmount(transaction.amount)}
          </div>
        </div>

        {error && (
          <div className="transaction-editor__error" role="alert">
            {error}
          </div>
        )}

        <div className="transaction-editor__section">
          <label className="transaction-editor__label">Tags</label>
          <TagEditor
            selectedTags={tags}
            availableTags={availableTags}
            onChange={setTags}
            onTagCreated={onTagCreated}
            disabled={isSaving}
          />
        </div>

        <div className="transaction-editor__section">
          <label className="transaction-editor__label">Category</label>
          <CategoryEditor
            selectedCategory={category}
            availableCategories={availableCategories}
            onChange={setCategory}
            onCategoryCreated={onCategoryCreated}
            disabled={isSaving}
          />
          {transaction.original_category && (
            <button
              type="button"
              className="transaction-editor__reset-category"
              onClick={() => setCategory(parseCategory(transaction.original_category))}
              disabled={isSaving}
              aria-label={`Reset to original: ${getOriginalCategoryName(transaction.original_category)}`}
            >
              Reset to Original ({getOriginalCategoryName(transaction.original_category)})
            </button>
          )}
        </div>

        <div className="transaction-editor__actions">
          <button
            type="button"
            className="transaction-editor__cancel"
            onClick={onClose}
            disabled={isSaving}
          >
            Cancel
          </button>
          <button
            type="button"
            className="transaction-editor__save"
            onClick={handleSave}
            disabled={isSaving || !hasChanges}
          >
            {isSaving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}
