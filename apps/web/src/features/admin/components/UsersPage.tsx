/**
 * UsersPage Component
 * Issue #644: Admin User Management UI
 *
 * Admin page for viewing and managing users with search, filtering, and pagination.
 */

import { useState } from 'react'
import { useListAdminUsers, useGetAdminUserDetail } from '@boostedbudget/api-client/endpoints/admin/admin'
import { Search, Users, ChevronLeft, ChevronRight, AlertCircle } from 'lucide-react'
import { useUserFilters } from '../hooks/useUserFilters'
import { UserDetailModal } from './UserDetailModal'
import './UsersPage.css'

// Subscription tier options for filter
const TIER_OPTIONS = [
  { value: '', label: 'All Tiers' },
  { value: 'free', label: 'Free' },
  { value: 'premium_lite', label: 'Premium Lite' },
  { value: 'premium', label: 'Premium' },
  { value: 'premium_plus', label: 'Premium+' },
]

const formatDate = (dateString: string | undefined): string => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

const getTierBadgeClass = (tier: string): string =>
  `users-page__tier-badge users-page__tier-badge--${tier}`

const getStatusBadgeClass = (status: string): string =>
  `users-page__status-badge users-page__status-badge--${status}`

/**
 * UsersPage - Admin user management page
 */
export function UsersPage() {
  const { filters, setSearch, setTier, setPage, apiParams, hasActiveFilters } = useUserFilters()
  const [selectedUserId, setSelectedUserId] = useState<string | null>(null)

  // Fetch users list
  const { data, isLoading, error } = useListAdminUsers(apiParams)

  // Fetch selected user detail
  const { data: userDetail, isLoading: detailLoading } = useGetAdminUserDetail(
    selectedUserId ?? '',
    { query: { enabled: !!selectedUserId } }
  )

  // Pagination info
  const totalPages = data?.total_pages ?? 0
  const currentPage = data?.page ?? 1
  const totalCount = data?.total_count ?? 0
  const hasMore = data?.has_more ?? false

  // Handle search with debounce
  const handleSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setSearch(e.target.value)
  }

  // Handle tier filter change
  const handleTierChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    setTier(e.target.value)
  }

  // Handle row click
  const handleRowClick = (userId: string) => {
    setSelectedUserId(userId)
  }

  // Handle modal close
  const handleCloseModal = () => {
    setSelectedUserId(null)
  }

  // Loading state
  if (isLoading) {
    return (
      <div className="users-page">
        <div className="users-page__loading" role="status">
          <div className="users-page__loading-spinner" />
          <span>Loading users...</span>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="users-page">
        <div className="users-page__error">
          <AlertCircle className="users-page__error-icon" />
          <span>Failed to load users. Please try again.</span>
        </div>
      </div>
    )
  }

  // Empty state
  const isEmpty = !data?.users || data.users.length === 0

  return (
    <div className="users-page">
      {/* Header */}
      <header className="users-page__header">
        <h1 className="users-page__title">
          Users
          {totalCount > 0 && (
            <span className="users-page__count"> ({totalCount} users)</span>
          )}
        </h1>
      </header>

      {/* Filters */}
      <div className="users-page__filters">
        {/* Search */}
        <div className="users-page__search">
          <div className="users-page__search-wrapper">
            <Search className="users-page__search-icon" aria-hidden="true" />
            <input
              type="text"
              className="users-page__search-input"
              placeholder="Search by email..."
              value={filters.search}
              onChange={handleSearchChange}
              aria-label="Search users by email"
            />
          </div>
        </div>

        {/* Tier filter */}
        <div className="users-page__tier-filter">
          <select
            className="users-page__tier-select"
            value={filters.tier}
            onChange={handleTierChange}
            aria-label="Filter by tier"
          >
            {TIER_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      {/* Table or Empty State */}
      {isEmpty ? (
        <div className="users-page__empty">
          <Users className="users-page__empty-icon" aria-hidden="true" />
          <span>No users found{hasActiveFilters ? '. Try adjusting your filters.' : '.'}</span>
        </div>
      ) : (
        <>
          <table className="users-page__table">
            <thead>
              <tr>
                <th>Email</th>
                <th>Tier</th>
                <th>Status</th>
                <th>Region</th>
                <th>Last Login</th>
                <th className="users-page__numeric">Accounts</th>
                <th className="users-page__numeric">Rules</th>
              </tr>
            </thead>
            <tbody>
              {data.users.map((user) => (
                <tr
                  key={user.id}
                  className="users-page__row--clickable"
                  onClick={() => handleRowClick(user.id)}
                >
                  <td>{user.email}</td>
                  <td>
                    <span className={getTierBadgeClass(user.subscription_tier)}>
                      {user.subscription_tier}
                    </span>
                  </td>
                  <td>
                    <span className={getStatusBadgeClass(user.subscription_status)}>
                      {user.subscription_status}
                    </span>
                  </td>
                  <td>{user.primary_region}</td>
                  <td>{formatDate(user.last_login_at)}</td>
                  <td className="users-page__numeric">{user.linked_accounts_count}</td>
                  <td className="users-page__numeric">{user.automation_rules_count}</td>
                </tr>
              ))}
            </tbody>
          </table>

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
        onClose={handleCloseModal}
      />
    </div>
  )
}
