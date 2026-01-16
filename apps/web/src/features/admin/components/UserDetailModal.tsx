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
import { Loader2, User, Calendar, MapPin, CreditCard, Link2, Zap, Receipt, PiggyBank } from 'lucide-react'
import type { AdminUserDetail } from '@boostedbudget/api-client/models'
import './UserDetailModal.css'

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

const TIER_DISPLAY_MAP: Record<string, string> = {
  free: 'Free',
  premium_lite: 'Premium Lite',
  premium: 'Premium',
  premium_plus: 'Premium+',
}

const getTierDisplay = (tier: string): string => TIER_DISPLAY_MAP[tier] ?? tier

/**
 * UserDetailModal - Modal for viewing user details
 */
export function UserDetailModal({ user, loading, open, onClose }: UserDetailModalProps) {
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="user-detail-modal" aria-describedby="user-detail-description">
        {loading ? (
          <div className="user-detail-modal__loading">
            <Loader2 className="user-detail-modal__spinner" />
            <span>Loading user details...</span>
          </div>
        ) : user ? (
          <>
            <DialogHeader>
              <DialogTitle className="user-detail-modal__title">
                <User className="user-detail-modal__title-icon" />
                {user.email}
              </DialogTitle>
              <DialogDescription id="user-detail-description">
                User ID: {user.id}
              </DialogDescription>
            </DialogHeader>

            <div className="user-detail-modal__content">
              {/* Profile Section */}
              <section className="user-detail-modal__section">
                <h3 className="user-detail-modal__section-title">Profile</h3>
                <div className="user-detail-modal__grid">
                  <div className="user-detail-modal__field">
                    <CreditCard className="user-detail-modal__field-icon" size={16} />
                    <div className="user-detail-modal__field-content">
                      <span className="user-detail-modal__field-label">Subscription</span>
                      <span className="user-detail-modal__field-value">
                        {getTierDisplay(user.subscription_tier)} ({user.subscription_status})
                      </span>
                    </div>
                  </div>

                  <div className="user-detail-modal__field">
                    <MapPin className="user-detail-modal__field-icon" size={16} />
                    <div className="user-detail-modal__field-content">
                      <span className="user-detail-modal__field-label">Region</span>
                      <span className="user-detail-modal__field-value">{user.primary_region}</span>
                    </div>
                  </div>

                  <div className="user-detail-modal__field">
                    <Calendar className="user-detail-modal__field-icon" size={16} />
                    <div className="user-detail-modal__field-content">
                      <span className="user-detail-modal__field-label">Created</span>
                      <span className="user-detail-modal__field-value">{formatDate(user.created_at)}</span>
                    </div>
                  </div>

                  <div className="user-detail-modal__field">
                    <Calendar className="user-detail-modal__field-icon" size={16} />
                    <div className="user-detail-modal__field-content">
                      <span className="user-detail-modal__field-label">Last Login</span>
                      <span className="user-detail-modal__field-value">{formatDate(user.last_login_at)}</span>
                    </div>
                  </div>
                </div>
              </section>

              {/* Statistics Section */}
              <section className="user-detail-modal__section">
                <h3 className="user-detail-modal__section-title">Statistics</h3>
                <div className="user-detail-modal__stats">
                  <div className="user-detail-modal__stat">
                    <Link2 className="user-detail-modal__stat-icon" size={20} />
                    <span className="user-detail-modal__stat-value">
                      {formatNumber(user.linked_accounts_count)}
                    </span>
                    <span className="user-detail-modal__stat-label">Linked Accounts</span>
                  </div>

                  <div className="user-detail-modal__stat">
                    <Zap className="user-detail-modal__stat-icon" size={20} />
                    <span className="user-detail-modal__stat-value">
                      {formatNumber(user.automation_rules_count)}
                    </span>
                    <span className="user-detail-modal__stat-label">Automation Rules</span>
                  </div>

                  <div className="user-detail-modal__stat">
                    <Receipt className="user-detail-modal__stat-icon" size={20} />
                    <span className="user-detail-modal__stat-value">
                      {formatNumber(user.transactions_count)}
                    </span>
                    <span className="user-detail-modal__stat-label">Transactions</span>
                  </div>

                  <div className="user-detail-modal__stat">
                    <PiggyBank className="user-detail-modal__stat-icon" size={20} />
                    <span className="user-detail-modal__stat-value">
                      {formatNumber(user.budgets_count)}
                    </span>
                    <span className="user-detail-modal__stat-label">Budgets</span>
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
          <div className="user-detail-modal__empty">
            <span>No user data available</span>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
