/**
 * TriageDetailView — one-at-a-time keyboard triage for the review inbox.
 *
 * Keyboard shortcuts (see KeyboardShortcutsHelp):
 *   e / Enter → approve (mark reviewed, advance)
 *   s         → skip (advance without marking)
 *   c         → open recategorize
 *   x         → toggle row selection
 *   →/n       → next
 *   ←/p       → previous
 *   ?         → keyboard help dialog
 *   Escape    → close triage
 *   ⌘Z/Ctrl+Z → undo last action (when onUndo provided)
 *
 * Amounts are masked with data-ph-mask for PostHog session replay PII compliance.
 */

import { useState, useCallback, useMemo, useRef, useEffect } from 'react'
import type { Account } from '@/features/accounts/types'
import { getCategoryPresentation } from '@/shared/categories/getCategoryPresentation'
import { formatTransactionAmount } from '@/shared/transactions/amount'
import { getEffectiveCategory } from '@/shared/categories/getEffectiveCategory'
import { categorySelectionEqual, seedCategorySelection, type CategorySelection } from '@/shared/categories/categorySelection'
import { ChevronLeft, ChevronRight, X, HelpCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { ReadOnlyGuard } from '@/auth'
import { ReviewBadge, reviewBadgeVariantFromTransaction } from '@/shared/components/ReviewBadge'
import { CategoryEditor } from '../../../categories/components/CategoryEditor'
import { useTriageKeyboard } from '../hooks/useTriageKeyboard'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'
import { getMerchantDisplayName } from '@/shared/transactions/merchant'
import { formatDate } from '@/shared/transactions/dates'
import type { Transaction } from '../../types'
import type { CustomCategory } from '../../../categories/types'
import type { Primary } from '@boostedbudget/api-client/models/primary'

const KEYBOARD_SHORTCUT_CLASS =
  'ml-1 hidden rounded border border-border px-1 text-[10px] text-muted-foreground opacity-70 sm:inline'

interface TriageDetailViewProps {
  transaction: Transaction
  account?: Pick<Account, 'name' | 'nickname'>
  onEdit?: () => void
  onInteractionBusyChange?: (busy: boolean) => void
  currentIndex: number
  total: number
  availableCategories?: CustomCategory[]
  plaidPrimaries?: Primary[]
  onApprove: () => void
  onSkip: () => void
  onRecategorize: (categoryRequest: CategorySelection) => void | Promise<void>
  onToggleSelect: () => void
  onNext: () => void
  onPrev: () => void
  onClose: () => void
  onUndo?: () => void
  isSelected?: boolean
  isPending?: boolean
  externalOverlayOpen?: boolean
}

export function TriageDetailView({
  transaction,
  account,
  onEdit,
  onInteractionBusyChange,
  currentIndex,
  total,
  availableCategories = [],
  plaidPrimaries = [],
  onApprove,
  onSkip,
  onRecategorize,
  onToggleSelect,
  onNext,
  onPrev,
  onClose,
  onUndo,
  isSelected = false,
  isPending = false,
  externalOverlayOpen = false,
}: TriageDetailViewProps) {
  const [showHelp, setShowHelp] = useState(false)
  const [showRecategorize, setShowRecategorize] = useState(false)

  const seedCategory = useMemo(() => seedCategorySelection(transaction), [transaction])
  const [pendingCategory, setPendingCategory] = useState<CategorySelection>(seedCategory)
  const [categoryError, setCategoryError] = useState(false)
  const [savingCategory, setSavingCategory] = useState(false)
  const categoryInFlight = useRef(false)
  const blocked = isPending || savingCategory || externalOverlayOpen || showHelp || showRecategorize
  useEffect(() => {
    onInteractionBusyChange?.(showHelp || showRecategorize || savingCategory)
    return () => onInteractionBusyChange?.(false)
  }, [showHelp, showRecategorize, savingCategory, onInteractionBusyChange])

  // Re-seed when the triage queue advances to a different transaction under an open panel.
  // Adjusting state during render (rather than in an effect) is the React-recommended form and
  // avoids briefly showing the previous transaction's category.
  const [seededForTxId, setSeededForTxId] = useState(transaction.id)
  if (seededForTxId !== transaction.id) {
    setSeededForTxId(transaction.id)
    setPendingCategory(seedCategory)
  }

  const handleToggleHelp = useCallback(() => setShowHelp((v) => !v), [])
  const handleRecategorize = useCallback(() => {
    setShowRecategorize(true)
    setCategoryError(false)
    setPendingCategory(seedCategory)
  }, [seedCategory])
  const handleClose = useCallback(() => {
    if (showHelp) {
      setShowHelp(false)
    } else if (showRecategorize) {
      setShowRecategorize(false)
    } else {
      onClose()
    }
  }, [showHelp, showRecategorize, onClose])

  useTriageKeyboard({
    onApprove,
    onSkip,
    onRecategorize: handleRecategorize,
    onToggleSelect,
    onNext,
    onPrev,
    onToggleHelp: handleToggleHelp,
    onClose: handleClose,
    onUndo,
    // Disable shortcuts when any overlay is open or a mutation is in flight
    enabled: !blocked,
  })

  const displayName = getMerchantDisplayName(transaction)
  const amount = formatTransactionAmount(transaction.amount)
  const category = getCategoryPresentation(getEffectiveCategory(transaction), availableCategories)
  const hasPrev = currentIndex > 0
  const hasNext = currentIndex < total - 1

  const handleSaveCategory = async () => {
    if (categoryInFlight.current || isPending || categorySelectionEqual(seedCategory, pendingCategory)) return
    categoryInFlight.current = true
    setCategoryError(false)
    setSavingCategory(true)
    try {
      await onRecategorize(pendingCategory)
      setShowRecategorize(false)
    } catch { setCategoryError(true) }
    finally { categoryInFlight.current = false; setSavingCategory(false) }
  }

  return (
    <>
      <Card className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onPrev}
              disabled={!hasPrev || blocked}
              aria-label="Previous transaction"
            >
              <ChevronLeft />
            </Button>
            <span className="text-xs text-muted-foreground tabular-nums">
              {currentIndex + 1} / {total}
            </span>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onNext}
              disabled={!hasNext || blocked}
              aria-label="Next transaction"
            >
              <ChevronRight />
            </Button>
          </div>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleToggleHelp}
              disabled={blocked}
              aria-label="Keyboard shortcuts"
              aria-pressed={showHelp}
            >
              <HelpCircle />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onClose}
              disabled={blocked}
              aria-label="Close triage"
            >
              <X />
            </Button>
          </div>
        </div>

        <div className="space-y-1">
          <div className="flex items-start justify-between gap-3">
            <p className="font-medium text-foreground" data-ph-mask>
              {displayName}
            </p>
            <p className={`shrink-0 font-semibold ${amount.signClass}`} data-ph-mask>
              {amount.text}
            </p>
          </div>
          {transaction.name !== displayName && <p className="text-sm text-muted-foreground" data-ph-mask>{transaction.name}</p>}
          <p className="text-sm text-muted-foreground" data-ph-mask>{account?.nickname || account?.name || 'Unknown account'}</p>
          <p className="text-sm" data-ph-mask>{category.breadcrumb}</p>
          {!!transaction.tags?.length && <div className="flex flex-wrap gap-1" data-ph-mask>{transaction.tags.map(tag => <span key={tag} className="rounded bg-muted px-2 text-xs">{tag}</span>)}</div>}
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">{formatDate(transaction.date)}</span>
            {transaction.pending && <span className="text-xs text-muted-foreground">Pending</span>}
            <ReviewBadge variant={reviewBadgeVariantFromTransaction(transaction)} />
            {isSelected && (
              <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                Selected
              </span>
            )}
          </div>
        </div>

        {showRecategorize && (
          <div className="rounded-lg border border-border p-3">
            <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              Recategorize
            </p>
            <CategoryEditor
              selectedCategory={pendingCategory}
              currentCategory={getEffectiveCategory(transaction)}
              bankCategory={getEffectiveCategory({ ...transaction, category_override_kind: null })}
              disabled={isPending || savingCategory}
              availableCategories={availableCategories}
              plaidPrimaries={plaidPrimaries}
              onChange={setPendingCategory}
            />
            {categoryError && <p role="alert" className="mt-2 text-sm text-destructive">Failed to recategorize. Please try again.</p>}
            <div className="mt-3 flex justify-end gap-2">
              <Button variant="outline" size="sm" disabled={isPending || savingCategory} onClick={() => setShowRecategorize(false)}>
                Cancel
              </Button>
              <ReadOnlyGuard message="Resubscribe to categorize transactions">
                <Button size="sm" onClick={handleSaveCategory} disabled={isPending || savingCategory || categorySelectionEqual(seedCategory, pendingCategory)}>
                  Apply
                </Button>
              </ReadOnlyGuard>
            </div>
          </div>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <ReadOnlyGuard message="Resubscribe to review transactions">
            <Button
              size="sm"
              onClick={onApprove}
              disabled={blocked}
              aria-label="Approve: mark reviewed and advance"
            >
              Approve
              <span className="ml-1 hidden rounded border border-primary-foreground/30 px-1 text-[10px] opacity-70 sm:inline">
                E
              </span>
            </Button>
          </ReadOnlyGuard>

          <Button
            variant="outline"
            size="sm"
            onClick={handleRecategorize}
            disabled={blocked}
            aria-label="Recategorize transaction"
          >
            Recategorize
            <span className={KEYBOARD_SHORTCUT_CLASS}>
              C
            </span>
          </Button>

          <Button
            variant="ghost"
            size="sm"
            onClick={onSkip}
            disabled={blocked}
            aria-label="Skip: advance without marking"
          >
            Skip
            <span className={KEYBOARD_SHORTCUT_CLASS}>
              S
            </span>
          </Button>

          <Button
            variant="ghost"
            size="sm"
            onClick={onToggleSelect}
            disabled={blocked}
            aria-pressed={isSelected}
            aria-label={isSelected ? 'Deselect transaction' : 'Select transaction for bulk action'}
          >
            {isSelected ? 'Deselect' : 'Select'}
            <span className={KEYBOARD_SHORTCUT_CLASS}>
              X
            </span>
          </Button>
          {onEdit && <Button variant="outline" size="sm" onClick={onEdit} disabled={blocked}>Edit details</Button>}
        </div>
      </Card>

      <KeyboardShortcutsHelp open={showHelp} onOpenChange={setShowHelp} />
    </>
  )
}
