/**
 * Subscription Presentation Derivation
 *
 * Issue #1413: The single shared derivation of "what does this subscription status mean for the
 * UI" — write access, the /pricing redirect, billing nags, and status badges. Every screen that
 * currently re-derives "expired" from a raw status string should consume this instead.
 *
 * `getSubscriptionPresentation` takes a plain `SubscriptionSignals` struct — deliberately NOT the
 * generated `User` type — so this module and its tests are fully green independent of the backend
 * contract landing. Only the two adapters at the bottom touch generated types, and they do so
 * defensively: a payload without `effective_status` / `is_comped` / `comp_ends_at` still produces
 * correct pre-#1413 behavior, since the two halves may deploy separately.
 */

import type { BadgeProps } from '@/components/ui/badge'
import type { GetCurrentSubscriptionOutputBody, User } from '@boostedbudget/api-client/models'

export type SubscriptionKind =
  | 'active'
  | 'trial'
  | 'trialing'
  | 'comped'
  | 'cancelled'
  | 'past_due'
  | 'inactive'
  | 'unknown'

// Plain struct — deliberately NOT the generated `User` type. This is what makes the module (and
// its tests) unblocked by the backend contract.
export interface SubscriptionSignals {
  status: string
  effectiveStatus?: string
  isComped?: boolean
  compEndsAt?: string
  trialDaysRemaining?: number
}

export interface SubscriptionPresentation {
  kind: SubscriptionKind
  hasWriteAccess: boolean // -> PremiumRoute's ReadOnlyProvider
  isUnknown: boolean // -> PremiumRoute's Navigate-to-/pricing branch
  suppressBillingNags: boolean // -> banners, frozen banner, upgrade CTAs
  isComped: boolean
  compEndsAt?: string
  badgeVariant: NonNullable<BadgeProps['variant']>
  badgeLabel: string
  contextStatus: string
}

/** Statuses derivable directly from a raw status string ('comped' and 'unknown' never are). */
type RecognizedStatusKind = Exclude<SubscriptionKind, 'comped' | 'unknown'>

const RECOGNIZED_STATUS_KINDS: readonly RecognizedStatusKind[] = [
  'active',
  'trial',
  'trialing',
  'cancelled',
  'past_due',
  'inactive',
]

function isRecognizedStatusKind(value: string): value is RecognizedStatusKind {
  return (RECOGNIZED_STATUS_KINDS as readonly string[]).includes(value)
}

const WRITABLE_KINDS: ReadonlySet<SubscriptionKind> = new Set<SubscriptionKind>([
  'active',
  'trial',
  'trialing',
  'comped',
])

const BADGE_VARIANT_BY_KIND: Record<SubscriptionKind, NonNullable<BadgeProps['variant']>> = {
  active: 'success',
  trial: 'info',
  trialing: 'info',
  comped: 'info',
  cancelled: 'secondary',
  past_due: 'warning',
  inactive: 'destructive',
  unknown: 'outline',
}

/**
 * Resolution order:
 * 1. `isComped === true` wins outright.
 * 2. Otherwise use `effectiveStatus ?? status`.
 * 3. If that value is not a recognised kind, fall back to the raw `status` before ever yielding
 *    'unknown' — a future backend enum value the frontend hasn't learned yet must never bounce a
 *    real user out of the app via the /pricing redirect.
 */
function resolveKind(signals: SubscriptionSignals): SubscriptionKind {
  if (signals.isComped === true) return 'comped'

  const candidate = signals.effectiveStatus ?? signals.status
  if (isRecognizedStatusKind(candidate)) return candidate

  if (isRecognizedStatusKind(signals.status)) return signals.status

  return 'unknown'
}

function formatBadgeLabel(kind: SubscriptionKind, trialDaysRemaining?: number): string {
  if (kind === 'comped') return 'Complimentary'

  if ((kind === 'trial' || kind === 'trialing') && trialDaysRemaining !== undefined) {
    const noun = trialDaysRemaining === 1 ? 'day' : 'days'
    return `Trial — ${trialDaysRemaining} ${noun} remaining`
  }

  switch (kind) {
    case 'trial':
      return 'Trial'
    case 'trialing':
      return 'Trialing'
    case 'past_due':
      return 'Past Due'
    case 'active':
      return 'Active'
    case 'cancelled':
      return 'Cancelled'
    case 'inactive':
      return 'Inactive'
    case 'unknown':
      return 'Unknown'
  }
}

export function getSubscriptionPresentation(signals: SubscriptionSignals): SubscriptionPresentation {
  const kind = resolveKind(signals)
  const isComped = kind === 'comped'

  return {
    kind,
    hasWriteAccess: WRITABLE_KINDS.has(kind),
    isUnknown: kind === 'unknown',
    suppressBillingNags: isComped,
    isComped,
    compEndsAt: signals.compEndsAt,
    badgeVariant: BADGE_VARIANT_BY_KIND[kind],
    badgeLabel: formatBadgeLabel(kind, signals.trialDaysRemaining),
    contextStatus: kind,
  }
}

// --- Adapters ---------------------------------------------------------------------------------
// `effective_status` / `is_comped` / `comp_ends_at` are part of the generated `User` and
// `GetCurrentSubscriptionOutputBody` shapes (packages/api-client/src/models), so these adapters
// read them directly. `effective_status` and `comp_ends_at` stay optional in those generated
// types (the backend's `omitempty` genuinely omits them when empty/nil), so
// `getSubscriptionPresentation` above still treats an absent value as "not reported" rather than
// assuming it's always present.

/** Adapter from the profile-endpoint (`GET /users/me`) shape. */
export function presentationFromUser(user: User): SubscriptionPresentation {
  return getSubscriptionPresentation({
    status: user.subscription_status,
    effectiveStatus: user.effective_status,
    isComped: user.is_comped,
    compEndsAt: user.comp_ends_at,
    trialDaysRemaining: user.trial_days_remaining,
  })
}

/**
 * Adapter from `GET /subscription/current`. A 404 (no subscription found) or a missing payload is
 * treated as inactive.
 */
export function presentationFromSubscription(
  subscription: GetCurrentSubscriptionOutputBody | undefined,
  opts?: { notFound?: boolean },
): SubscriptionPresentation {
  if (opts?.notFound || !subscription) {
    return getSubscriptionPresentation({ status: 'inactive' })
  }

  return getSubscriptionPresentation({
    status: subscription.status,
    isComped: subscription.is_comped,
    compEndsAt: subscription.comp_ends_at,
    trialDaysRemaining: subscription.trial_days_remaining,
  })
}
