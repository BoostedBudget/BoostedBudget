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
 * Whether a manual tier override is currently active: set to a non-empty value, (it has no
 * expiry, or its expiry has not yet passed), AND — when `knownTierNames` is supplied — names a
 * real, currently-configured tier.
 *
 * Was documented as mirroring the backend predicate character-for-character
 * (`domain.User.hasActiveManualOverrideAt` in backend/internal/domain/user.go) — true when this
 * was written, but only half the backend rule since (Issue #1413, this task):
 * manual_tier_override is an unconstrained VARCHAR(50) with no CHECK constraint, so it can hold
 * a value naming no configured tier (a legacy row, a direct SQL write, a write made while the
 * tier config was nil). The backend's ComputeEffectiveTier()/ComputeEffectiveStatus() reject
 * such a value and fall back to the raw tier/status; this predicate must do the same, or the
 * admin UI renders the raw garbage string as the tier badge and shows the "Comped" chip for a
 * user with zero write access.
 *
 * `knownTierNames` is optional and nil-safe by design (mirrors every backend
 * SetOverrideTierValidator call site's "skip validation, not deny" convention): omit it (or
 * pass undefined) to check liveness alone, exactly as before. UsersPage.tsx's one call site
 * always passes the known tier set derived from its own TIER_DISPLAY map — the single source of
 * truth for "what counts as a configured tier" on this page, not duplicated here.
 *
 * Note the expiry boundary: an override ending exactly `now` is still active — the backend uses
 * `>=` (via `!endsAt.Before(now)`), never `>`, so this must agree at the tie.
 *
 * `now` defaults to the current time; accept it as a parameter so callers (and tests) can
 * evaluate liveness as of a fixed instant.
 */
export function isOverrideActive(
  override: string | null | undefined,
  endsAt: string | null | undefined,
  knownTierNames?: ReadonlySet<string> | readonly string[],
  now: Date = new Date()
): boolean {
  if (!override) return false
  if (endsAt) {
    const endsAtMs = new Date(endsAt).getTime()
    // An UNPARSEABLE date must fail CLOSED. Every comparison against NaN is false, so the naive
    // `new Date(endsAt).getTime() < now` form silently falls through and reports the override as
    // LIVE — precisely backwards. The three-axis rule's *representation* axis says: when the data
    // does not let us establish a live comp, do not claim one. (Issue #1413; found by Codex
    // adversarial review of the round-7 diff.)
    if (Number.isNaN(endsAtMs) || endsAtMs < now.getTime()) return false
  }
  if (knownTierNames === undefined) return true
  const known = knownTierNames instanceof Set ? knownTierNames : new Set(knownTierNames)
  return known.has(override)
}

export interface OverrideStatus {
  label: string
  variant: string
}

/**
 * Human-readable label + variant for the (hideable) "Override Status" column: Permanent /
 * Expires in Nd / Expired / null (no override at all). This is presentational only — it is
 * NOT the source of truth for whether the override is live; use `isOverrideActive` for that.
 *
 * `now` defaults to the current time; accept it as a parameter — mirroring `isOverrideActive`
 * above — so a caller that also calls `isOverrideActive` for the same row can evaluate both
 * against a single instant instead of each independently reading the clock (Issue #1413 round 9,
 * Part 2: two separate `new Date()` calls in SubscriptionOverrideModal.tsx could straddle an
 * expiry, producing a chip and a badge that momentarily disagree about the same row).
 */
export function getOverrideStatus(
  manualTierOverride: string | null | undefined,
  manualTierEndsAt: string | null | undefined,
  now: Date = new Date()
): OverrideStatus | null {
  if (!manualTierOverride) return null
  if (!manualTierEndsAt) return { label: 'Permanent', variant: 'permanent' }

  const expiresAt = new Date(manualTierEndsAt)

  // Unparseable date: report it as such rather than rendering "Expires in NaNd", and use the
  // non-live `expired` variant so the row is never styled as an active comp. Matches
  // isOverrideActive's fail-closed handling above — the two must not disagree about the same row.
  if (Number.isNaN(expiresAt.getTime())) return { label: 'Invalid expiry', variant: 'expired' }
  if (expiresAt < now) return { label: 'Expired', variant: 'expired' }

  const daysLeft = Math.ceil((expiresAt.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
  return { label: `Expires in ${daysLeft}d`, variant: 'expiring' }
}
