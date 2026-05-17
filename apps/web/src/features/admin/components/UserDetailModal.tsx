/**
 * UserDetailModal Component
 * Issue #644: Admin User Management UI
 *
 * Modal dialog displaying detailed user information and statistics.
 */

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Loader2, User, Calendar, MapPin, CreditCard, Link2, Zap, Receipt, PiggyBank, Shield } from 'lucide-react'
import type { AdminUserDetail } from '@boostedbudget/api-client/models'
import { useRegions } from '../../../shared/hooks/useRegions'

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


/**
 * UserDetailModal - Modal for viewing user details
 */
export function UserDetailModal({ user, loading, open, onClose }: UserDetailModalProps) {
  const { getRegionWithFlag } = useRegions()

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="max-w-[600px]" aria-describedby="user-detail-description">
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
                {user.email}
                {user.is_admin && (
                  <span className="inline-flex items-center gap-1 ml-2 px-2 py-0.5 text-xs font-medium bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300 rounded-full">
                    <Shield size={14} />
                    Admin
                  </span>
                )}
              </DialogTitle>
              <DialogDescription id="user-detail-description">
                User ID: {user.id}
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
                      <span className="text-sm text-foreground">
                        {TIER_DISPLAY_MAP[user.subscription_tier] ?? user.subscription_tier} ({user.subscription_status})
                      </span>
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
                      {formatNumber(user.automation_rules_count)}
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
