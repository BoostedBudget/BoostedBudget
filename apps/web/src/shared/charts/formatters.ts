/**
 * Chart Formatters — currency and percentage utilities for axes, tooltips, and change indicators.
 */

export function formatCurrency(value: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(value)
}

export function formatCurrencyCompact(value: number): string {
  const absValue = Math.abs(value)
  const sign = value < 0 ? '-' : ''

  if (absValue >= 1_000_000) {
    return `${sign}$${(absValue / 1_000_000).toFixed(1)}M`
  }
  if (absValue >= 1_000) {
    return `${sign}$${(absValue / 1_000).toFixed(1)}k`
  }
  return `${sign}$${absValue.toFixed(0)}`
}

/**
 * Formats a value with two decimal places in the currency it was recorded in.
 *
 * ⚠ `currencyCode` IS OPTIONAL FOR COMPATIBILITY, NOT BECAUSE IT IS DECORATION (Issue #1569).
 * This function hardcoded `currency: 'USD'` and was called from surfaces that render figures the
 * API labels per row — a retained 1000 EUR balance came out as `$1,000.00`, which is a WRONG
 * number in a financial UI rather than a missing one. The parameter is optional so that the many
 * pre-existing call sites keep their exact behaviour, and every call site that HAS a currency code
 * should pass it.
 *
 * An unknown or malformed code falls back to USD rather than throwing: `Intl.NumberFormat` raises
 * a RangeError on a code that is not three letters, and a chart cell is not the place to take a
 * page down over a bad string from a provider.
 */
export function formatCurrencyFull(value: number, currencyCode?: string): string {
  const currency = currencyCode && /^[A-Za-z]{3}$/.test(currencyCode) ? currencyCode.toUpperCase() : 'USD'
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value)
}

interface FormatPercentOptions {
  decimals?: number
  abs?: boolean
}

/**
 * Formats a number as a percentage string (e.g. `formatPercent(45.67)` → `"45.67%"`).
 * Keeps percentage display consistent across charts, tables, and change indicators.
 */
export function formatPercent(value: number, options: FormatPercentOptions = {}): string {
  const { decimals = 2, abs = false } = options
  const v = abs ? Math.abs(value) : value
  return `${v.toFixed(decimals)}%`
}
