/**
 * Chart Formatters
 * Issue #445: Recharts to Plotly Migration
 *
 * Currency and date formatting utilities for chart axes and tooltips.
 */

/**
 * Format a number as USD currency (full format)
 * Example: 1234.56 -> "$1,234.56"
 */
export function formatCurrency(value: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(value);
}

/**
 * Format a number as abbreviated USD currency
 * Example: 1234 -> "$1.2k", 1234567 -> "$1.2M"
 */
export function formatCurrencyCompact(value: number): string {
  const absValue = Math.abs(value);
  const sign = value < 0 ? '-' : '';

  if (absValue >= 1_000_000) {
    return `${sign}$${(absValue / 1_000_000).toFixed(1)}M`;
  }
  if (absValue >= 1_000) {
    return `${sign}$${(absValue / 1_000).toFixed(1)}k`;
  }
  return `${sign}$${absValue.toFixed(0)}`;
}

/**
 * Format a number as full USD currency with cents
 * Example: 1234.56 -> "$1,234.56"
 */
export function formatCurrencyFull(value: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value);
}

/**
 * Get Plotly tickformat string for currency axis
 * Uses Plotly's d3-format syntax
 */
export function getCurrencyTickFormat(): string {
  return '$,.0f';
}

/**
 * Get Plotly tickformat string for compact currency axis
 * Uses Plotly's d3-format syntax with SI prefix
 */
export function getCurrencyCompactTickFormat(): string {
  return '$.2s';
}

/**
 * Create a hovertemplate for currency values
 * @param label - The label to show (e.g., "Balance", "Income")
 */
export function createCurrencyHoverTemplate(label: string): string {
  return `<b>${label}</b>: %{customdata}<extra></extra>`;
}

/**
 * Create a hovertemplate for time-series with date and value
 */
export function createTimeSeriesHoverTemplate(valueLabel: string): string {
  return `<b>%{x}</b><br>${valueLabel}: %{customdata}<extra></extra>`;
}
