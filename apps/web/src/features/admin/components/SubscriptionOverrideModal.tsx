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
import type { AdminSubscriptionTierInputBodyTier } from '@boostedbudget/api-client/models'
import { useAdminSubscriptionDetail, useUpdateUserTier } from '../hooks/useAdminSubscription'
import './SubscriptionOverrideModal.css'

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

    return (
        <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
            <DialogContent className="subscription-override-modal" aria-describedby="sub-override-desc">
                <DialogHeader>
                    <DialogTitle className="subscription-override-modal__title">
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
                    <div className="subscription-override-modal__loading">
                        <Loader2 className="subscription-override-modal__spinner" />
                        <span>Loading subscription details…</span>
                    </div>
                ) : sub ? (
                    <div className="subscription-override-modal__body">
                        {/* Current state hero */}
                        <div className="subscription-override-modal__current">
                            <div className="subscription-override-modal__current-tier">
                                <span className="subscription-override-modal__current-label">Effective tier</span>
                                <span className="subscription-override-modal__tier-badge subscription-override-modal__tier-badge--active">
                                    {TIER_DISPLAY[sub.effective_tier] ?? sub.effective_tier}
                                </span>
                            </div>

                            <div className="subscription-override-modal__meta">
                                <span>
                                    <Shield size={14} /> Base: {TIER_DISPLAY[sub.base_tier] ?? sub.base_tier}
                                </span>
                                <span>
                                    <TrendingUp size={14} /> Status: {sub.subscription_status}
                                </span>
                            </div>

                            {hasOverride && (
                                <div
                                    className={`subscription-override-modal__override-badge ${isExpired ? 'subscription-override-modal__override-badge--expired' : ''}`}
                                >
                                    {isExpired ? (
                                        <AlertTriangle size={14} />
                                    ) : (
                                        <CheckCircle2 size={14} />
                                    )}
                                    Manual override active{isExpired ? ' (expired)' : ''}
                                    {sub.manual_tier_ends_at && (
                                        <span className="subscription-override-modal__override-expiry">
                                            <Clock size={12} />
                                            {isExpired ? 'Expired' : 'Expires'}: {formatDate(sub.manual_tier_ends_at)}
                                        </span>
                                    )}
                                </div>
                            )}
                        </div>

                        {/* Limits summary */}
                        {sub.limits && (
                            <div className="subscription-override-modal__limits">
                                <h4 className="subscription-override-modal__section-title">
                                    <Layers size={14} /> Effective Limits
                                </h4>
                                <div className="subscription-override-modal__limits-grid">
                                    <div className="subscription-override-modal__limit">
                                        <span className="subscription-override-modal__limit-val">
                                            {sub.limits.accounts === -1 ? '∞' : sub.limits.accounts}
                                        </span>
                                        <span className="subscription-override-modal__limit-key">Accounts</span>
                                    </div>
                                    <div className="subscription-override-modal__limit">
                                        <span className="subscription-override-modal__limit-val">
                                            {sub.limits.transaction_history_days === -1
                                                ? '∞'
                                                : `${sub.limits.transaction_history_days}d`}
                                        </span>
                                        <span className="subscription-override-modal__limit-key">History</span>
                                    </div>
                                    <div className="subscription-override-modal__limit">
                                        <span className="subscription-override-modal__limit-val">
                                            {sub.limits.executions_per_day === -1 ? '∞' : sub.limits.executions_per_day}
                                        </span>
                                        <span className="subscription-override-modal__limit-key">Runs/day</span>
                                    </div>
                                </div>
                            </div>
                        )}

                        {/* Form */}
                        <div className="subscription-override-modal__form">
                            <h4 className="subscription-override-modal__section-title">Set Override</h4>

                            <label className="subscription-override-modal__field">
                                <span className="subscription-override-modal__field-label">New Tier *</span>
                                <select
                                    id="override-tier-select"
                                    className="subscription-override-modal__select"
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

                            <label className="subscription-override-modal__field">
                                <span className="subscription-override-modal__field-label">Reason *</span>
                                <textarea
                                    id="override-reason-input"
                                    className="subscription-override-modal__textarea"
                                    value={reason}
                                    onChange={(e) => setReason(e.target.value)}
                                    placeholder="e.g. Gifted upgrade, trial extension, support case #123..."
                                    rows={2}
                                    disabled={isSaving}
                                />
                            </label>

                            <label className="subscription-override-modal__field">
                                <span className="subscription-override-modal__field-label">
                                    Expires at <em>(optional — leave blank for permanent)</em>
                                </span>
                                <DatePicker
                                    selected={expiresAt}
                                    onChange={(date: Date | null) => setExpiresAt(date)}
                                    showTimeSelect
                                    timeIntervals={15}
                                    dateFormat="MMM d, yyyy h:mm aa"
                                    placeholderText="Select expiry date…"
                                    className="subscription-override-modal__input"
                                    disabled={isSaving}
                                    isClearable
                                    minDate={new Date()}
                                />
                            </label>
                        </div>
                    </div>
                ) : (
                    <div className="subscription-override-modal__loading">
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
                                <Loader2 size={16} className="subscription-override-modal__spinner-inline" />
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
