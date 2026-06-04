import { NavLink } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useIsAdmin, useAdminMode, ADMIN_NAV_ITEMS } from '@/features/admin'
import { Badge } from '@/components/ui/badge'
import { useReviewSummary } from '@/features/transactions/hooks/useReviewSummary'

interface NavigationProps {
  className?: string
  orientation?: 'horizontal' | 'vertical'
  onLinkClick?: () => void
}

const baseNavItems = [
  { to: '/dashboard', label: 'Dashboard' },
  { to: '/net-worth', label: 'Net Worth' },
  { to: '/accounts', label: 'Accounts' },
  { to: '/transactions', label: 'Transactions' },
  { to: '/review', label: 'Review' },
  { to: '/budgets', label: 'Budgets' },
  { to: '/savings', label: 'Savings' },
  { to: '/reports', label: 'Reports' },
  { to: '/rules', label: 'Rules' },
  { to: '/auto-categorization', label: 'Auto-Cat' },
]

export function Navigation({ className, orientation = 'horizontal', onLinkClick }: NavigationProps) {
  const { isAdmin } = useIsAdmin()
  const { adminModeEnabled } = useAdminMode()
  const { summary } = useReviewSummary()
  const unreviewedCount = summary?.unreviewed_count ?? 0

  const navItems = isAdmin && adminModeEnabled ? [...baseNavItems, ...ADMIN_NAV_ITEMS] : baseNavItems

  return (
    <nav
      aria-label="Main navigation"
      className={cn(
        'flex',
        orientation === 'horizontal' ? 'items-center gap-6' : 'flex-col gap-2',
        className
      )}
    >
      {navItems.map((item) => {
        const isReview = item.to === '/review'

        return (
          <NavLink
            key={item.to}
            to={item.to}
            onClick={onLinkClick}
            aria-label={isReview && unreviewedCount > 0 ? `Review, ${unreviewedCount} unreviewed` : undefined}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-1.5 text-sm font-medium transition-colors',
                orientation === 'vertical' && 'py-2 px-3 rounded-md w-full',
                isActive
                  ? 'text-foreground font-semibold'
                  : 'text-muted-foreground hover:text-foreground'
              )
            }
          >
            {item.label}
            {isReview && unreviewedCount > 0 && (
              <Badge variant="warning" size="sm" aria-hidden="true">
                {unreviewedCount}
              </Badge>
            )}
          </NavLink>
        )
      })}
    </nav>
  )
}
