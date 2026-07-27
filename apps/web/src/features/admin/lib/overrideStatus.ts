/**
 * Override liveness + status-label logic
 * Issue #1413/#1414: Admin UI — stop lying about override state
 *
 * Extracted from UsersPage.tsx so the override-expiry arithmetic is covered directly
 * (rather than only incidentally, through page rendering) and so `isOverrideActive` can be
 * reused everywhere the admin UI needs to know whether a manual tier override is *currently
 * in effect* — not just present.
 */

/**
 * Whether a manual tier override is currently active: set to a non-empty value AND
 * (it has no expiry, or its expiry has not yet passed).
 *
 * Mirrors the backend predicate character-for-character
 * (`domain.User.hasActiveManualOverrideAt` in backend/internal/domain/user.go):
 * `override != nil && *override != "" && (endsAt == nil || !endsAt.Before(now))`.
 *
 * Note the boundary: an override ending exactly `now` is still active — the backend uses
 * `>=` (via `!endsAt.Before(now)`), never `>`, so this must agree at the tie.
 *
 * `now` defaults to the current time; accept it as a parameter so callers (and tests) can
 * evaluate liveness as of a fixed instant.
 */
export function isOverrideActive(
  override: string | null | undefined,
  endsAt: string | null | undefined,
  now: Date = new Date()
): boolean {
  if (!override) return false
  if (!endsAt) return true
  return new Date(endsAt).getTime() >= now.getTime()
}

export interface OverrideStatus {
  label: string
  variant: string
}

/**
 * Human-readable label + variant for the (hideable) "Override Status" column: Permanent /
 * Expires in Nd / Expired / null (no override at all). This is presentational only — it is
 * NOT the source of truth for whether the override is live; use `isOverrideActive` for that.
 */
export function getOverrideStatus(
  manualTierOverride: string | null | undefined,
  manualTierEndsAt: string | null | undefined
): OverrideStatus | null {
  if (!manualTierOverride) return null
  if (!manualTierEndsAt) return { label: 'Permanent', variant: 'permanent' }

  const expiresAt = new Date(manualTierEndsAt)
  const now = new Date()

  if (expiresAt < now) return { label: 'Expired', variant: 'expired' }

  const daysLeft = Math.ceil((expiresAt.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
  return { label: `Expires in ${daysLeft}d`, variant: 'expiring' }
}
