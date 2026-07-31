/**
 * Known subscription tier names
 * Issue #1413/#1414: Admin UI — stop lying about override state
 *
 * The single source of truth for "what counts as a configured tier" across the admin UI. Pass
 * this into `isOverrideActive` (./overrideStatus.ts) so a `manual_tier_override` naming no
 * configured tier is rejected the same way the backend's ComputeEffectiveTier()/
 * ComputeEffectiveStatus() reject it (manual_tier_override is an unconstrained VARCHAR(50) with no
 * CHECK constraint, so it can hold a legacy/garbage value).
 *
 * Previously this set was computed locally in UsersPage.tsx (`new Set(Object.keys(TIER_DISPLAY))`)
 * and SubscriptionOverrideModal.tsx re-implemented override liveness inline without ever
 * consulting it at all — exactly the drift that let a malformed override (e.g. 'not_a_tier')
 * render as a real, currently-active comp on the modal (Issue #1413 AC6) even though UsersPage
 * already rejected the same value. Both screens' TIER_DISPLAY maps intentionally keep different
 * label text ("Household 2" vs "Household (2)") — this module owns only the shared set of valid
 * tier *keys*, not their presentation, so it does not attempt to unify those.
 */
export const KNOWN_TIER_NAMES: ReadonlySet<string> = new Set([
  'premium',
  'household_2',
  'household_3',
  'household_4',
  'household_5',
])
