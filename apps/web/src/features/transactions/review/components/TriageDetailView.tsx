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

import { useState, useCallback, useMemo } from 'react'
import { effectiveCategoryToStringArray } from '@/shared/categories/getEffectiveCategory'
import { ChevronLeft, ChevronRight, X, HelpCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { ReadOnlyGuard } from '@/auth'
import { ReviewBadge, reviewBadgeVariantFromTransaction } from '@/shared/components/ReviewBadge'
import { CategoryEditor } from '../../../categories/components/CategoryEditor'
import { useTriageKeyboard } from '../hooks/useTriageKeyboard'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'
import { getMerchantDisplayName } from '@/shared/transactions/merchant'
import type { Transaction } from '../../types'
import type { CustomCategory } from '../../../categories/types'
import type { Primary } from '@boostedbudget/api-client/models/primary'

const CURRENCY_FORMATTER = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
})

const KEYBOARD_SHORTCUT_CLASS =
  'ml-1 hidden rounded border border-border px-1 text-[10px] text-muted-foreground opacity-70 sm:inline'

interface TriageDetailViewProps {
  transaction: Transaction
  currentIndex: number
  total: number
  availableCategories?: CustomCategory[]
  plaidPrimaries?: Primary[]
  onApprove: () => void
  onSkip: () => void
  onRecategorize: (categoryRequest: { kind: 'pfc' | 'custom' | 'clear'; pfc_primary?: string; pfc_detailed?: string; custom_category_id?: string }) => void
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

  // #1625: seed from the transaction's effective category. Opening with [] rendered a
  // well-categorized transaction as "Uncategorized" and, because handleSaveCategory reads an
  // empty selection as { kind: 'clear' }, pressing Apply without touching it DESTROYED the
  // category. TransactionEditor has always seeded this way; triage never did.
  const seedCategory = useMemo(() => effectiveCategoryToStringArray(transaction), [transaction])
  const [pendingCategory, setPendingCategory] = useState<string[]>(seedCategory)

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
    enabled: !showHelp && !showRecategorize && !isPending && !externalOverlayOpen,
  })

  const displayName = getMerchantDisplayName(transaction)
  const hasPrev = currentIndex > 0
  const hasNext = currentIndex < total - 1

  const handleSaveCategory = () => {
    // #1625: the selection can arrive in EITHER convention — a display name (the user picked
    // from the dropdown) or a raw UUID / PFC enum (seeded from the transaction). Match both, or
    // an untouched custom category would be sent as `pfc_primary: "<uuid>"`.
    const selected = pendingCategory[0]
    const customCat = availableCategories.find((c) => c.id === selected || c.name === selected)
    const plaidMatch = !customCat
      ? plaidPrimaries?.find((p) => p.enum === selected || p.display === selected)
      : null
    const request =
      pendingCategory.length === 0
        ? { kind: 'clear' as const }
        : customCat
          ? { kind: 'custom' as const, custom_category_id: customCat.id }
          : { kind: 'pfc' as const, pfc_primary: plaidMatch?.enum ?? selected, pfc_detailed: pendingCategory[1] }
    onRecategorize(request)
    setShowRecategorize(false)
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
              disabled={!hasPrev || isPending}
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
              disabled={!hasNext || isPending}
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
              aria-label="Keyboard shortcuts"
              aria-pressed={showHelp}
            >
              <HelpCircle />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onClose}
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
            <p className="shrink-0 font-semibold text-foreground" data-ph-mask>
              {CURRENCY_FORMATTER.format(Math.abs(transaction.amount))}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">{transaction.date}</span>
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
              availableCategories={availableCategories}
              plaidPrimaries={plaidPrimaries}
              onChange={setPendingCategory}
            />
            <div className="mt-3 flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setShowRecategorize(false)}>
                Cancel
              </Button>
              <ReadOnlyGuard message="Resubscribe to categorize transactions">
                <Button size="sm" onClick={handleSaveCategory} disabled={isPending}>
                  Apply
                </Button>
              </ReadOnlyGuard>
            </div>
          </div>
        )}

        <div className="flex items-center gap-2">
          <ReadOnlyGuard message="Resubscribe to review transactions">
            <Button
              size="sm"
              onClick={onApprove}
              disabled={isPending}
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
            disabled={isPending}
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
            disabled={isPending}
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
            aria-pressed={isSelected}
            aria-label={isSelected ? 'Deselect transaction' : 'Select transaction for bulk action'}
          >
            {isSelected ? 'Deselect' : 'Select'}
            <span className={KEYBOARD_SHORTCUT_CLASS}>
              X
            </span>
          </Button>
        </div>
      </Card>

      <KeyboardShortcutsHelp open={showHelp} onOpenChange={setShowHelp} />
    </>
  )
}
