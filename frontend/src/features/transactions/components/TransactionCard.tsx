import type { Transaction, CategorySource } from '../types'
import './TransactionCard.css'

interface TransactionCardProps {
  transaction: Transaction
  onEdit?: (transaction: Transaction) => void
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

export function TransactionCard({ transaction, onEdit }: TransactionCardProps) {
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
          <div className="transaction-card__meta">
            <span className="transaction-card__category">
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
          <div className={amountClass}>{amountFormatted}</div>
          {pending && (
            <div className="transaction-card__pending-badge">Pending</div>
          )}
        </div>
      </div>
    </article>
  )
}
