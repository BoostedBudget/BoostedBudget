/**
 * Formats a date (or date string) as a relative time label (e.g., "2m ago", "3h ago").
 * Falls back to toLocaleDateString() for dates older than 7 days.
 *
 * Accepts an already-parsed `Date` so callers that already have one (e.g. SnapshotAgeLabel,
 * which parses `asOf` once via `parseServerDateTime` and reuses that same Date here and for its
 * `title` attribute) don't need to re-parse the source string with a second, potentially
 * disagreeing parser. Callers that only have the raw string (BoostCard, BoostTable,
 * NotificationItem — all trusted backend fields, not the admin `as_of` guarded by
 * `isParsableDate`) can keep passing it directly.
 */
export function formatRelativeTime(dateOrStr: Date | string): string {
  const date = typeof dateOrStr === 'string' ? new Date(dateOrStr) : dateOrStr
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)
  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString()
}
