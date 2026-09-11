import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import { ReadOnlyGuard, useReadOnly } from '@/auth'
import { TransactionBudgetBadge } from '../../budgets/components/transaction-link'
import type { BudgetCategoryMatch } from '../../budgets/types'
import { getCategoryColor } from '@/shared/categories/pfcColors'
import { getEffectiveCategory, getEffectiveCategoryLabel } from '@/shared/categories/getEffectiveCategory'
import { formatTransactionAmount } from '@/shared/transactions/amount'
import { getMerchantDisplayName } from '@/shared/transactions/merchant'
import { ArrowUpRight, ArrowDownRight, Clock, Pencil, Tag } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { ReviewBadge } from '@/shared/components/ReviewBadge'
import { reviewBadgeVariantFromTransaction } from '@/shared/components/ReviewBadge/reviewBadgeUtils'

interface TransactionCardProps {
  transaction: Transaction
  /** Account this transaction belongs to (for context display) */
  account?: Account | null
  onEdit?: (transaction: Transaction) => void
  /** Called when user clicks categorize quick action */
  onCategorize?: (transaction: Transaction) => void
  /** Issue #499: Budget category match for displaying budget badge */
  budgetMatch?: BudgetCategoryMatch | null
}

/**
 * Format date with relative labels (Today, Yesterday) or readable format.
 * Uses local timezone for consistent comparison across environments.
 * Handles both YYYY-MM-DD and RFC3339 (YYYY-MM-DDTHH:MM:SSZ) formats.
 */
function formatDate(dateStr: string | null | undefined): string {
  if (!dateStr || typeof dateStr !== 'string') return 'Invalid Date'

  const datePortion = dateStr.split('T')[0]
  if (!/^\d{4}-\d{2}-\d{2}$/.test(datePortion)) return 'Invalid Date'

  const [year, month, day] = datePortion.split('-').map(Number)
  if (isNaN(year) || isNaN(month) || isNaN(day)) return 'Invalid Date'

  const transactionDate = new Date(year, month - 1, day)
  if (isNaN(transactionDate.getTime())) return 'Invalid Date'

  const today = new Date()
  today.setHours(0, 0, 0, 0)
  transactionDate.setHours(0, 0, 0, 0)

  const timeDiff = transactionDate.getTime() - today.getTime()
  if (timeDiff === 0) return 'Today'

  const yesterday = new Date(today)
  yesterday.setDate(yesterday.getDate() - 1)
  if (timeDiff === -1 * 24 * 60 * 60 * 1000) return 'Yesterday'

  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }).format(transactionDate)
}

export function TransactionCard({ transaction, account, onEdit, onCategorize, budgetMatch }: TransactionCardProps) {
  const { isReadOnly } = useReadOnly()
  const { id, amount, date, authorized_date, tags, pending } = transaction

  const handleEdit = (e?: React.MouseEvent) => {
    if (e) e.stopPropagation()
    if (!isReadOnly) onEdit?.(transaction)
  }

  const displayDate = formatDate(authorized_date || date)
  const displayName = getMerchantDisplayName(transaction)
  const effective = getEffectiveCategory(transaction)
  const categoryLabel = getEffectiveCategoryLabel(effective)
  const showUncategorizedWarning = categoryLabel === 'Uncategorized'
  const { text: amountFormatted, signClass: amountClass, arrow, testId: arrowTestId } = formatTransactionAmount(amount)
  const ariaLabel = `Transaction, ${displayDate}${pending ? ', pending' : ''}`
  const categoryColor = getCategoryColor(categoryLabel)
  const accountDisplayName = account ? (account.nickname || account.name) : null
  const accountBackgroundColor = account?.custom_color || '#E5E7EB'

  const handleCategorize = (e: React.MouseEvent) => {
    e.stopPropagation()
    onCategorize?.(transaction)
  }

  return (
    <article
      className={cn(
        'relative bg-card border border-border rounded-lg p-4 mb-3 transition-shadow',
        // #1569 task 20 (A3): the `bg-warning/10` half is removed. The card DOES paint its own
        // bg-card, so the tint composited onto it and dropped text-muted-foreground to 4.38:1 —
        // the same failure the table row had, on the mobile surface. A left rule marks the card
        // instead; it changes no text's ground. See TransactionTable's rowClassName for the full
        // argument, including why tuning the alpha was rejected.
        pending && 'border-warning/40 border-l-4 border-l-warning',
        onEdit && 'cursor-pointer hover:shadow-md focus:outline-2 focus:outline-ring focus:outline-offset-2 active:scale-[0.99]'
      )}
      data-transaction-id={id}
      data-pending={pending}
      data-pfc-primary={transaction.pfc_primary || undefined}
      aria-label={ariaLabel}
      onClick={onEdit ? handleEdit : undefined}
      onKeyDown={onEdit ? (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          handleEdit()
        }
      } : undefined}
      role={onEdit ? 'button' : undefined}
      tabIndex={onEdit ? 0 : undefined}
    >
      <div className="flex justify-between items-start gap-4">
        <div className="flex-1 min-w-0">
          <div className="text-base font-semibold text-foreground mb-1 overflow-hidden text-ellipsis whitespace-nowrap"><span data-ph-mask>{displayName}</span></div>
          {/* Issue #531: Account badge */}
          {/* Test-contract CSS classes: transaction-card__account / __meta / __category /
              __category-dot / __category-icon / __uncategorized-warning are queried by
              TransactionCard.test.tsx via container.querySelector() AND by
              e2e/transaction-visual-enhancements.auth.spec.ts. Do not remove — see commits
              d8c6bce6 (#835) and this commit for prior restorations after Tailwind/PII
              migrations. transaction-card__account-badge is also part of this contract. */}
          {account && (
            <span
              className="transaction-card__account transaction-card__account-badge inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium text-white"
              style={{ backgroundColor: accountBackgroundColor }}
            >
              {account.custom_emoji && <span>{account.custom_emoji}</span>}
              <span data-ph-mask>{accountDisplayName}</span>
            </span>
          )}
          <div className="transaction-card__meta flex gap-3 text-sm text-muted-foreground mt-1 flex-wrap">
            <ReviewBadge variant={reviewBadgeVariantFromTransaction(transaction)} className="text-xs" />
            <TransactionBudgetBadge match={budgetMatch} />
            <span className="transaction-card__category inline-flex items-center gap-1">
              {transaction.pfc_category_icon_url ? (
                <img
                  src={transaction.pfc_category_icon_url}
                  alt=""
                  className="transaction-card__category-icon w-4 h-4 shrink-0 mr-0.5"
                  aria-hidden="true"
                />
              ) : (
                <span
                  className="transaction-card__category-dot w-2 h-2 rounded-full shrink-0 mr-1"
                  style={{ backgroundColor: categoryColor }}
                />
              )}
              {showUncategorizedWarning && (
                <span className="transaction-card__uncategorized-warning" title="Uncategorized">⚠️</span>
              )}
              {categoryLabel}
            </span>
            <span>{displayDate}</span>
          </div>
          {tags && tags.length > 0 && (
            <div className="transaction-card__tags flex flex-wrap gap-1 mt-1.5" data-testid="transaction-tags">
              {tags.map((tag) => (
                <span key={tag} data-ph-mask className="inline-block text-[11px] font-medium text-foreground bg-muted px-2 py-0.5 rounded-full whitespace-nowrap">
                  {tag}
                </span>
              ))}
            </div>
          )}
        </div>
        <div className="flex flex-col items-end gap-1">
          <div className={cn('text-lg font-bold whitespace-nowrap inline-flex items-center', amountClass)} data-ph-mask>
            {arrow === 'down' ? (
              <ArrowDownRight className="w-4 h-4 mr-0.5 inline-block align-middle" data-testid={arrowTestId} />
            ) : arrow === 'up' ? (
              <ArrowUpRight className="w-4 h-4 mr-0.5 inline-block align-middle" data-testid={arrowTestId} />
            ) : null}
            {amountFormatted}
          </div>
          {pending && (
            <Badge
              variant="warning"
              className="transaction-card__pending-badge uppercase tracking-wide"
              title="This transaction is still pending and may be updated by your bank"
            >
              <Clock className="w-3 h-3" data-testid="pending-icon" />
              Pending
            </Badge>
          )}
        </div>
        {(onEdit || onCategorize) && (
          <div className="transaction-card__quick-actions hidden absolute right-4 top-1/2 -translate-y-1/2 gap-1 group-hover:flex [article:hover_&]:flex [article:focus-within_&]:flex">
            {onEdit && (
              <ReadOnlyGuard message="Resubscribe to edit transactions">
                <button
                  className="inline-flex items-center justify-center w-7 h-7 border border-border rounded-md bg-card text-muted-foreground cursor-pointer transition-all hover:bg-muted hover:text-foreground hover:border-input focus:outline-2 focus:outline-ring focus:outline-offset-2"
                  onClick={handleEdit}
                  aria-label="Edit transaction"
                  type="button"
                >
                  <Pencil className="w-3.5 h-3.5" />
                </button>
              </ReadOnlyGuard>
            )}
            {onCategorize && (
              <ReadOnlyGuard message="Resubscribe to categorize transactions">
                <button
                  className="inline-flex items-center justify-center w-7 h-7 border border-border rounded-md bg-card text-muted-foreground cursor-pointer transition-all hover:bg-muted hover:text-foreground hover:border-input focus:outline-2 focus:outline-ring focus:outline-offset-2"
                  onClick={handleCategorize}
                  aria-label="Categorize transaction"
                  type="button"
                >
                  <Tag className="w-3.5 h-3.5" />
                </button>
              </ReadOnlyGuard>
            )}
          </div>
        )}
      </div>
    </article>
  )
}
