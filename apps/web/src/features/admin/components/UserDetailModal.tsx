/**
 * UserDetailModal Component
 * Issue #644: Admin User Management UI
 *
 * Modal dialog displaying detailed user information and statistics.
 */

import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import type { BadgeProps } from '@/components/ui/badge'
import { Loader2, User, Calendar, MapPin, CreditCard, Link2, Zap, Receipt, PiggyBank, Shield } from 'lucide-react'
import type { AdminUserDetail } from '@boostedbudget/api-client/models'
import { resolveServerSnapshotTime } from '@/shared/dates'
import { useRegions } from '../../../shared/hooks/useRegions'
import { isOverrideActive, getOverrideStatus } from '../lib/overrideStatus'
import { KNOWN_TIER_NAMES } from '../lib/knownTiers'
import { SnapshotAgeLabel } from './SnapshotAgeLabel'
import { RevenueCatUserPanel } from './RevenueCatUserPanel'

export interface UserDetailModalProps {
  /** User detail data */
  user: AdminUserDetail | undefined
  /** Whether data is loading */
  loading: boolean
  /** Whether modal is open */
  open: boolean
  /** Called when modal should close */
  onClose: () => void
}

const formatDate = (dateString: string | undefined): string => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const formatNumber = (num: number): string => num.toLocaleString()

// Tier display names (matches config/subscription_tiers.yaml)
const TIER_DISPLAY_MAP: Record<string, string> = {
  trial: 'Trial',
  premium: 'Premium',
  household_2: 'Household (2)',
  household_3: 'Household (3)',
  household_4: 'Household (4)',
  household_5: 'Household (5+)',
}

// Mirrors UsersPage.tsx's overrideStatusVariant so the "Override Status" label reads
// identically on both surfaces (Issue #1413 M10 follow-up).
const overrideStatusVariant = (variant: string): BadgeProps['variant'] => {
  if (variant === 'permanent') return 'success'
  if (variant === 'expiring') return 'info'
  if (variant === 'expired') return 'warning'
  return 'secondary'
}

/**
 * UserDetailModal - Modal for viewing user details
 */
export function UserDetailModal({ user, loading, open, onClose }: UserDetailModalProps) {
  const { getRegionWithFlag } = useRegions()
  const [diagnosticUserId, setDiagnosticUserId] = useState<string | null>(null)
  const showDiagnostics = Boolean(user && diagnosticUserId === user.id)

  // Issue #1462 follow-up G2: anchors this modal's override-liveness arithmetic to the server's
  // own snapshot instant (`user.as_of`, populated on AdminUserDetail) instead of a fresh
  // browser-clock read — the same fix SubscriptionOverrideModal already applies via
  // AdminSubscriptionDetailOutputBody.as_of. Falls back to the browser clock only when `user`
  // hasn't loaded yet or `as_of` is absent/unparseable (e.g. an older cached response) —
  // display-only, must never crash the modal.
  const now = resolveServerSnapshotTime(user?.as_of)

  // Whether a manual tier override is currently live for this user. Must NOT be derived from
  // `effective_status` — that field answers "does this user currently have write access", which
  // is also true when access comes from someone else's household plan, so it would badge users
  // with no override of their own as comped (Issue #1413 M10 regression). `isOverrideActive`
  // with `KNOWN_TIER_NAMES` is the only correct predicate for "is there a live admin override on
  // this row right now" — same derivation as UsersPage.tsx's `hasLiveOverride` and
  // SubscriptionOverrideModal.tsx's `isLiveOverride`, reused rather than reinvented so all three
  // surfaces agree.
  const hasLiveOverride = user
    ? isOverrideActive(user.manual_tier_override, user.manual_tier_ends_at, KNOWN_TIER_NAMES, now)
    : false

  // An expired (or malformed-tier) override must NOT be shown as the effective tier — falls back
  // to the raw subscription_tier, same as the backend's ComputeEffectiveTier (Issue #1413).
  const effectiveTier = user ? (hasLiveOverride ? (user.manual_tier_override as string) : user.subscription_tier) : ''

  // Presentational only (Permanent / Expires in Nd / Expired / null) — independent of tier-name
  // validity, matching UsersPage.tsx's "Override Status" column exactly. Anchored to the same
  // server-snapshot `now` as hasLiveOverride above (Issue #1462 follow-up G2).
  const overrideStatus = user ? getOverrideStatus(user.manual_tier_override, user.manual_tier_ends_at, now) : null

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className={showDiagnostics ? 'max-w-[1100px] sm:max-w-[1100px]' : 'max-w-[600px] sm:max-w-[600px]'} aria-describedby="user-detail-description">
        {loading ? (
          <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
            <Loader2 className="w-8 h-8 mb-4 animate-spin" />
            <span>Loading user details...</span>
          </div>
        ) : user ? (
          <>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <User className="w-5 h-5 text-primary" />
                {/* Email is a MUST-mask field (docs/security/posthog-pii-masking.md). UsersPage
                    masks the email COLUMN; this modal is the same address one click deeper, and
                    the admin's consent to session replay is not the subject's. */}
                <span data-ph-mask>{user.email}</span>
                {user.is_admin && (
                  <Badge variant="warning" className="ml-2 gap-1">
                    <Shield size={14} />
                    Admin
                  </Badge>
                )}
              </DialogTitle>
              <DialogDescription id="user-detail-description">
                User ID: <span data-ph-mask>{user.id}</span>
              </DialogDescription>
            </DialogHeader>

            <div className="flex flex-col gap-6 my-4">
              {/* Profile Section */}
              <section className="flex flex-col gap-3">
                <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider m-0 pb-2 border-b border-border">
                  Profile
                </h3>
                <div className="grid grid-cols-2 gap-4 max-[480px]:grid-cols-1">
                  <div className="flex items-start gap-2">
                    <CreditCard className="text-muted-foreground/60 mt-0.5 shrink-0" size={16} />
                    <div className="flex flex-col gap-0.5">
                      <span className="text-xs text-muted-foreground/60">Subscription</span>
                      <span className="text-sm text-foreground flex items-center gap-1.5 flex-wrap">
                        {TIER_DISPLAY_MAP[effectiveTier] ?? effectiveTier} ({user.subscription_status})
                        {hasLiveOverride && (
                          <Badge
                            variant="info"
                            size="sm"
                            title="Effective status: active (manual override)"
                            aria-label="Effective status: active (manual override)"
                          >
                            Comped
                          </Badge>
                        )}
                        {overrideStatus && (
                          <Badge variant={overrideStatusVariant(overrideStatus.variant)} size="sm">
                            {overrideStatus.label}
                          </Badge>
                        )}
                      </span>
                      {/* Issue #1462 follow-up G2 / R3-10: hasLiveOverride/overrideStatus above are
                          anchored to `now`, a FROZEN read of `user.as_of` that never refreshes on a
                          timer. Discloses that snapshot's age so an admin who leaves this modal open
                          can tell the Comped/status badges above may no longer reflect the server's
                          current answer — mirrors SubscriptionOverrideModal's identical disclosure. */}
                      <SnapshotAgeLabel asOf={user.as_of} className="text-[0.7rem]" />
                    </div>
                  </div>

                  <div className="flex items-start gap-2">
                    <MapPin className="text-muted-foreground/60 mt-0.5 shrink-0" size={16} />
                    <div className="flex flex-col gap-0.5">
                      <span className="text-xs text-muted-foreground/60">Region</span>
                      <span className="text-sm text-foreground">{getRegionWithFlag(user.primary_region)}</span>
                    </div>
                  </div>

                  <div className="flex items-start gap-2">
                    <Calendar className="text-muted-foreground/60 mt-0.5 shrink-0" size={16} />
                    <div className="flex flex-col gap-0.5">
                      <span className="text-xs text-muted-foreground/60">Created</span>
                      <span className="text-sm text-foreground">{formatDate(user.created_at)}</span>
                    </div>
                  </div>

                  <div className="flex items-start gap-2">
                    <Calendar className="text-muted-foreground/60 mt-0.5 shrink-0" size={16} />
                    <div className="flex flex-col gap-0.5">
                      <span className="text-xs text-muted-foreground/60">Last Login</span>
                      <span className="text-sm text-foreground">{formatDate(user.last_login_at)}</span>
                    </div>
                  </div>
                </div>
              </section>

              {/* Statistics Section */}
              <section className="flex flex-col gap-3">
                <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider m-0 pb-2 border-b border-border">
                  Statistics
                </h3>
                <div className="grid grid-cols-2 gap-4 min-[480px]:grid-cols-4">
                  <div className="flex flex-col items-center p-4 bg-muted rounded-lg text-center">
                    <Link2 className="text-primary mb-2" size={20} />
                    <span className="text-xl font-semibold text-foreground tabular-nums">
                      {formatNumber(user.linked_accounts_count)}
                    </span>
                    <span className="text-xs text-muted-foreground mt-1">Linked Accounts</span>
                  </div>

                  <div className="flex flex-col items-center p-4 bg-muted rounded-lg text-center">
                    <Zap className="text-primary mb-2" size={20} />
                    <span className="text-xl font-semibold text-foreground tabular-nums">
                      {formatNumber(user.boosts_count)}
                    </span>
                    <span className="text-xs text-muted-foreground mt-1">Automation Rules</span>
                  </div>

                  <div className="flex flex-col items-center p-4 bg-muted rounded-lg text-center">
                    <Receipt className="text-primary mb-2" size={20} />
                    <span className="text-xl font-semibold text-foreground tabular-nums">
                      {formatNumber(user.transactions_count)}
                    </span>
                    <span className="text-xs text-muted-foreground mt-1">Transactions</span>
                  </div>

                  <div className="flex flex-col items-center p-4 bg-muted rounded-lg text-center">
                    <PiggyBank className="text-primary mb-2" size={20} />
                    <span className="text-xl font-semibold text-foreground tabular-nums">
                      {formatNumber(user.budgets_count)}
                    </span>
                    <span className="text-xs text-muted-foreground mt-1">Budgets</span>
                  </div>
                </div>
              </section>
            </div>

            <Button variant="outline" aria-expanded={showDiagnostics} onClick={() => setDiagnosticUserId(showDiagnostics ? null : user.id)}>
              {showDiagnostics ? 'Hide RevenueCat diagnostics' : 'Show RevenueCat diagnostics'}
            </Button>
            {showDiagnostics && <RevenueCatUserPanel key={user.id} userId={user.id} />}

            <DialogFooter>
              <Button variant="outline" onClick={onClose}>
                Close
              </Button>
            </DialogFooter>
          </>
        ) : (
          <div className="flex items-center justify-center py-12 text-muted-foreground">
            <span>No user data available</span>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
