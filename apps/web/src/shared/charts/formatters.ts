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

export function formatCurrencyFull(value: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
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
