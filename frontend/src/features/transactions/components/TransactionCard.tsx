import type { Transaction, CategorySource } from '../types'
import type { Account } from '../../accounts/types'
import { TransactionBudgetBadge } from '../../budgets/components/transaction-link'
import type { BudgetCategoryMatch } from '../../budgets/utils/transactionBudgetMatcher'
import { getCategoryColor } from '@/lib/category-colors'
import { ArrowUpRight, ArrowDownRight, Clock, Pencil, Tag } from 'lucide-react'
import './TransactionCard.css'

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
 * Parse category from JSON string
 * Uses primary category (index 0) for consistency across the application
 */
function parseCategory(categoryJson: string | null | undefined): string {
  if (!categoryJson) return 'Uncategorized'

  try {
    const categories = JSON.parse(categoryJson)
    if (Array.isArray(categories) && categories.length > 0) {
      return categories[0] // Always use primary category
    }
    return 'Uncategorized'
  } catch {
    return 'Uncategorized'
  }
}

/**
 * Check if transaction is explicitly uncategorized by user
 * Returns true when user manually cleared the category (should NOT fall back to Plaid)
 */
function isExplicitlyUncategorized(
  categoryOverride: string | null | undefined,
  categorySource: CategorySource | undefined
): boolean {
  // If category_source is 'manual', user explicitly set something
  // If override is empty/null at that point, they explicitly uncategorized it
  if (categorySource === 'manual') {
    const parsed = parseCategory(categoryOverride)
    return parsed === 'Uncategorized'
  }
  return false
}

/**
 * Check if transaction should display as uncategorized (for warning indicator)
 */
function isUncategorized(
  category: string | null | undefined,
  categoryOverride: string | null | undefined,
  categorySource: CategorySource | undefined
): boolean {
  // Explicitly uncategorized by user
  if (isExplicitlyUncategorized(categoryOverride, categorySource)) {
    return true
  }
  // Never had a category
  if (!category && !categoryOverride) {
    return true
  }
  // Has override but it's empty (and not manual source)
  if (categoryOverride && parseCategory(categoryOverride) === 'Uncategorized' && categorySource !== 'manual') {
    return parseCategory(category) === 'Uncategorized'
  }
  // Check if displayed category would be uncategorized
  const displayedCategory = categoryOverride && parseCategory(categoryOverride) !== 'Uncategorized'
    ? categoryOverride
    : category
  return parseCategory(displayedCategory) === 'Uncategorized'
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
  const {
    id,
    name,
    merchant_name,
    amount,
    date,
    authorized_date,
    category,
    category_override,
    category_source,
    original_category,
    tags,
    pending,
  } = transaction

  const handleClick = () => {
    onEdit?.(transaction)
  }

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      onEdit?.(transaction)
    }
  }

  const displayDate = formatDate(authorized_date || date)
  const displayName = merchant_name || name

  // Check if explicitly uncategorized by user (should NOT fall back to Plaid)
  const explicitlyUncategorized = isExplicitlyUncategorized(category_override, category_source)

  // Use category_override if present and non-empty, otherwise fall back to category
  // UNLESS explicitly uncategorized by user
  let categoryToDisplay: string | null | undefined
  if (explicitlyUncategorized) {
    categoryToDisplay = null // Will display as "Uncategorized"
  } else if (category_override && parseCategory(category_override) !== 'Uncategorized') {
    categoryToDisplay = category_override
  } else {
    categoryToDisplay = category
  }
  const categoryLabel = parseCategory(categoryToDisplay)

  // Check if should show warning indicator
  const showUncategorizedWarning = isUncategorized(category, category_override, category_source)
  const amountFormatted = formatAmount(amount)

  // Determine amount styling
  const amountClass = amount > 0
    ? 'transaction-card__amount credit text-green-600'
    : amount < 0
      ? 'transaction-card__amount debit text-red-600'
      : 'transaction-card__amount'

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
      className={`transaction-card${pending ? ' pending' : ''}${onEdit ? ' clickable' : ''}`}
      data-transaction-id={id}
      data-pending={pending}
      data-original-category={original_category || undefined}
      aria-label={ariaLabel}
      onClick={onEdit ? handleClick : undefined}
      onKeyDown={onEdit ? handleKeyDown : undefined}
      role={onEdit ? 'button' : undefined}
      tabIndex={onEdit ? 0 : undefined}
    >
      <div className="transaction-card__header">
        <div className="transaction-card__info">
          <div className="transaction-card__name">{displayName}</div>
          {/* Issue #531: Account badge */}
          {account && (
            <div className="transaction-card__account">
              <span
                className="transaction-card__account-badge"
                style={{ backgroundColor: accountBackgroundColor }}
              >
                {account.custom_emoji && <span>{account.custom_emoji}</span>}
                {accountDisplayName}
              </span>
            </div>
          )}
          <div className="transaction-card__meta">
            <TransactionBudgetBadge match={budgetMatch} />
            <span className="transaction-card__category">
              {/* Issue #531: Category color dot */}
              <span
                className="transaction-card__category-dot"
                style={{ backgroundColor: categoryColor }}
              />
              {showUncategorizedWarning && (
                <span className="transaction-card__uncategorized-warning" title="Uncategorized">⚠️</span>
              )}
              {categoryLabel}
            </span>
            <span className="transaction-card__date">{displayDate}</span>
          </div>
          {tags && tags.length > 0 && (
            <div className="transaction-card__tags" data-testid="transaction-tags">
              {tags.map((tag) => (
                <span key={tag} className="transaction-card__tag">
                  {tag}
                </span>
              ))}
            </div>
          )}
        </div>
        <div className="transaction-card__amount-container">
          <div className={amountClass}>
            {/* Issue #531: Income/Expense arrows */}
            {amount > 0 ? (
              <ArrowUpRight className="transaction-card__amount-arrow" data-testid="income-arrow" />
            ) : amount < 0 ? (
              <ArrowDownRight className="transaction-card__amount-arrow" data-testid="expense-arrow" />
            ) : null}
            {amountFormatted}
          </div>
          {/* Issue #531: Enhanced pending badge */}
          {pending && (
            <div
              className="transaction-card__pending-badge"
              title="This transaction is still pending and may be updated by your bank"
            >
              <Clock className="transaction-card__pending-icon" data-testid="pending-icon" />
              Pending
            </div>
          )}
        </div>
        {/* Issue #531: Quick action buttons */}
        {(onEdit || onCategorize) && (
          <div className="transaction-card__quick-actions">
            {onEdit && (
              <button
                className="transaction-card__action-btn"
                onClick={handleEditClick}
                aria-label="Edit transaction"
                type="button"
              >
                <Pencil className="transaction-card__action-icon" />
              </button>
            )}
            {onCategorize && (
              <button
                className="transaction-card__action-btn"
                onClick={handleCategorizeClick}
                aria-label="Categorize transaction"
                type="button"
              >
                <Tag className="transaction-card__action-icon" />
              </button>
            )}
          </div>
        )}
      </div>
    </article>
  )
}
