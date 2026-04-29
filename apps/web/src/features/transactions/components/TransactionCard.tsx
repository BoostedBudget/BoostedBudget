import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import { ReadOnlyGuard, useReadOnly } from '@/auth'
import { TransactionBudgetBadge } from '../../budgets/components/transaction-link'
import type { BudgetCategoryMatch } from '../../budgets/utils/transactionBudgetMatcher'
import { getCategoryColor } from '@/shared/categories/pfcColors'
import { getEffectiveCategory, getEffectiveCategoryLabel } from '@/shared/categories/getEffectiveCategory'
import { ArrowUpRight, ArrowDownRight, Clock, Pencil, Tag } from 'lucide-react'
import { cn } from '@/lib/utils'

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
 * Format transaction amount with sign and currency
 */
function formatAmount(amount: number): string {
  const absAmount = Math.abs(amount)
  const formatted = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(absAmount)

  if (amount === 0) return formatted
  if (amount > 0) return `+${formatted}`
  return `-${formatted}`
}

/**
 * Format date with relative labels (Today, Yesterday) or readable format
 * Uses local timezone for consistent comparison across environments
 * Handles both YYYY-MM-DD and RFC3339 (YYYY-MM-DDTHH:MM:SSZ) formats
 */
function formatDate(dateStr: string | null | undefined): string {
  // Validate input
  if (!dateStr || typeof dateStr !== 'string') {
    return 'Invalid Date'
  }

  // Extract date portion from RFC3339 format (e.g., "2025-11-12T00:00:00Z" -> "2025-11-12")
  const datePortion = dateStr.split('T')[0]

  // Validate YYYY-MM-DD format
  if (!/^\d{4}-\d{2}-\d{2}$/.test(datePortion)) {
    return 'Invalid Date'
  }

  // Parse date components to avoid timezone issues
  const [year, month, day] = datePortion.split('-').map(Number)

  // Validate parsed values
  if (isNaN(year) || isNaN(month) || isNaN(day)) {
    return 'Invalid Date'
  }

  const transactionDate = new Date(year, month - 1, day) // month is 0-indexed

  // Validate resulting date
  if (isNaN(transactionDate.getTime())) {
    return 'Invalid Date'
  }

  const today = new Date()
  today.setHours(0, 0, 0, 0)

  const yesterday = new Date(today)
  yesterday.setDate(yesterday.getDate() - 1)

  // Normalize transaction date to midnight local time
  transactionDate.setHours(0, 0, 0, 0)

  if (transactionDate.getTime() === today.getTime()) {
    return 'Today'
  }

  if (transactionDate.getTime() === yesterday.getTime()) {
    return 'Yesterday'
  }

  // Format as "Oct 25, 2025"
  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }).format(transactionDate)
}

export function TransactionCard({ transaction, account, onEdit, onCategorize, budgetMatch }: TransactionCardProps) {
  const { isReadOnly } = useReadOnly()
  const {
    id,
    name,
    merchant_name,
    amount,
    date,
    authorized_date,
    tags,
    pending,
  } = transaction

  const handleClick = () => {
    if (!isReadOnly) onEdit?.(transaction)
  }

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      if (!isReadOnly) onEdit?.(transaction)
    }
  }

  const displayDate = formatDate(authorized_date || date)
  const displayName = merchant_name || name

  const effective = getEffectiveCategory(transaction)
  const categoryLabel = getEffectiveCategoryLabel(effective)

  // Check if should show warning indicator — no warning when PFC provides a fallback
  const showUncategorizedWarning = categoryLabel === 'Uncategorized'
  const amountFormatted = formatAmount(amount)

  // Determine amount styling
  const amountClass = amount > 0
    ? 'text-success'
    : amount < 0
      ? 'text-destructive'
      : 'text-foreground'

  const ariaLabel = `${displayName}, ${amountFormatted}, ${displayDate}${pending ? ', pending' : ''}`

  // Issue #531: Get category color for the dot
  const categoryColor = getCategoryColor(categoryLabel)

  // Issue #531: Account display name (nickname takes precedence)
  const accountDisplayName = account ? (account.nickname || account.name) : null
  const accountBackgroundColor = account?.custom_color || '#E5E7EB'

  // Issue #531: Handle quick action clicks (prevent card click propagation)
  const handleEditClick = (e: React.MouseEvent) => {
    e.stopPropagation()
    onEdit?.(transaction)
  }

  const handleCategorizeClick = (e: React.MouseEvent) => {
    e.stopPropagation()
    onCategorize?.(transaction)
  }

  return (
    <article
      className={cn(
        'relative bg-card border border-border rounded-lg p-4 mb-3 transition-shadow',
        pending && 'bg-yellow-50 dark:bg-yellow-950 border-yellow-300 dark:border-yellow-700',
        onEdit && 'cursor-pointer hover:shadow-md focus:outline-2 focus:outline-ring focus:outline-offset-2 active:scale-[0.99]'
      )}
      data-transaction-id={id}
      data-pending={pending}
      data-pfc-primary={transaction.pfc_primary || undefined}
      aria-label={ariaLabel}
      onClick={onEdit ? handleClick : undefined}
      onKeyDown={onEdit ? handleKeyDown : undefined}
      role={onEdit ? 'button' : undefined}
      tabIndex={onEdit ? 0 : undefined}
    >
      <div className="flex justify-between items-start gap-4">
        <div className="flex-1 min-w-0">
          <div className="text-base font-semibold text-foreground mb-1 overflow-hidden text-ellipsis whitespace-nowrap">{displayName}</div>
          {/* Issue #531: Account badge */}
          {account && (
            <div className="transaction-card__account mt-0.5">
              <span
                className="transaction-card__account-badge inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium text-white"
                style={{ backgroundColor: accountBackgroundColor }}
              >
                {account.custom_emoji && <span>{account.custom_emoji}</span>}
                {accountDisplayName}
              </span>
            </div>
          )}
          <div className="transaction-card__meta flex gap-3 text-sm text-muted-foreground mt-1">
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
                <span className="transaction-card__uncategorized-warning text-xs leading-none" title="Uncategorized">⚠️</span>
              )}
              {categoryLabel}
            </span>
            <span>{displayDate}</span>
          </div>
          {tags && tags.length > 0 && (
            <div className="transaction-card__tags flex flex-wrap gap-1 mt-1.5" data-testid="transaction-tags">
              {tags.map((tag) => (
                <span key={tag} className="inline-block text-[11px] font-medium text-foreground bg-muted px-2 py-0.5 rounded-full whitespace-nowrap">
                  {tag}
                </span>
              ))}
            </div>
          )}
        </div>
        <div className="flex flex-col items-end gap-1">
          <div className={cn('text-lg font-bold whitespace-nowrap inline-flex items-center', amountClass)} data-ph-mask>
            {/* Issue #531: Income/Expense arrows */}
            {amount > 0 ? (
              <ArrowUpRight className="w-4 h-4 mr-0.5 inline-block align-middle" data-testid="income-arrow" />
            ) : amount < 0 ? (
              <ArrowDownRight className="w-4 h-4 mr-0.5 inline-block align-middle" data-testid="expense-arrow" />
            ) : null}
            {amountFormatted}
          </div>
          {/* Issue #531: Enhanced pending badge */}
          {pending && (
            <div
              className="transaction-card__pending-badge inline-flex items-center gap-1 text-xs font-semibold text-yellow-700 dark:text-yellow-300 bg-yellow-100 dark:bg-yellow-800 px-2 py-0.5 rounded-full uppercase tracking-wide"
              title="This transaction is still pending and may be updated by your bank"
            >
              <Clock className="w-3 h-3" data-testid="pending-icon" />
              Pending
            </div>
          )}
        </div>
        {/* Issue #531: Quick action buttons */}
        {(onEdit || onCategorize) && (
          <div className="transaction-card__quick-actions hidden absolute right-4 top-1/2 -translate-y-1/2 gap-1 group-hover:flex [article:hover_&]:flex [article:focus-within_&]:flex">
            {onEdit && (
              <ReadOnlyGuard message="Resubscribe to edit transactions">
                <button
                  className="inline-flex items-center justify-center w-7 h-7 border border-border rounded-md bg-card text-muted-foreground cursor-pointer transition-all hover:bg-muted hover:text-foreground hover:border-input focus:outline-2 focus:outline-ring focus:outline-offset-2"
                  onClick={handleEditClick}
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
                  onClick={handleCategorizeClick}
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
