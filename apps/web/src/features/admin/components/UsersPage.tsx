/**
 * UsersPage Component
 * Issue #644 + #646: Admin User Management + Subscription Management UI
 *
 * Unified admin page for viewing users, managing subscriptions, and applying
 * manual tier overrides. Merged from SubscriptionsPage.
 */

import { useState, useEffect } from 'react'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { useListAdminUsers, useGetAdminUserDetail } from '@boostedbudget/api-client/endpoints/admin/admin'
import {
  Search,
  Users,
  ChevronLeft,
  ChevronRight,
  AlertCircle,
  Shield,
  CheckCircle2,
  Clock,
  Edit2,
} from 'lucide-react'
import { useUserFilters } from '../hooks/useUserFilters'
import { useRegions } from '../../../shared/hooks/useRegions'
import { UserDetailModal } from './UserDetailModal'
import { SubscriptionOverrideModal } from './SubscriptionOverrideModal'
import './UsersPage.css'

const TIER_OPTIONS = [
  { value: '', label: 'All Tiers' },
  { value: 'premium', label: 'Premium' },
  { value: 'household_2', label: 'Household (2)' },
  { value: 'household_3', label: 'Household (3)' },
  { value: 'household_4', label: 'Household (4)' },
  { value: 'household_5', label: 'Household (5+)' },
]

const TIER_DISPLAY: Record<string, string> = {
  premium: 'Premium',
  household_2: 'Household 2',
  household_3: 'Household 3',
  household_4: 'Household 4',
  household_5: 'Household 5+',
}

const STATUS_ICON: Record<string, React.ReactNode> = {
  active: <CheckCircle2 size={13} className="users-page__status-icon--active" />,
  trial: <Clock size={13} className="users-page__status-icon--trial" />,
  trialing: <Clock size={13} className="users-page__status-icon--trial" />,
  inactive: <AlertCircle size={13} className="users-page__status-icon--inactive" />,
  cancelled: <AlertCircle size={13} className="users-page__status-icon--inactive" />,
  past_due: <AlertCircle size={13} className="users-page__status-icon--past-due" />,
}

interface OverrideStatus {
  label: string
  variant: string
}

function getOverrideStatus(
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

const formatDate = (dateString: string | undefined | null): string => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

interface SelectedUser {
  id: string
  email: string
}

/**
 * UsersPage - Unified admin user + subscription management page
 */
export function UsersPage() {
  const { filters, setSearch, setTier, setPage, apiParams, hasActiveFilters } = useUserFilters()
  const [selectedUserId, setSelectedUserId] = useState<string | null>(null)
  const [overrideUser, setOverrideUser] = useState<SelectedUser | null>(null)
  const trackEvent = useTrackEvent()
  const { getRegionWithFlag } = useRegions()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'users' })
  }, [trackEvent])

  const { data, isLoading, error } = useListAdminUsers(apiParams)

  const { data: userDetail, isLoading: detailLoading } = useGetAdminUserDetail(
    selectedUserId ?? '',
    { query: { enabled: !!selectedUserId } }
  )

  const totalPages = data?.total_pages ?? 0
  const currentPage = data?.page ?? 1
  const totalCount = data?.total_count ?? 0
  const hasMore = data?.has_more ?? false
  const users = data?.users ?? []
  const isEmpty = !isLoading && users.length === 0

  return (
    <div className="users-page">
      {/* Header */}
      <header className="users-page__header">
        <div className="users-page__header-inner">
          <h1 className="users-page__title">
            Users
            {totalCount > 0 && (
              <span className="users-page__count">{totalCount} users</span>
            )}
          </h1>
          <p className="users-page__subtitle">
            View and manage users. Click a row to see details, or use Override to set a manual subscription tier.
          </p>
        </div>
      </header>

      {/* Filters */}
      <div className="users-page__filters">
        <div className="users-page__search-wrapper">
          <Search size={16} className="users-page__search-icon" aria-hidden="true" />
          <input
            type="text"
            className="users-page__search-input"
            placeholder="Search by email..."
            value={filters.search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label="Search users by email"
          />
        </div>

        <select
          className="users-page__tier-select"
          value={filters.tier}
          onChange={(e) => setTier(e.target.value)}
          aria-label="Filter by tier"
        >
          {TIER_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </div>

      {/* Content */}
      {isLoading ? (
        <div className="users-page__loading" role="status">
          <div className="users-page__loading-spinner" />
          <span>Loading users...</span>
        </div>
      ) : error ? (
        <div className="users-page__error">
          <AlertCircle size={20} className="users-page__error-icon" />
          <span>Failed to load users. Please try again.</span>
        </div>
      ) : isEmpty ? (
        <div className="users-page__empty">
          <Users className="users-page__empty-icon" aria-hidden="true" />
          <span>No users found{hasActiveFilters ? '. Try adjusting your filters.' : '.'}</span>
        </div>
      ) : (
        <>
          <div className="users-page__table-wrapper">
            <table className="users-page__table">
              <thead>
                <tr>
                  <th>Email</th>
                  <th>Tier</th>
                  <th>Status</th>
                  <th>Override Status</th>
                  <th>Region</th>
                  <th>Last Login</th>
                  <th className="users-page__numeric">Accounts</th>
                  <th className="users-page__numeric">Rules</th>
                  <th className="users-page__numeric">Referrals</th>
                  <th className="users-page__th--action">Action</th>
                </tr>
              </thead>
              <tbody>
                {users.map((user) => {
                  const effectiveTier = user.manual_tier_override ?? user.subscription_tier
                  const overrideStatus = getOverrideStatus(user.manual_tier_override, user.manual_tier_ends_at)

                  return (
                    <tr
                      key={user.id}
                      className="users-page__row--clickable"
                      onClick={() => setSelectedUserId(user.id)}
                    >
                      <td className="users-page__email-cell">
                        {user.email}
                        {user.is_admin && (
                          <Shield className="users-page__admin-icon" size={14} aria-label="Administrator" />
                        )}
                      </td>
                      <td>
                        <span className={`users-page__tier-badge users-page__tier-badge--${effectiveTier}`}>
                          {TIER_DISPLAY[effectiveTier] ?? effectiveTier}
                        </span>
                      </td>
                      <td>
                        <span className={`users-page__status-badge users-page__status-badge--${user.subscription_status}`}>
                          {STATUS_ICON[user.subscription_status]}
                          {user.subscription_status}
                        </span>
                      </td>
                      <td>
                        {overrideStatus ? (
                          <span className={`users-page__override-status users-page__override-status--${overrideStatus.variant}`}>
                            {overrideStatus.label}
                          </span>
                        ) : (
                          <span className="users-page__override-status--none">—</span>
                        )}
                      </td>
                      <td className="users-page__region">{getRegionWithFlag(user.primary_region)}</td>
                      <td>{formatDate(user.last_login_at)}</td>
                      <td className="users-page__numeric">{user.linked_accounts_count}</td>
                      <td className="users-page__numeric">{user.automation_rules_count}</td>
                      <td className="users-page__numeric">{user.referrals_count}</td>
                      <td className="users-page__action-cell">
                        <button
                          type="button"
                          className="users-page__override-btn"
                          onClick={(e) => {
                            e.stopPropagation()
                            setOverrideUser({ id: user.id, email: user.email })
                          }}
                          aria-label={`Override subscription for ${user.email}`}
                        >
                          <Edit2 size={14} />
                          Override
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="users-page__pagination">
              <span className="users-page__page-info">
                Page {currentPage} of {totalPages}
              </span>
              <div className="users-page__page-controls">
                <button
                  type="button"
                  className="users-page__page-btn"
                  onClick={() => setPage(currentPage - 1)}
                  disabled={currentPage <= 1}
                  aria-label="Previous page"
                >
                  <ChevronLeft size={16} />
                  Previous
                </button>
                <button
                  type="button"
                  className="users-page__page-btn"
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

      {/* User Detail Modal */}
      <UserDetailModal
        user={userDetail}
        loading={detailLoading}
        open={!!selectedUserId}
        onClose={() => setSelectedUserId(null)}
      />

      {/* Subscription Override Modal */}
      <SubscriptionOverrideModal
        userId={overrideUser?.id ?? null}
        userEmail={overrideUser?.email}
        open={!!overrideUser}
        onClose={() => setOverrideUser(null)}
      />
    </div>
  )
}
