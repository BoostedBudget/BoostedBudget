/**
 * getErrorStatus
 *
 * Issue #1370: Household feature — hooks layer (shared helper)
 *
 * Extracts an HTTP status code from an unknown mutation/query error, whether
 * it's shaped as an axios-style `{ response: { status } }` error or a plain
 * `{ status }` object. Shared by useHouseholdInvites.ts (409 duplicate-email
 * / seats-full classification) and useAcceptInvite.ts (404/403/409
 * errorKind classification).
 */
export function getErrorStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined

  if ('response' in error) {
    const response = (error as { response?: { status?: number } }).response
    if (response?.status !== undefined) return response.status
  }

  if ('status' in error) {
    return (error as { status?: number }).status
  }

  return undefined
}
