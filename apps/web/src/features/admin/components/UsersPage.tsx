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
  AlertCircle,
  Shield,
  CheckCircle2,
  Clock,
  Edit2,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useUserFilters } from '../hooks/useUserFilters'
import { useRegions } from '../../../shared/hooks/useRegions'
import { UserDetailModal } from './UserDetailModal'
import { SubscriptionOverrideModal } from './SubscriptionOverrideModal'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'

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

const tierBadgeClass = (tier: string): string => {
  if (tier === 'premium') return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300'
  if (tier === 'trial') return 'bg-info/10 text-info'
  if (tier.startsWith('household_')) return 'bg-success/10 text-success'
  return 'bg-muted text-muted-foreground'
}

const statusBadgeClass = (status: string): string => {
  if (status === 'active') return 'bg-success/10 text-success'
  if (status === 'trial' || status === 'trialing') return 'bg-info/10 text-info'
  if (status === 'past_due') return 'bg-destructive/10 text-destructive'
  return 'bg-muted text-muted-foreground'
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

const overrideStatusClass = (variant: string): string => {
  if (variant === 'permanent') return 'bg-success/10 text-success'
  if (variant === 'expiring') return 'bg-info/10 text-info'
  if (variant === 'expired') return 'bg-warning/10 text-warning'
  return 'text-muted-foreground'
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
export const ADMIN_USERS_COLUMN_IDS = ['email', 'tier', 'status', 'override_status', 'region', 'last_login', 'accounts', 'rules', 'referrals'] as const

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
            <Shield className="text-amber-500 shrink-0" size={14} aria-label="Administrator" />
          )}
        </div>
      ),
    },
    {
      id: 'tier',
      header: 'Tier',
      defaultSize: 110,
      minSize: 90,
      maxSize: 140,
      render: (user) => {
        const effectiveTier = user.manual_tier_override ?? user.subscription_tier
        return (
          <span className={cn('inline-block px-2.5 py-0.5 rounded-full text-xs font-semibold capitalize', tierBadgeClass(effectiveTier))}>
            {TIER_DISPLAY[effectiveTier] ?? effectiveTier}
          </span>
        )
      },
    },
    {
      id: 'status',
      header: 'Status',
      defaultSize: 110,
      minSize: 90,
      maxSize: 140,
      render: (user) => (
        <span className={cn('inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium capitalize', statusBadgeClass(user.subscription_status))}>
          {STATUS_ICON[user.subscription_status]}
          {user.subscription_status}
        </span>
      ),
    },
    {
      id: 'override_status',
      header: 'Override Status',
      hideable: true,
      defaultSize: 140,
      minSize: 110,
      maxSize: 180,
      render: (user) => {
        const overrideStatus = getOverrideStatus(user.manual_tier_override, user.manual_tier_ends_at)
        return overrideStatus ? (
          <span className={cn('inline-block px-2 py-0.5 rounded text-xs font-medium', overrideStatusClass(overrideStatus.variant))}>
            {overrideStatus.label}
          </span>
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
      id: 'last_login',
      header: 'Last Login',
      resizable: true,
      hideable: true,
      defaultSize: 180,
      minSize: 140,
      maxSize: 220,
      render: (user) => formatDate(user.last_login_at),
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
      key: 'automation_rules_count',
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
          <Search
            size={16}
            className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none"
            aria-hidden="true"
          />
          <input
            type="text"
            className="w-full pl-9 pr-3 py-2 border border-border rounded-md bg-background text-foreground text-sm font-[inherit] transition-colors focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/15"
            placeholder="Search by email..."
            value={filters.search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label="Search users by email"
          />
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
        <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground">
          <Users className="opacity-30 w-9 h-9" aria-hidden="true" />
          <span>No users found{hasActiveFilters ? '. Try adjusting your filters.' : '.'}</span>
        </div>
      ) : (
        <>
          <DataTable
            data={users}
            columns={columns}
            rowKey={(u) => u.id}
            sort={null}
            onSortChange={() => {}}
            filters={{}}
            onFiltersChange={() => {}}
            loading={isLoading}
            onRowClick={(user) => setSelectedUserId(user.id)}
            maskedColumnIds={['email']}
            emptyState={<p className="py-12 px-4 text-center text-muted-foreground">No users found{hasActiveFilters ? '. Try adjusting your filters.' : '.'}</p>}
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
