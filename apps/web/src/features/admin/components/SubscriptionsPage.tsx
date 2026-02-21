/**
 * SubscriptionsPage Component
 * Issue #646: Admin Subscription Management UI
 *
 * Admin page for viewing all users' subscription tiers and applying manual overrides.
 * Reuses the user list from the admin API and opens a SubscriptionOverrideModal for editing.
 */

import { useState } from 'react'
import { useListAdminUsers } from '@boostedbudget/api-client/endpoints/admin/admin'
import {
    Search,
    CreditCard,
    ChevronLeft,
    ChevronRight,
    AlertCircle,
    Shield,
    CheckCircle2,
    Clock,
    Edit2,
} from 'lucide-react'
import { useUserFilters } from '../hooks/useUserFilters'
import { SubscriptionOverrideModal } from './SubscriptionOverrideModal'
import './SubscriptionsPage.css'

const TIER_OPTIONS = [
    { value: '', label: 'All Tiers' },
    { value: 'trial', label: 'Trial' },
    { value: 'premium', label: 'Premium' },
    { value: 'household_2', label: 'Household (2)' },
    { value: 'household_3', label: 'Household (3)' },
    { value: 'household_4', label: 'Household (4)' },
    { value: 'household_5', label: 'Household (5+)' },
]

const TIER_DISPLAY: Record<string, string> = {
    trial: 'Trial',
    premium: 'Premium',
    household_2: 'Household 2',
    household_3: 'Household 3',
    household_4: 'Household 4',
    household_5: 'Household 5+',
}

const STATUS_ICON: Record<string, React.ReactNode> = {
    active: <CheckCircle2 size={13} className="subscriptions-page__status-icon--active" />,
    trial: <Clock size={13} className="subscriptions-page__status-icon--trial" />,
    trialing: <Clock size={13} className="subscriptions-page__status-icon--trial" />,
    inactive: <AlertCircle size={13} className="subscriptions-page__status-icon--inactive" />,
    cancelled: <AlertCircle size={13} className="subscriptions-page__status-icon--inactive" />,
    past_due: <AlertCircle size={13} className="subscriptions-page__status-icon--past-due" />,
}

interface SelectedUser {
    id: string
    email: string
}

/**
 * SubscriptionsPage - Admin subscription management page
 */
export function SubscriptionsPage() {
    const { filters, setSearch, setTier, setPage, apiParams, hasActiveFilters } = useUserFilters()
    const [selected, setSelected] = useState<SelectedUser | null>(null)

    const { data, isLoading, error } = useListAdminUsers(apiParams)

    const totalPages = data?.total_pages ?? 0
    const currentPage = data?.page ?? 1
    const totalCount = data?.total_count ?? 0
    const hasMore = data?.has_more ?? false
    const users = data?.users ?? []
    const isEmpty = !isLoading && users.length === 0

    return (
        <div className="subscriptions-page">
            {/* Header */}
            <header className="subscriptions-page__header">
                <div className="subscriptions-page__header-inner">
                    <h1 className="subscriptions-page__title">
                        <CreditCard size={22} />
                        Subscriptions
                        {totalCount > 0 && (
                            <span className="subscriptions-page__count">{totalCount} users</span>
                        )}
                    </h1>
                    <p className="subscriptions-page__subtitle">
                        View and manually override user subscription tiers. Changes sync to Clerk metadata immediately.
                    </p>
                </div>
            </header>

            {/* Filters */}
            <div className="subscriptions-page__filters">
                <div className="subscriptions-page__search-wrapper">
                    <Search size={16} className="subscriptions-page__search-icon" aria-hidden="true" />
                    <input
                        id="subscriptions-search"
                        type="text"
                        className="subscriptions-page__search-input"
                        placeholder="Search by email…"
                        value={filters.search}
                        onChange={(e) => setSearch(e.target.value)}
                        aria-label="Search users by email"
                    />
                </div>

                <select
                    id="subscriptions-tier-filter"
                    className="subscriptions-page__tier-select"
                    value={filters.tier}
                    onChange={(e) => setTier(e.target.value)}
                    aria-label="Filter by subscription tier"
                >
                    {TIER_OPTIONS.map((opt) => (
                        <option key={opt.value} value={opt.value}>
                            {opt.label}
                        </option>
                    ))}
                </select>
            </div>

            {/* Content */}
            {isLoading ? (
                <div className="subscriptions-page__loading" role="status">
                    <div className="subscriptions-page__loading-spinner" />
                    <span>Loading users…</span>
                </div>
            ) : error ? (
                <div className="subscriptions-page__error">
                    <AlertCircle size={20} />
                    <span>Failed to load users. Please try again.</span>
                </div>
            ) : isEmpty ? (
                <div className="subscriptions-page__empty">
                    <CreditCard size={36} className="subscriptions-page__empty-icon" />
                    <span>
                        No users found{hasActiveFilters ? '. Try adjusting your filters.' : '.'}
                    </span>
                </div>
            ) : (
                <>
                    <div className="subscriptions-page__table-wrapper">
                        <table className="subscriptions-page__table">
                            <thead>
                                <tr>
                                    <th>User</th>
                                    <th>Tier</th>
                                    <th>Status</th>
                                    <th>Region</th>
                                    <th>Accounts</th>
                                    <th className="subscriptions-page__th--action">Action</th>
                                </tr>
                            </thead>
                            <tbody>
                                {users.map((user) => (
                                    <tr key={user.id} className="subscriptions-page__row">
                                        <td className="subscriptions-page__email-cell">
                                            {user.email}
                                            {user.is_admin && (
                                                <Shield
                                                    size={13}
                                                    className="subscriptions-page__admin-icon"
                                                    aria-label="Administrator"
                                                />
                                            )}
                                        </td>
                                        <td>
                                            <span
                                                className={`subscriptions-page__tier-badge subscriptions-page__tier-badge--${user.subscription_tier}`}
                                            >
                                                {TIER_DISPLAY[user.subscription_tier] ?? user.subscription_tier}
                                            </span>
                                        </td>
                                        <td>
                                            <span
                                                className={`subscriptions-page__status-badge subscriptions-page__status-badge--${user.subscription_status}`}
                                            >
                                                {STATUS_ICON[user.subscription_status]}
                                                {user.subscription_status}
                                            </span>
                                        </td>
                                        <td className="subscriptions-page__region">{user.primary_region}</td>
                                        <td className="subscriptions-page__numeric">{user.linked_accounts_count}</td>
                                        <td className="subscriptions-page__action-cell">
                                            <button
                                                id={`override-btn-${user.id}`}
                                                type="button"
                                                className="subscriptions-page__override-btn"
                                                onClick={() => setSelected({ id: user.id, email: user.email })}
                                                aria-label={`Override subscription for ${user.email}`}
                                            >
                                                <Edit2 size={14} />
                                                Override
                                            </button>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>

                    {/* Pagination */}
                    {totalPages > 1 && (
                        <div className="subscriptions-page__pagination">
                            <span className="subscriptions-page__page-info">
                                Page {currentPage} of {totalPages}
                            </span>
                            <div className="subscriptions-page__page-controls">
                                <button
                                    type="button"
                                    className="subscriptions-page__page-btn"
                                    onClick={() => setPage(currentPage - 1)}
                                    disabled={currentPage <= 1}
                                    aria-label="Previous page"
                                >
                                    <ChevronLeft size={16} />
                                    Previous
                                </button>
                                <button
                                    type="button"
                                    className="subscriptions-page__page-btn"
                                    onClick={() => setPage(currentPage + 1)}
                                    disabled={!hasMore}
                                    aria-label="Next page"
                                >
                                    Next
                                    <ChevronRight size={16} />
                                </button>
                            </div>
                        </div>
                    )}
                </>
            )}

            {/* Subscription Override Modal */}
            <SubscriptionOverrideModal
                userId={selected?.id ?? null}
                userEmail={selected?.email}
                open={!!selected}
                onClose={() => setSelected(null)}
            />
        </div>
    )
}
