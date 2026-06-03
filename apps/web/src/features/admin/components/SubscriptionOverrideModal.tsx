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
import type { AdminSubscriptionTierInputBodyTier } from '@boostedbudget/api-client/models'
import { useAdminSubscriptionDetail, useUpdateUserTier } from '../hooks/useAdminSubscription'

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

    // Derive initial form values from loaded subscription data
    const initialTier = useMemo(
        () => sub?.manual_tier_override ?? sub?.effective_tier ?? '',
        [sub]
    )

    // Form state initialized from derived values
    const [selectedTier, setSelectedTier] = useState(initialTier)
    const [reason, setReason] = useState('')
    const [expiresAt, setExpiresAt] = useState<Date | null>(
        sub?.manual_tier_ends_at ? new Date(sub.manual_tier_ends_at) : null
    )

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
                onSuccess: () => {
                    toast.success('Subscription tier updated successfully')
                    onClose()
                },
                onError: (err) => {
                    toast.error(`Failed to update tier: ${err.message}`)
                },
            }
        )
    }

    const hasOverride = !!sub?.manual_tier_override
    const isExpired =
        sub?.manual_tier_ends_at && new Date(sub.manual_tier_ends_at) < new Date()

    const inputClass = 'w-full px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm font-[inherit] transition-colors focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/15 disabled:opacity-60 disabled:cursor-not-allowed'

    return (
        <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
            <DialogContent className="max-w-[540px]" aria-describedby="sub-override-desc">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <CreditCard size={20} />
                        Subscription Override
                    </DialogTitle>
                    <DialogDescription id="sub-override-desc">
                        {userEmail ? (
                            <>
                                Managing subscription for <strong>{userEmail}</strong>
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
                            </div>

                            {hasOverride && (
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

                        {/* Limits summary */}
                        {sub.limits && (
                            <div className="flex flex-col gap-2.5">
                                <h4 className="text-[0.8rem] font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5 m-0 pb-1.5 border-b border-border">
                                    <Layers size={14} /> Effective Limits
                                </h4>
                                <div className="grid grid-cols-3 gap-3">
                                    <div className="flex flex-col items-center py-3 px-2 bg-muted border border-border rounded-lg text-center">
                                        <span className="text-xl font-bold text-primary tabular-nums">
                                            {sub.limits.accounts === -1 ? '∞' : sub.limits.accounts}
                                        </span>
                                        <span className="text-[0.7rem] text-muted-foreground mt-0.5 uppercase tracking-wider">Accounts</span>
                                    </div>
                                    <div className="flex flex-col items-center py-3 px-2 bg-muted border border-border rounded-lg text-center">
                                        <span className="text-xl font-bold text-primary tabular-nums">
                                            {sub.limits.transaction_history_days === -1
                                                ? '∞'
                                                : `${sub.limits.transaction_history_days}d`}
                                        </span>
                                        <span className="text-[0.7rem] text-muted-foreground mt-0.5 uppercase tracking-wider">History</span>
                                    </div>
                                    <div className="flex flex-col items-center py-3 px-2 bg-muted border border-border rounded-lg text-center">
                                        <span className="text-xl font-bold text-primary tabular-nums">
                                            {sub.limits.executions_per_day === -1 ? '∞' : sub.limits.executions_per_day}
                                        </span>
                                        <span className="text-[0.7rem] text-muted-foreground mt-0.5 uppercase tracking-wider">Runs/day</span>
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
                                    disabled={isSaving}
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
                                <span className="text-[0.8125rem] font-medium text-foreground">Reason *</span>
                                <textarea
                                    id="override-reason-input"
                                    className={cn(inputClass, 'resize-y min-h-[4rem]')}
                                    value={reason}
                                    onChange={(e) => setReason(e.target.value)}
                                    placeholder="e.g. Gifted upgrade, trial extension, support case #123..."
                                    rows={2}
                                    disabled={isSaving}
                                />
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
                                    disabled={isSaving}
                                    isClearable
                                    minDate={new Date()}
                                />
                            </label>
                        </div>
                    </div>
                ) : (
                    <div className="flex flex-col items-center justify-center py-12 gap-3 text-muted-foreground">
                        <span>No subscription data found for this user.</span>
                    </div>
                )}

                <DialogFooter>
                    <Button variant="outline" onClick={onClose} disabled={isSaving}>
                        Cancel
                    </Button>
                    <Button
                        id="save-override-btn"
                        onClick={handleSubmit}
                        disabled={isSaving || !selectedTier || !reason.trim() || isLoading}
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
    )
}
