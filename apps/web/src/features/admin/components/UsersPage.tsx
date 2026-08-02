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
import type { AdminUserListItem } from '@boostedbudget/api-client/models'
import {
  Search,
  Users,
  AlertCircle,
  Shield,
  CheckCircle2,
  Clock,
  Edit2,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import type { BadgeProps } from '@/components/ui/badge'
import { EmptyState } from '@/components/ui/empty-state'
import { useUserFilters } from '../hooks/useUserFilters'
import { useRegions } from '../../../shared/hooks/useRegions'
import { UserDetailModal } from './UserDetailModal'
import { SubscriptionOverrideModal } from './SubscriptionOverrideModal'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import { resolveServerSnapshotTime } from '@/shared/dates'
import { getSubscriptionPresentation } from '../../subscription/lib/subscriptionPresentation'
import { isOverrideActive, getOverrideStatus } from '../lib/overrideStatus'
import { KNOWN_TIER_NAMES } from '../lib/knownTiers'

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
  active: <CheckCircle2 size={13} className="text-success" />,
  trial: <Clock size={13} className="text-info" />,
  trialing: <Clock size={13} className="text-info" />,
  inactive: <AlertCircle size={13} className="text-muted-foreground" />,
  cancelled: <AlertCircle size={13} className="text-muted-foreground" />,
  past_due: <AlertCircle size={13} className="text-destructive" />,
}

const tierBadgeVariant = (tier: string): BadgeProps['variant'] => {
  if (tier === 'premium') return 'default'
  if (tier === 'trial') return 'info'
  if (tier.startsWith('household_')) return 'success'
  return 'secondary'
}

/**
 * Whether a manual tier override is currently live for this row. A single source of truth
 * shared by the Tier column and the Status column's "Comped" chip — the original bug was
 * exactly these two cells disagreeing about override liveness, so they must derive it the
 * same way.
 *
 * `AdminUserListItem` does not carry a server-computed `is_comped` field — only
 * `effective_status`, which answers "does this user currently have write access" (forced to
 * 'active' whenever EITHER the user's own subscription is active OR a manual override is
 * currently in effect). That is a different question from "is this user comped by an override
 * right now", so `effective_status` must never be consulted here: a user whose own subscription
 * is genuinely active, but whose override has *expired*, would otherwise still read as comped
 * (Issue #1413 M10 — the exact bug the admin fixes were meant to eliminate, reintroduced through
 * this fallback). Comp state is therefore derived purely from the expiry + tier-name-validity
 * predicate, `isOverrideActive`.
 *
 * manual_tier_override is an unconstrained VARCHAR(50) with no CHECK constraint on the backend,
 * so it can hold a value naming no configured tier (Issue #1413, this task) — KNOWN_TIER_NAMES
 * is passed here so such a row is treated as "not comped", matching the backend's own rejection
 * of the same malformed value.
 *
 * `now` is REQUIRED (not defaulted) — Issue #1462 follow-up G2: callers must anchor it to
 * `data.as_of`, the single per-page server snapshot instant `AdminUserListResponse` now carries,
 * the same fix SubscriptionOverrideModal already applies via `AdminSubscriptionDetailOutputBody.
 * as_of`. A bare `new Date()` default here would silently reintroduce the browser/server
 * clock-skew divergence #1462 exists to close.
 */
function hasLiveOverride(user: AdminUserListItem, now: Date): boolean {
  return isOverrideActive(user.manual_tier_override, user.manual_tier_ends_at, KNOWN_TIER_NAMES, now)
}

const overrideStatusVariant = (variant: string): BadgeProps['variant'] => {
  if (variant === 'permanent') return 'success'
  if (variant === 'expiring') return 'info'
  if (variant === 'expired') return 'warning'
  return 'secondary'
}

const formatDate = (dateString: string | undefined | null): string => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

// eslint-disable-next-line react-refresh/only-export-components
export const ADMIN_USERS_COLUMN_IDS = ['email', 'subscription_tier', 'subscription_status', 'override_status', 'region', 'last_login_at', 'created_at', 'accounts', 'rules', 'referrals'] as const

interface SelectedUser {
  id: string
  email: string
}

/**
 * UsersPage - Unified admin user + subscription management page
 */
export function UsersPage() {
  const { filters, setSearch, setTier, setPage, apiParams, hasActiveFilters, sort, setSort } = useUserFilters()
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

  // Issue #1462 follow-up G2: `now` anchors every row's override-liveness arithmetic
  // (hasLiveOverride/getOverrideStatus below) to the server's own per-page snapshot instant
  // (`data.as_of`) instead of a fresh browser-clock read, so this page's chips cannot disagree
  // with SubscriptionOverrideModal's about the same row due to client/server clock skew. Falls
  // back to the browser clock only while `data` hasn't loaded yet or `as_of` is absent/
  // unparseable (e.g. an older cached response) — mirrors the identical fallback in
  // SubscriptionOverrideModal.tsx.
  const now = resolveServerSnapshotTime(data?.as_of)

  const { sizing, onSizingChange, columnVisibility, onColumnVisibilityChange } = useTablePreferences(
    'admin-users',
    [...ADMIN_USERS_COLUMN_IDS]
  )

  type AdminUser = typeof users[0]

  const columns: ColumnDef<AdminUser>[] = [
    {
      id: 'email',
      header: 'Email',
      resizable: true,
      defaultPinned: 'left',
      defaultSize: 260,
      minSize: 180,
      render: (user) => (
        <div className="flex items-center gap-1.5 font-medium">
          {user.email}
          {user.is_admin && (
            <Shield className="text-warning shrink-0" size={14} aria-label="Administrator" />
          )}
        </div>
      ),
    },
    {
      id: 'subscription_tier',
      header: 'Tier',
      sortable: true,
      defaultSize: 110,
      minSize: 90,
      maxSize: 140,
      render: (user) => {
        // An expired override must NOT be shown as the effective tier — it falls back to the
        // raw subscription_tier, same as the backend's ComputeEffectiveTier (Issue #1413).
        const effectiveTier = hasLiveOverride(user, now)
          ? (user.manual_tier_override as string)
          : user.subscription_tier
        return (
          <Badge variant={tierBadgeVariant(effectiveTier)}>
            {TIER_DISPLAY[effectiveTier] ?? effectiveTier}
          </Badge>
        )
      },
    },
    {
      id: 'subscription_status',
      header: 'Status',
      sortable: true,
      defaultSize: 110,
      minSize: 90,
      maxSize: 140,
      render: (user) => {
        const presentation = getSubscriptionPresentation({ status: user.subscription_status })
        const comped = hasLiveOverride(user, now)
        return (
          <div className="flex items-center gap-1.5">
            <Badge variant={presentation.badgeVariant} className="capitalize">
              {STATUS_ICON[user.subscription_status]}
              {user.subscription_status}
            </Badge>
            {comped && (
              <Badge
                variant="info"
                size="sm"
                title="Effective status: active (manual override)"
                aria-label="Effective status: active (manual override)"
              >
                Comped
              </Badge>
            )}
          </div>
        )
      },
    },
    {
      id: 'override_status',
      header: 'Override Status',
      hideable: true,
      defaultSize: 140,
      minSize: 110,
      maxSize: 180,
      render: (user) => {
        // Same server-anchored `now` as hasLiveOverride above (Issue #1462 follow-up G2) — both
        // derive liveness from the same per-page `data.as_of` snapshot, never the browser clock.
        const overrideStatus = getOverrideStatus(user.manual_tier_override, user.manual_tier_ends_at, now)
        return overrideStatus ? (
          <Badge variant={overrideStatusVariant(overrideStatus.variant)}>
            {overrideStatus.label}
          </Badge>
        ) : (
          <span className="text-muted-foreground text-sm">—</span>
        )
      },
    },
    {
      id: 'region',
      header: 'Region',
      hideable: true,
      defaultSize: 110,
      minSize: 90,
      maxSize: 140,
      render: (user) => (
        <span className="font-mono text-[0.8125rem] text-muted-foreground">
          {getRegionWithFlag(user.primary_region)}
        </span>
      ),
    },
    {
      id: 'last_login_at',
      header: 'Last Login',
      sortable: true,
      resizable: true,
      hideable: true,
      defaultSize: 180,
      minSize: 140,
      maxSize: 220,
      render: (user) => formatDate(user.last_login_at),
    },
    {
      id: 'created_at',
      header: 'Member Since',
      sortable: true,
      hideable: true,
      defaultSize: 180,
      minSize: 140,
      maxSize: 220,
      render: (u) => formatDate(u.created_at),
    },
    {
      id: 'accounts',
      header: 'Accounts',
      align: 'center',
      numeric: true,
      hideable: true,
      defaultSize: 100,
      minSize: 80,
      maxSize: 130,
      key: 'linked_accounts_count',
    },
    {
      id: 'rules',
      header: 'Rules',
      align: 'center',
      numeric: true,
      hideable: true,
      defaultSize: 100,
      minSize: 80,
      maxSize: 130,
      key: 'boosts_count',
    },
    {
      id: 'referrals',
      header: 'Referrals',
      align: 'center',
      numeric: true,
      hideable: true,
      defaultSize: 110,
      minSize: 80,
      maxSize: 140,
      key: 'referrals_count',
    },
  ]

  return (
    <div className="flex flex-col gap-6 p-6 min-h-full">
      {/* Header */}
      <header className="border-b border-border pb-4">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2.5 m-0">
            Users
            {totalCount > 0 && (
              <span className="text-sm font-medium text-muted-foreground bg-muted border border-border rounded-full px-2.5 py-0.5">
                {totalCount} users
              </span>
            )}
          </h1>
          <p className="text-sm text-muted-foreground m-0">
            View and manage users. Click a row to see details, or use Override to set a manual subscription tier.
          </p>
        </div>
      </header>

      {/* Filters */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="relative flex-1 min-w-[200px] max-w-[360px]">
          <div className="relative">
            <Search
              size={16}
              className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none"
              aria-hidden="true"
            />
            <input
              type="text"
              className="w-full pl-9 pr-3 py-2 border border-border rounded-md bg-background text-foreground text-sm font-[inherit] transition-colors focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/15"
              placeholder="Search by email prefix (min. 3 chars)..."
              value={filters.search}
              onChange={(e) => setSearch(e.target.value)}
              aria-label="Search users by email prefix"
              minLength={3}
            />
          </div>
          {filters.search.trim().length > 0 && Array.from(filters.search.trim()).length < 3 && (
            <p className="text-xs text-muted-foreground mt-1">
              Keep typing — searches emails that start with your query (min. 3 chars)
            </p>
          )}
        </div>

        <select
          className="px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm font-[inherit] cursor-pointer transition-colors focus:outline-none focus:border-primary"
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
        <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground" role="status">
          <div className="w-8 h-8 border-[3px] border-border border-t-primary rounded-full animate-spin" />
          <span>Loading users...</span>
        </div>
      ) : error ? (
        <div className="flex items-center gap-3 py-16 text-destructive justify-center">
          <AlertCircle size={20} className="shrink-0" />
          <span>Failed to load users. Please try again.</span>
        </div>
      ) : isEmpty ? (
        <EmptyState
          icon={<Users />}
          title="No users found"
          description={hasActiveFilters ? 'Try adjusting your filters.' : undefined}
        />
      ) : (
        <>
          <DataTable
            data={users}
            columns={columns}
            rowKey={(u) => u.id}
            sort={sort}
            onSortChange={setSort}
            filters={{}}
            onFiltersChange={() => {}}
            loading={isLoading}
            onRowClick={(user) => setSelectedUserId(user.id)}
            maskedColumnIds={['email']}
            emptyState={<EmptyState size="compact" icon={<Users />} title={`No users found${hasActiveFilters ? '. Try adjusting your filters.' : '.'}`} />}
            columnSizing={sizing}
            onColumnSizingChange={onSizingChange}
            columnVisibility={columnVisibility}
            onColumnVisibilityChange={onColumnVisibilityChange}
            rowActions={(user) => (
              <button
                type="button"
                className="inline-flex items-center gap-1 px-3 py-1.5 border border-border rounded-md bg-background text-foreground text-[0.8125rem] font-medium cursor-pointer font-[inherit] transition-colors hover:bg-primary hover:border-primary hover:text-primary-foreground"
                onClick={() => setOverrideUser({ id: user.id, email: user.email })}
                aria-label="Override subscription"
              >
                <Edit2 size={14} />
                Override
              </button>
            )}
          />
          <TablePagination
            currentPage={currentPage}
            totalPages={totalPages}
            hasMore={hasMore}
            onPageChange={setPage}
          />
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
