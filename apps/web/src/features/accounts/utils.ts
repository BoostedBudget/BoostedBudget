/**
 * Shared account utility helpers
 *
 * Consolidates duplicated formatting logic from AccountCard and AccountTable.
 * Each helper accepts a surface/options parameter to reproduce the exact output
 * each consumer previously produced inline.
 */

/**
 * Returns a human-readable account type label.
 *
 * - surface:'card' (default) — full format used by AccountCard:
 *   e.g. "Bank Account - Checking", "Investment - 401k", "Credit Card - Credit card"
 * - surface:'table' — compact format used by AccountTable:
 *   e.g. "Checking", "401(k)", "Credit Card"
 */
export function getAccountTypeLabel(
  type: string,
  subtype?: string | null,
  options: { surface: 'card' | 'table' } = { surface: 'card' }
): string {
  return options.surface === 'table'
    ? _getAccountTypeLabelTable(type, subtype)
    : _getAccountTypeLabelCard(type, subtype)
}

function _getAccountTypeLabelCard(type: string, subtype?: string | null): string {
  const typeMap: Record<string, string> = {
    depository: 'Bank Account',
    credit: 'Credit Card',
    loan: 'Loan',
    investment: 'Investment',
    other: 'Other',
  }

  const baseLabel = typeMap[type] ?? type

  if (!subtype) return baseLabel

  const subtypeCapitalized = subtype.charAt(0).toUpperCase() + subtype.slice(1)
  return `${baseLabel} - ${subtypeCapitalized}`
}

function _getAccountTypeLabelTable(type: string, subtype?: string | null): string {
  if (subtype) {
    switch (subtype.toLowerCase()) {
      case 'checking': return 'Checking'
      case 'savings': return 'Savings'
      case 'credit card': return 'Credit Card'
      case 'money market': return 'Money Market'
      case 'cd': return 'CD'
      case 'paypal': return 'PayPal'
      case 'brokerage': return 'Brokerage'
      case '401k': return '401(k)'
      case 'ira': return 'IRA'
      case 'roth': return 'Roth IRA'
      case 'mortgage': return 'Mortgage'
      case 'auto': return 'Auto Loan'
      case 'student': return 'Student Loan'
      default: return subtype.charAt(0).toUpperCase() + subtype.slice(1)
    }
  }
  switch (type.toLowerCase()) {
    case 'depository': return 'Bank Account'
    case 'credit': return 'Credit'
    case 'investment': return 'Investment'
    case 'loan': return 'Loan'
    case 'other': return 'Other'
    default: return type.charAt(0).toUpperCase() + type.slice(1)
  }
}

/**
 * Formats an account balance as a currency string.
 *
 * - Honors currencyCode (not hardcoded USD)
 * - Returns options.empty ('N/A' or '—') for undefined/null amount
 * - Handles negative amounts correctly
 * - Falls back gracefully on invalid currency codes
 *
 * AccountCard migrates with { empty: 'N/A' } (default)
 * AccountTable migrates with { empty: '—' }
 */
export function formatAccountBalance(
  amount: number | undefined | null,
  currencyCode: string | undefined | null,
  options: { empty: 'N/A' | '—' } = { empty: 'N/A' }
): string {
  if (amount === undefined || amount === null) return options.empty

  // Fall back to USD when currencyCode is absent (account not yet currency-resolved).
  const currency = currencyCode ?? 'USD'

  try {
    const absAmount = Math.abs(amount)
    const formatted = new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(absAmount)
    // Use Math.abs + manual '-' prefix to match the AccountTable formatting convention;
    // output is identical for en-US (Intl.NumberFormat with Math.abs vs. negative input).
    return amount < 0 ? `-${formatted}` : formatted
  } catch {
    // Fallback if currency code is invalid
    return `${currency} ${amount.toFixed(2)}`
  }
}
