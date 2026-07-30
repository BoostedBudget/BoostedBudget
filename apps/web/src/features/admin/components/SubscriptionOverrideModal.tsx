/**
 * SubscriptionOverrideModal
 * Issue #646: Admin Subscription Management UI
 *
 * Modal form for an admin to view and/or override a user's subscription tier,
 * set a reason, and optionally set an expiration date for the override.
 */

import { useState, useMemo } from 'react'
import DatePicker from 'react-datepicker'
import 'react-datepicker/dist/react-datepicker.css'
import { toast } from 'sonner'
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
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import {
    Loader2,
    CreditCard,
    Shield,
    AlertTriangle,
    CheckCircle2,
    Clock,
    TrendingUp,
    Layers,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { getSubscriptionPresentation } from '@/features/subscription/lib/subscriptionPresentation'
import type { AdminSubscriptionTierInputBodyTier } from '@boostedbudget/api-client/models'
import { useAdminSubscriptionDetail, useUpdateUserTier, useRemoveUserTierOverride } from '../hooks/useAdminSubscription'
import { isOverrideActive } from '../lib/overrideStatus'
import { KNOWN_TIER_NAMES } from '../lib/knownTiers'

/** The fixed type-to-confirm literal for removing an override — never PII like the user's
 * email, same rationale as DisbandHouseholdDialog's `CONFIRM_TEXT = 'DISBAND'`. */
const REMOVE_CONFIRM_TEXT = 'REMOVE'

const TIER_DISPLAY: Record<string, string> = {
    premium: 'Premium',
    household_2: 'Household (2)',
    household_3: 'Household (3)',
    household_4: 'Household (4)',
    household_5: 'Household (5+)',
}

const TIER_OPTIONS = Object.entries(TIER_DISPLAY)

function formatDate(iso: string | undefined | null): string {
    if (!iso) return '—'
    return new Date(iso).toLocaleDateString('en-US', {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
    })
}

export interface SubscriptionOverrideModalProps {
    /** The BoostedBudget internal user ID */
    userId: string | null
    userEmail?: string
    open: boolean
    onClose: () => void
}

export function SubscriptionOverrideModal(props: SubscriptionOverrideModalProps) {
    const { data: sub, isLoading } = useAdminSubscriptionDetail(props.userId)
    // Use sub as key so the inner form resets its state when subscription data loads
    return <SubscriptionOverrideForm {...props} sub={sub} isLoading={isLoading} key={sub ? 'loaded' : 'loading'} />
}

function SubscriptionOverrideForm({
    userId,
    userEmail,
    open,
    onClose,
    sub,
    isLoading,
}: SubscriptionOverrideModalProps & {
    sub: ReturnType<typeof useAdminSubscriptionDetail>['data']
    isLoading: boolean
}) {
    const { mutate: updateTier, isPending: isSaving } = useUpdateUserTier(userId ?? '')
    const { mutate: removeOverride, isPending: isRemoving } = useRemoveUserTierOverride(userId ?? '')
    const trackEvent = useTrackEvent()

    // Derive initial form values from loaded subscription data
    const initialTier = useMemo(
        () => sub?.manual_tier_override ?? sub?.effective_tier ?? '',
        [sub]
    )

    // Form state initialized from derived values
    const [selectedTier, setSelectedTier] = useState(initialTier)
    // Reason is shared by both the Save Override and Remove Override actions — there is only
    // ever one destructive/mutating action in flight at a time, so a single field governs both.
    const [reason, setReason] = useState('')
    const [expiresAt, setExpiresAt] = useState<Date | null>(
        sub?.manual_tier_ends_at ? new Date(sub.manual_tier_ends_at) : null
    )
    const [confirmRemove, setConfirmRemove] = useState(false)

    const isBusy = isSaving || isRemoving

    function handleSubmit() {
        if (!userId || !selectedTier || !reason.trim()) return

        updateTier(
            {
                userId,
                data: {
                    tier: selectedTier as AdminSubscriptionTierInputBodyTier,
                    reason: reason.trim(),
                    expires_at: expiresAt ? expiresAt.toISOString() : null,
                },
            },
            {
                onSuccess: (data) => {
                    // Defect 5 / G4: the backend appends a seat/offboard warning to `message`
                    // when comping a household owner DOWN to a smaller tier risks LIFO-offboarding
                    // active members (admin_user_service.go:253-267) — a destructive side effect of
                    // what the admin intends as a gift. A plain green success toast would bury that
                    // warning; surface it as a warning-styled toast instead so it reads as "this
                    // needs your attention", never as an unqualified success.
                    const message = data?.message
                    if (message && message.includes('Warning:')) {
                        toast.warning(message)
                    } else {
                        toast.success('Subscription tier updated successfully')
                    }
                    trackEvent(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_APPLIED, {
                        tier: selectedTier,
                        had_expiry: !!expiresAt,
                    })
                    onClose()
                },
                onError: (err) => {
                    toast.error(`Failed to update tier: ${err.message}`)
                },
            }
        )
    }

    function handleRemove() {
        if (!userId || !reason.trim()) return

        // Capture what's being removed before the mutation resolves and `sub` refetches —
        // the analytics payload describes the override that WAS active, not the post-removal state.
        const removedTier = sub?.manual_tier_override
        const hadExpiry = !!sub?.manual_tier_ends_at

        removeOverride(
            { userId, data: { reason: reason.trim() } },
            {
                onSuccess: () => {
                    toast.success('Subscription override removed successfully')
                    trackEvent(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED, {
                        tier: removedTier,
                        had_expiry: hadExpiry,
                    })
                    setConfirmRemove(false)
                    onClose()
                },
                onError: (err) => {
                    toast.error(`Failed to remove override: ${err.message}`)
                    setConfirmRemove(false)
                },
            }
        )
    }

    // `hasOverride` is RAW presence — an override value exists on the record, valid or not. This
    // gates the Remove Override button and the removal-confirmation labels below: an admin must
    // still be able to see and clear a malformed/garbage override, exactly as the backend
    // preserves it in the response instead of 500ing (admin_subscriptions_test.go
    // TestGetUserSubscription_MalformedOverride_FallsBackToRawTier_NotServerError — "that response
    // is what the override modal needs to render the Remove Override button").
    const hasOverride = !!sub?.manual_tier_override
    const isExpired =
        sub?.manual_tier_ends_at && new Date(sub.manual_tier_ends_at) < new Date()

    // Issue #1413 AC6: this used to be `hasOverride && !isExpired`, handed straight to `isComped`
    // below without ever checking whether the override actually names a real, currently-configured
    // tier. manual_tier_override is an unconstrained VARCHAR(50) with no CHECK constraint, so a
    // legacy row / direct SQL write / write made while tier config was nil can hold a value naming
    // no tier at all (e.g. 'not_a_tier'). `resolveKind` in subscriptionPresentation.ts gives
    // `isComped` absolute precedence over the server-computed `effectiveStatus` passed one line
    // below, so that unvalidated value was silently overriding the server's correct answer —
    // rendering "Complimentary" and a green "Manual override active" chip for a user with zero
    // actual write access. `isOverrideActive` (shared with UsersPage.tsx via the same
    // `KNOWN_TIER_NAMES` set — not a second copy, which is exactly the drift that caused this) is
    // the fix.
    //
    // `isKnownTierOverride` ignores expiry (endsAt omitted) — it answers "does this override name a
    // real tier at all", independent of whether it has lapsed. `isLiveOverride` folds in expiry too
    // — "is this override granting a comp right now". These are deliberately two different
    // questions (the tier axis vs. the expiry axis): a malformed override must be treated as no
    // override at all on the access-representation axis (`showOverrideChip` below, and `isComped`),
    // while an expired-but-validly-named override still renders — just in its existing amber
    // "(expired)" state, not hidden — because expiring is a normal lifecycle event, not garbage
    // data.
    const isKnownTierOverride = isOverrideActive(sub?.manual_tier_override, undefined, KNOWN_TIER_NAMES)
    const isLiveOverride = isOverrideActive(sub?.manual_tier_override, sub?.manual_tier_ends_at, KNOWN_TIER_NAMES)
    const showOverrideChip = hasOverride && isKnownTierOverride

    // G10 (AC 6): this is the surface where an admin actually decides to grant or revoke a
    // comp, so it must show both raw AND effective status — not raw alone. Reuses the shared
    // derivation (and its existing badge variants) rather than a bespoke mapping, exactly like
    // UsersPage's "Comped" chip. `AdminSubscriptionDetailOutputBody` has no server-computed
    // `is_comped` — only `effective_status` (does this user have write access right now), which
    // answers a different question than "is this user comped by a LIVE override" — an expired
    // (or malformed) override must not still read as comped, so isComped is derived from
    // `isLiveOverride`, not from effective_status.
    const presentation = getSubscriptionPresentation({
        status: sub?.subscription_status ?? '',
        effectiveStatus: sub?.effective_status,
        isComped: isLiveOverride,
        compEndsAt: sub?.manual_tier_ends_at ?? undefined,
    })

    // Fix 6: a user's own GDPR deletion request beats an admin comp — the backend deliberately
    // leaves the deletion path alone, so comping will NOT cancel it. Prefer the scheduled date
    // (when the notice period has resolved into a firm date) over the originally-requested one.
    const deletionDate = sub?.deletion_scheduled_at ?? sub?.deletion_requested_at

    const inputClass = 'w-full px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm font-[inherit] transition-colors focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/15 disabled:opacity-60 disabled:cursor-not-allowed'

    // Names for the Remove Override confirmation: the tier being removed and the base tier the
    // user falls back to. Read from the admin detail response (`base_tier`), never guessed.
    const removingTierLabel = sub?.manual_tier_override
        ? (TIER_DISPLAY[sub.manual_tier_override] ?? sub.manual_tier_override)
        : ''
    const fallbackTierLabel = sub?.base_tier ? (TIER_DISPLAY[sub.base_tier] ?? sub.base_tier) : ''

    return (
        <>
            <Dialog open={open} onOpenChange={(o) => !o && !isBusy && onClose()}>
                <DialogContent className="max-w-[540px]" aria-describedby="sub-override-desc">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <CreditCard size={20} />
                            Subscription Override
                        </DialogTitle>
                        <DialogDescription id="sub-override-desc">
                            {userEmail ? (
                                <>
                                    Managing subscription for <strong data-ph-mask>{userEmail}</strong>
                                </>
                            ) : (
                                "Manually override this user's subscription tier"
                            )}
                        </DialogDescription>
                    </DialogHeader>

                    {isLoading ? (
                        <div className="flex flex-col items-center justify-center py-12 gap-3 text-muted-foreground">
                            <Loader2 className="w-8 h-8 animate-spin" />
                            <span>Loading subscription details…</span>
                        </div>
                    ) : sub ? (
                        <div className="flex flex-col gap-5 my-2">
                            {/* Current state hero */}
                            <div className="bg-muted border border-border rounded-[0.625rem] p-4 flex flex-col gap-2.5">
                                <div className="flex items-center justify-between flex-wrap gap-2">
                                    <span className="text-[0.8125rem] text-muted-foreground font-medium">Effective tier</span>
                                    <span className="inline-flex items-center px-3 py-1 rounded-full text-[0.8125rem] font-semibold tracking-wide bg-primary text-primary-foreground shadow-sm">
                                        {TIER_DISPLAY[sub.effective_tier] ?? sub.effective_tier}
                                    </span>
                                </div>

                                <div className="flex gap-4 flex-wrap text-[0.8125rem] text-muted-foreground">
                                    <span className="inline-flex items-center gap-1">
                                        <Shield size={14} /> Base: {TIER_DISPLAY[sub.base_tier] ?? sub.base_tier}
                                    </span>
                                    <span className="inline-flex items-center gap-1">
                                        <TrendingUp size={14} /> Status: {sub.subscription_status}
                                    </span>
                                    <span className="inline-flex items-center gap-1">
                                        Effective:
                                        <Badge
                                            variant={presentation.badgeVariant}
                                            size="sm"
                                            aria-label={`Effective status: ${presentation.badgeLabel}`}
                                            title={`Effective status: ${presentation.badgeLabel}`}
                                        >
                                            {presentation.badgeLabel}
                                        </Badge>
                                    </span>
                                </div>

                                {showOverrideChip && (
                                    <div
                                        className={cn(
                                            'inline-flex items-center gap-1.5 flex-wrap text-[0.8rem] font-medium px-2.5 py-1.5 rounded-md border',
                                            isExpired
                                                ? 'bg-warning/10 text-warning border-warning/20'
                                                : 'bg-success/10 text-success border-success/20'
                                        )}
                                    >
                                        {isExpired ? (
                                            <AlertTriangle size={14} />
                                        ) : (
                                            <CheckCircle2 size={14} />
                                        )}
                                        Manual override active{isExpired ? ' (expired)' : ''}
                                        {sub.manual_tier_ends_at && (
                                            <span className="inline-flex items-center gap-1 font-normal opacity-85">
                                                <Clock size={12} />
                                                {isExpired ? 'Expired' : 'Expires'}: {formatDate(sub.manual_tier_ends_at)}
                                            </span>
                                        )}
                                    </div>
                                )}
                            </div>

                            {deletionDate && (
                                <Alert variant="warning">
                                    <AlertTriangle className="h-4 w-4" aria-hidden="true" />
                                    <AlertTitle>Account deletion requested</AlertTitle>
                                    <AlertDescription>
                                        This user has requested account deletion (scheduled {formatDate(deletionDate)}).
                                        A comp will NOT cancel it.
                                    </AlertDescription>
                                </Alert>
                            )}

                            {/* Limits summary */}
                            {sub.limits && (
                                <div className="flex flex-col gap-2.5">
                                    <h4 className="text-[0.8rem] font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5 m-0 pb-1.5 border-b border-border">
                                        <Layers size={14} /> Effective Limits
                                    </h4>
                                    <div className="grid grid-cols-1 gap-3">
                                        <div className="flex flex-col items-center py-3 px-2 bg-muted border border-border rounded-lg text-center">
                                            <span className="text-xl font-bold text-primary tabular-nums">
                                                {sub.limits.transaction_history_days === -1
                                                    ? '∞'
                                                    : `${sub.limits.transaction_history_days}d`}
                                            </span>
                                            <span className="text-[0.7rem] text-muted-foreground mt-0.5 uppercase tracking-wider">History</span>
                                        </div>
                                    </div>
                                </div>
                            )}

                            {/* Form */}
                            <div className="flex flex-col gap-3.5">
                                <h4 className="text-[0.8rem] font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5 m-0 pb-1.5 border-b border-border">
                                    Set Override
                                </h4>

                                <label className="flex flex-col gap-1.5 cursor-text">
                                    <span className="text-[0.8125rem] font-medium text-foreground">New Tier *</span>
                                    <select
                                        id="override-tier-select"
                                        className={inputClass}
                                        value={selectedTier}
                                        onChange={(e) => setSelectedTier(e.target.value)}
                                        disabled={isBusy}
                                    >
                                        <option value="">— select tier —</option>
                                        {TIER_OPTIONS.map(([value, label]) => (
                                            <option key={value} value={value}>
                                                {label}
                                            </option>
                                        ))}
                                    </select>
                                </label>

                                <label className="flex flex-col gap-1.5 cursor-text">
                                    <span className="text-[0.8125rem] font-medium text-foreground">
                                        Expires at <span className="font-normal text-muted-foreground">(optional — leave blank for permanent)</span>
                                    </span>
                                    <DatePicker
                                        selected={expiresAt}
                                        onChange={(date: Date | null) => setExpiresAt(date)}
                                        showTimeSelect
                                        timeIntervals={15}
                                        dateFormat="MMM d, yyyy h:mm aa"
                                        placeholderText="Select expiry date…"
                                        className={inputClass}
                                        disabled={isBusy}
                                        isClearable
                                        minDate={new Date()}
                                    />
                                </label>
                            </div>

                            {/* Reason governs BOTH Save Override and Remove Override below — reused
                                rather than duplicated, so it sits outside the "Set Override" fieldset. */}
                            <label className="flex flex-col gap-1.5 cursor-text">
                                <span className="text-[0.8125rem] font-medium text-foreground">
                                    Reason * <span className="font-normal text-muted-foreground">(required to save or remove an override)</span>
                                </span>
                                <textarea
                                    id="override-reason-input"
                                    className={cn(inputClass, 'resize-y min-h-[4rem]')}
                                    value={reason}
                                    onChange={(e) => setReason(e.target.value)}
                                    placeholder="e.g. Gifted upgrade, trial extension, support case #123..."
                                    rows={2}
                                    disabled={isBusy}
                                />
                            </label>
                        </div>
                    ) : (
                        <div className="flex flex-col items-center justify-center py-12 gap-3 text-muted-foreground">
                            <span>No subscription data found for this user.</span>
                        </div>
                    )}

                    <DialogFooter>
                        <Button variant="outline" onClick={onClose} disabled={isBusy}>
                            Cancel
                        </Button>
                        {hasOverride && (
                            <Button
                                id="remove-override-btn"
                                variant="destructive"
                                onClick={() => setConfirmRemove(true)}
                                disabled={isBusy || !reason.trim() || isLoading}
                            >
                                {isRemoving ? (
                                    <>
                                        <Loader2 size={16} className="mr-1.5 animate-spin" />
                                        Removing…
                                    </>
                                ) : (
                                    'Remove Override'
                                )}
                            </Button>
                        )}
                        <Button
                            id="save-override-btn"
                            onClick={handleSubmit}
                            disabled={isBusy || !selectedTier || !reason.trim() || isLoading}
                        >
                            {isSaving ? (
                                <>
                                    <Loader2 size={16} className="mr-1.5 animate-spin" />
                                    Saving…
                                </>
                            ) : (
                                'Save Override'
                            )}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <ConfirmationDialog
                open={confirmRemove}
                onOpenChange={setConfirmRemove}
                onConfirm={handleRemove}
                variant="destructive"
                title="Remove subscription override?"
                description={
                    <>
                        This will remove the {removingTierLabel} override.{' '}
                        {userEmail ? <strong data-ph-mask>{userEmail}</strong> : 'This user'} will revert to their{' '}
                        {fallbackTierLabel} subscription.
                    </>
                }
                confirmButtonText="Remove Override"
                confirmText={REMOVE_CONFIRM_TEXT}
                loading={isRemoving}
            />
        </>
    )
}
