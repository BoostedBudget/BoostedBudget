/**
 * Format YYYY-MM-DD or ISO 8601 date string as "MMM D, YYYY" (e.g. "Oct 25, 2025").
 * Parses locally (no UTC shift). Falls back gracefully on invalid input.
 */
export function formatDate(dateStr: string | null | undefined): string {
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
    year: 'numeric',
  }).format(transactionDate)
}
