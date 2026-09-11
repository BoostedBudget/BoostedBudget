/**
 * Format YYYY-MM-DD or ISO 8601 date string as "MMM D, YYYY" (e.g. "Oct 25, 2025").
 * Parses locally (no UTC shift). Falls back gracefully on invalid input.
 *
 * `options.omitYear` drops the year (e.g. "Oct 25") — added for #1631 Batch F2 so
 * `BudgetCard`'s compact date-range layout can use this shared, correctly-parsing formatter
 * instead of keeping its own private copy that also happened to omit the year.
 */
export function formatDate(
  dateStr: string | null | undefined,
  options?: { omitYear?: boolean }
): string {
  if (!dateStr || typeof dateStr !== 'string') return 'Invalid Date'
  const datePortion = dateStr.split('T')[0]
  if (!/^\d{4}-\d{2}-\d{2}$/.test(datePortion)) return 'Invalid Date'
  const [year, month, day] = datePortion.split('-').map(Number)
  if (isNaN(year) || isNaN(month) || isNaN(day)) return 'Invalid Date'
  const transactionDate = new Date(year, month - 1, day)
  if (isNaN(transactionDate.getTime())) return 'Invalid Date'
  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    ...(options?.omitYear ? {} : { year: 'numeric' }),
  }).format(transactionDate)
}
