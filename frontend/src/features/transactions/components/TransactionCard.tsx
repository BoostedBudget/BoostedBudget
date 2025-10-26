import type { Transaction } from '../types'
import './TransactionCard.css'

interface TransactionCardProps {
  transaction: Transaction
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
 */
function parseCategory(categoryJson: string | null): string {
  if (!categoryJson) return 'Uncategorized'

  try {
    const categories = JSON.parse(categoryJson)
    if (Array.isArray(categories) && categories.length > 0) {
      return categories[0] // Return primary category
    }
    return 'Uncategorized'
  } catch {
    return 'Uncategorized'
  }
}

/**
 * Format date with relative labels (Today, Yesterday) or readable format
 */
function formatDate(dateStr: string): string {
  const date = new Date(dateStr + 'T00:00:00') // Parse as midnight UTC
  const today = new Date()
  today.setHours(0, 0, 0, 0)

  const yesterday = new Date(today)
  yesterday.setDate(yesterday.getDate() - 1)

  const transactionDate = new Date(date)
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
  }).format(date)
}

export function TransactionCard({ transaction }: TransactionCardProps) {
  const {
    id,
    name,
    merchant_name,
    amount,
    date,
    authorized_date,
    category,
    pending,
  } = transaction

  const displayDate = formatDate(authorized_date || date)
  const displayName = merchant_name || name
  const categoryLabel = parseCategory(category)
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
      className={`transaction-card${pending ? ' pending' : ''}`}
      data-transaction-id={id}
      data-pending={pending}
      aria-label={ariaLabel}
    >
      <div className="transaction-card__header">
        <div className="transaction-card__info">
          <div className="transaction-card__name">{displayName}</div>
          <div className="transaction-card__meta">
            <span className="transaction-card__category">{categoryLabel}</span>
            <span className="transaction-card__date">{displayDate}</span>
          </div>
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
