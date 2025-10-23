import type { Account } from './types'
import './AccountCard.css'

interface AccountCardProps {
  account: Account
}

/**
 * Format currency amount with proper symbol and decimal places
 */
function formatCurrency(amount: number | undefined, currencyCode: string): string {
  if (amount === undefined) return 'N/A'

  // Use Intl.NumberFormat for proper currency formatting
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currencyCode,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount)
  } catch {
    // Fallback if currency code is invalid
    return `${currencyCode} ${amount.toFixed(2)}`
  }
}

/**
 * Get human-readable account type label
 */
function getAccountTypeLabel(type: string, subtype?: string): string {
  const typeMap: Record<string, string> = {
    depository: 'Bank Account',
    credit: 'Credit Card',
    loan: 'Loan',
    investment: 'Investment',
    other: 'Other',
  }

  const baseLabel = typeMap[type] || type

  // Add subtype if available (e.g., "Bank Account - Checking")
  if (subtype) {
    const subtypeCapitalized = subtype.charAt(0).toUpperCase() + subtype.slice(1)
    return `${baseLabel} - ${subtypeCapitalized}`
  }

  return baseLabel
}

export function AccountCard({ account }: AccountCardProps) {
  const {
    name,
    official_name,
    type,
    subtype,
    mask,
    current_balance,
    available_balance,
    currency_code,
    institution_name,
  } = account

  const typeLabel = getAccountTypeLabel(type, subtype)
  const currentBalanceFormatted = formatCurrency(current_balance, currency_code)
  const availableBalanceFormatted = formatCurrency(available_balance, currency_code)

  return (
    <div className="account-card">
      <div className="account-card__header">
        <div className="account-card__institution">
          {institution_name || 'Unknown Bank'}
        </div>
        {mask && (
          <div className="account-card__mask">
            ****{mask}
          </div>
        )}
      </div>

      <div className="account-card__body">
        <h3 className="account-card__name">
          {official_name || name}
        </h3>
        <p className="account-card__type">{typeLabel}</p>

        <div className="account-card__balances">
          <div className="balance-item">
            <span className="balance-label">Current Balance</span>
            <span className="balance-amount">{currentBalanceFormatted}</span>
          </div>
          {available_balance !== undefined && available_balance !== current_balance && (
            <div className="balance-item">
              <span className="balance-label">Available Balance</span>
              <span className="balance-amount">{availableBalanceFormatted}</span>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
